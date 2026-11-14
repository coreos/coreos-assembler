// Copyright 2026 Red Hat
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package stackit

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// UploadImage uploads a Fedora CoreOS QCOW2 image and waits until it is usable.
// The upload URL is signed by STACKIT and must not receive our API credentials.
func (a *API) UploadImage(ctx context.Context, name, path, arch string) (string, error) {
	client := &http.Client{
		Timeout: a.operationTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return a.uploadImage(ctx, name, path, arch, client)
}

func (a *API) uploadImage(ctx context.Context, name, path, arch string, uploadClient *http.Client) (_ string, retErr error) {
	architecture, ok := map[string]string{"x86_64": "x86", "aarch64": "arm64"}[arch]
	if !ok {
		return "", fmt.Errorf("unsupported image architecture %q: use x86_64 or aarch64", arch)
	}
	if strings.TrimSpace(name) == "" {
		return "", errors.New("an image name is required")
	}
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("opening image: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", fmt.Errorf("checking image: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("image must be a regular QCOW2 file")
	}
	var magic [4]byte
	if _, err := file.ReadAt(magic[:], 0); err != nil || string(magic[:]) != "QFI\xfb" {
		return "", errors.New("image is not QCOW2; decompress or convert it before uploading")
	}

	ctx, cancel := context.WithTimeout(ctx, a.operationTimeout)
	defer cancel()
	labels := a.resourceLabels()
	var created struct {
		ID        string `json:"id"`
		UploadURL string `json:"uploadUrl"`
	}
	payload := map[string]interface{}{
		"name":       name,
		"diskFormat": "qcow2",
		"labels":     labels,
		"config": map[string]interface{}{
			"architecture":          architecture,
			"operatingSystem":       "linux",
			"operatingSystemDistro": "fedora",
			"uefi":                  true,
		},
	}
	if err := a.request(ctx, http.MethodPost, a.projectPath("images"), payload, &created); err != nil {
		return "", fmt.Errorf("creating STACKIT image: %w", err)
	}
	if created.ID == "" {
		return "", errors.New("STACKIT created an image without returning its ID")
	}
	imagePath := a.projectPath("images/" + url.PathEscape(created.ID))
	defer func() {
		if retErr != nil {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(ctx), a.operationTimeout)
			defer cleanupCancel()
			if err := a.delete(cleanupCtx, imagePath); err != nil {
				retErr = errors.Join(retErr, fmt.Errorf("deleting incomplete STACKIT image %s: %w", created.ID, err))
			}
		}
	}()
	if err := a.ensureResourceLabels(ctx, imagePath, labels); err != nil {
		return "", fmt.Errorf("labeling STACKIT image %s: %w", created.ID, err)
	}
	uploadURL, err := url.Parse(created.UploadURL)
	if err != nil || uploadURL.Scheme != "https" || uploadURL.Host == "" || uploadURL.User != nil || uploadURL.Fragment != "" {
		return "", errors.New("STACKIT returned an invalid HTTPS image upload URL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, uploadURL.String(), file)
	if err != nil {
		return "", errors.New("creating image upload request")
	}
	req.ContentLength = info.Size()
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := uploadClient.Do(req)
	if err != nil {
		// net/url errors include the signed URL, which is a credential.
		if ctx.Err() != nil {
			return "", fmt.Errorf("uploading STACKIT image: %w", ctx.Err())
		}
		return "", errors.New("uploading STACKIT image: request to image storage failed")
	}
	io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("uploading STACKIT image: HTTP %d %s", resp.StatusCode, http.StatusText(resp.StatusCode))
	}
	if err := a.poll(ctx, func() (bool, error) {
		var image struct {
			Status string `json:"status"`
		}
		if err := a.request(ctx, http.MethodGet, imagePath, nil, &image); err != nil {
			return false, err
		}
		switch image.Status {
		case "AVAILABLE":
			return true, nil
		case "ERROR", "DEACTIVATED", "DELETED":
			return false, fmt.Errorf("STACKIT image %s entered status %s", created.ID, image.Status)
		default:
			return false, nil
		}
	}); err != nil {
		return "", fmt.Errorf("waiting for STACKIT image %s: %w", created.ID, err)
	}
	return created.ID, nil
}
