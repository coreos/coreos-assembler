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
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testImagePath = testRegionPath + "/images/" + testImage

func imageFile(t *testing.T) (string, string) {
	t.Helper()
	data := "QFI\xfbtest image payload"
	path := filepath.Join(t.TempDir(), "image.qcow2")
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	return path, data
}

func imageCreateExchanges(t *testing.T, arch, uploadURL string) []apiExchange {
	return []apiExchange{
		{http.MethodPost, testRegionPath + "/images", http.StatusCreated, `{"id":"` + testImage + `","uploadUrl":"` + uploadURL + `"}`, func(req *http.Request) {
			body := requestJSON(t, req)
			config := body["config"].(map[string]interface{})
			if body["diskFormat"] != "qcow2" || body["name"] != "fcos-test" || config["architecture"] != arch || config["operatingSystemDistro"] != "fedora" {
				t.Errorf("unexpected image payload: %#v", body)
			}
			if body["labels"] == nil {
				t.Error("image ownership labels are missing")
			}
		}},
		{http.MethodGet, testImagePath, http.StatusOK, `{}`, nil},
		{http.MethodPatch, testImagePath, http.StatusNoContent, "", func(req *http.Request) {
			labels := requestJSON(t, req)["labels"].(map[string]interface{})
			if labels["mantle-project"] != testProject || labels["mantle-region"] != "eu01" || labels["created-at"] == "" {
				t.Errorf("invalid image ownership labels: %#v", labels)
			}
		}},
	}
}

func TestUploadImage(t *testing.T) {
	for _, arch := range []struct{ input, api string }{{"x86_64", "x86"}, {"aarch64", "arm64"}} {
		t.Run(arch.input, func(t *testing.T) {
			path, data := imageFile(t)
			exchanges := imageCreateExchanges(t, arch.api, "https://upload.example/image?signature=secret")
			exchanges = append(exchanges,
				apiExchange{http.MethodGet, testImagePath, http.StatusOK, `{"status":"CREATING"}`, nil},
				apiExchange{http.MethodGet, testImagePath, http.StatusOK, `{"status":"AVAILABLE"}`, nil})
			api := newTestAPI(t, exchanges...)
			uploads := 0
			client := &http.Client{Transport: testTransport(func(req *http.Request) (*http.Response, error) {
				uploads++
				if req.Method != http.MethodPut || req.Header.Get("Authorization") != "" || req.Header.Get("Content-Type") != "application/octet-stream" {
					t.Errorf("incorrect upload method or headers: %s %#v", req.Method, req.Header)
				}
				body, err := io.ReadAll(req.Body)
				if err != nil || string(body) != data || req.ContentLength != int64(len(data)) {
					t.Errorf("incorrect streamed image: %q length %d: %v", body, req.ContentLength, err)
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{}}, nil
			})}
			id, err := api.uploadImage(context.Background(), "fcos-test", path, arch.input, client)
			if err != nil || id != testImage || uploads != 1 {
				t.Fatalf("upload returned %q, %v with %d uploads", id, err, uploads)
			}
		})
	}
}

func TestUploadImageFailureCleanup(t *testing.T) {
	for _, scenario := range []string{"insecure-url", "http-error", "transport-error", "image-error", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			path, _ := imageFile(t)
			uploadURL := "https://upload.example/image?signature=secret"
			if scenario == "insecure-url" {
				uploadURL = "http://upload.example/image"
			}
			exchanges := imageCreateExchanges(t, "x86", uploadURL)
			if scenario == "image-error" {
				exchanges = append(exchanges, apiExchange{http.MethodGet, testImagePath, http.StatusOK, `{"status":"ERROR"}`, nil})
			}
			exchanges = append(exchanges, apiExchange{http.MethodDelete, testImagePath, http.StatusNoContent, "", func(req *http.Request) {
				if req.Context().Err() != nil {
					t.Error("upload cleanup must use a fresh context")
				}
			}})
			api := newTestAPI(t, exchanges...)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			client := &http.Client{Transport: testTransport(func(req *http.Request) (*http.Response, error) {
				if scenario == "insecure-url" {
					t.Fatal("insecure upload URL must be rejected before uploading")
				}
				if scenario == "transport-error" {
					return nil, &url.Error{URL: uploadURL, Err: errors.New("storage unavailable")}
				}
				if scenario == "cancelled" {
					cancel()
					return nil, context.Canceled
				}
				status := http.StatusOK
				if scenario == "http-error" {
					status = http.StatusForbidden
				}
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("signature=secret")), Header: http.Header{}}, nil
			})}
			if _, err := api.uploadImage(ctx, "fcos-test", path, "x86_64", client); err == nil || strings.Contains(err.Error(), "secret") {
				t.Errorf("expected an upload error without signed credentials, got %v", err)
			}
		})
	}
}

func TestUploadImageValidatesBeforeCreating(t *testing.T) {
	path, _ := imageFile(t)
	for _, scenario := range []struct{ name, path, arch string }{
		{"fcos-test", path, "ppc64le"},
		{"", path, "x86_64"},
		{"fcos-test", filepath.Join(t.TempDir(), "missing"), "x86_64"},
		{"fcos-test", t.TempDir(), "x86_64"},
	} {
		api := newTestAPI(t)
		if _, err := api.UploadImage(context.Background(), scenario.name, scenario.path, scenario.arch); err == nil {
			t.Errorf("accepted invalid image input: %#v", scenario)
		}
	}
}
