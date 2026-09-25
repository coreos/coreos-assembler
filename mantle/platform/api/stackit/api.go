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

// Package stackit implements the subset of the STACKIT IaaS v2 API used by kola.
// API schemas: https://github.com/stackitcloud/stackit-sdk-go/tree/main/services/iaas/v2api
package stackit

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/coreos/coreos-assembler/mantle/platform"
)

type Options struct {
	*platform.Options
	Project               string
	Region                string
	Image                 string
	MachineType           string
	Network               string
	SecurityGroups        []string
	DiskSize              int
	AvailabilityZone      string
	DiskPerformanceClass  string
	SSHSourceCIDR         string
	ServiceAccountKeyPath string
	// TokenFile overrides SDK credential discovery. The file is read on
	// every request so an external credential helper can refresh the token.
	TokenFile string
}

type API struct {
	opts              *Options
	client            *http.Client
	baseURL           string
	token             string
	sdkAuthentication bool
	pollInterval      time.Duration
	operationTimeout  time.Duration
}

// Server records the resources owned by a kola machine.
type Server struct {
	ID         string
	PublicIP   string
	PrivateIP  string
	PublicIPID string
}

type serverResponse struct {
	ID          string `json:"id"`
	Status      string `json:"status"`
	PowerStatus string `json:"powerStatus"`
	NICs        []struct {
		ID        string `json:"nicId"`
		NetworkID string `json:"networkId"`
		IPv4      string `json:"ipv4"`
	} `json:"nics"`
}

type publicIPResponse struct {
	ID string `json:"id"`
	IP string `json:"ip"`
}

type apiError struct {
	StatusCode int
	message    string
}

func (e *apiError) Error() string { return e.message }

// New configures access without creating cloud resources. Machine-specific
// settings are validated separately so ore can upload images and collect garbage.
func New(opts *Options) (*API, error) {
	if opts == nil {
		return nil, errors.New("STACKIT options are required")
	}
	copyOpts := *opts
	copyOpts.SecurityGroups = append([]string(nil), opts.SecurityGroups...)
	opts = &copyOpts
	if strings.TrimSpace(opts.Project) == "" {
		return nil, errors.New("--stackit-project is required")
	}
	for _, group := range opts.SecurityGroups {
		if strings.TrimSpace(group) == "" {
			return nil, errors.New("--stackit-security-group IDs must not be empty")
		}
	}
	if opts.Region == "" {
		opts.Region = "eu01"
	}
	if opts.MachineType == "" {
		opts.MachineType = "c2i.2"
	}
	if opts.SSHSourceCIDR == "" {
		opts.SSHSourceCIDR = "0.0.0.0/0"
	}
	ip, _, err := net.ParseCIDR(opts.SSHSourceCIDR)
	if err != nil || ip.To4() == nil {
		return nil, errors.New("--stackit-ssh-source-cidr must be an IPv4 CIDR")
	}
	if opts.DiskSize == 0 {
		opts.DiskSize = 16
	}
	if opts.DiskSize < 1 {
		return nil, errors.New("--stackit-disk-size must be positive")
	}

	a := &API{
		opts:             opts,
		client:           &http.Client{Timeout: time.Minute},
		baseURL:          "https://iaas.api.stackit.cloud",
		token:            strings.TrimSpace(os.Getenv("STACKIT_SERVICE_ACCOUNT_TOKEN")),
		pollInterval:     5 * time.Second,
		operationTimeout: 10 * time.Minute,
	}
	// Never forward the bearer token to a redirect target.
	a.client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if err := a.configureAuthentication(); err != nil {
		return nil, err
	}
	return a, nil
}

// ValidateMachineOptions checks settings needed to boot an instance before a
// flight creates its key, networks, or security groups.
func (a *API) ValidateMachineOptions() error {
	if strings.TrimSpace(a.opts.Image) == "" {
		return errors.New("--stackit-image is required")
	}
	if strings.TrimSpace(a.opts.MachineType) == "" {
		return errors.New("--stackit-machine-type is required")
	}
	if a.opts.DiskSize < 1 {
		return errors.New("--stackit-disk-size must be positive")
	}
	return nil
}

func (a *API) bearerToken() (string, error) {
	token := a.token
	if a.opts.TokenFile != "" {
		data, err := os.ReadFile(a.opts.TokenFile)
		if err != nil {
			return "", fmt.Errorf("reading STACKIT token file: %w", err)
		}
		token = strings.TrimSpace(string(data))
	}
	if token == "" {
		return "", errors.New("set STACKIT_SERVICE_ACCOUNT_TOKEN or --stackit-token-file")
	}
	if strings.ContainsAny(token, "\r\n") {
		return "", errors.New("STACKIT bearer token must be a single line")
	}
	return token, nil
}

func (a *API) projectPath(resource string) string {
	return "/v2/projects/" + url.PathEscape(a.opts.Project) + "/regions/" + url.PathEscape(a.opts.Region) + "/" + resource
}

func (a *API) request(ctx context.Context, method, path string, payload, result interface{}) error {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("encoding STACKIT request: %w", err)
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, a.baseURL+path, body)
	if err != nil {
		return fmt.Errorf("creating STACKIT request: %w", err)
	}
	if !a.sdkAuthentication {
		token, err := a.bearerToken()
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "mantle/stackit")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("STACKIT %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	const maxResponseSize = 16 << 20
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize+1))
	if err != nil {
		return fmt.Errorf("reading STACKIT response: %w", err)
	}
	if len(data) > maxResponseSize {
		return errors.New("STACKIT response exceeds 16 MiB")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Avoid including arbitrary response bodies, which may contain secrets.
		return &apiError{resp.StatusCode, fmt.Sprintf("STACKIT %s %s: HTTP %d %s", method, path, resp.StatusCode, http.StatusText(resp.StatusCode))}
	}
	if result != nil {
		if err := json.Unmarshal(data, result); err != nil {
			return fmt.Errorf("decoding STACKIT response: %w", err)
		}
	}
	return nil
}

func statusIs(err error, status int) bool {
	var apiErr *apiError
	return errors.As(err, &apiErr) && apiErr.StatusCode == status
}

func retryable(err error) bool {
	var apiErr *apiError
	return errors.As(err, &apiErr) && (apiErr.StatusCode == http.StatusConflict || apiErr.StatusCode == http.StatusTooManyRequests || apiErr.StatusCode >= 500)
}

func (a *API) poll(ctx context.Context, check func() (bool, error)) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		done, err := check()
		if err != nil && !retryable(err) {
			return err
		}
		if done && err == nil {
			return nil
		}
		timer := time.NewTimer(a.pollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// AddKey imports the flight's SSH public key. STACKIT keypairs belong to the
// authenticated account, rather than a project or region.
func (a *API) AddKey(name, publicKey string) error {
	ctx, cancel := context.WithTimeout(context.Background(), a.operationTimeout)
	defer cancel()
	labels := a.resourceLabels()
	if err := a.request(ctx, http.MethodPost, "/v2/keypairs", map[string]interface{}{"name": name, "publicKey": publicKey, "labels": labels}, nil); err != nil {
		return err
	}
	if err := a.ensureResourceLabels(ctx, "/v2/keypairs/"+url.PathEscape(name), labels); err != nil {
		return errors.Join(err, a.DeleteKey(name))
	}
	return nil
}

func (a *API) DeleteKey(name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), a.operationTimeout)
	defer cancel()
	return a.delete(ctx, "/v2/keypairs/"+url.PathEscape(name))
}

func (a *API) delete(ctx context.Context, path string) error {
	return a.poll(ctx, func() (bool, error) {
		err := a.request(ctx, http.MethodDelete, path, nil, nil)
		if statusIs(err, http.StatusNotFound) {
			return true, nil
		}
		return err == nil, err
	})
}

// CreateServer boots an uploaded image with Ignition user data, then attaches a
// public IPv4 address to its NIC on the configured network.
func (a *API) CreateServer(name, keyName, userData string) (*Server, error) {
	return a.CreateServerWithNetwork(name, keyName, userData, a.opts.Network, a.opts.SecurityGroups)
}

// CreateServerWithNetwork selects the cluster's network without mutating the
// shared API client, allowing multiple clusters to provision concurrently.
func (a *API) CreateServerWithNetwork(name, keyName, userData, network string, securityGroups []string) (_ *Server, retErr error) {
	if err := a.ValidateMachineOptions(); err != nil {
		return nil, err
	}
	if network == "" {
		return nil, errors.New("a STACKIT network is required to create a server")
	}
	ctx, cancel := context.WithTimeout(context.Background(), a.operationTimeout)
	defer cancel()
	server := &Server{}
	defer func() {
		if retErr != nil && server.ID != "" {
			// Use a fresh context: creation may have exhausted its deadline.
			if err := a.DeleteServer(server); err != nil {
				retErr = errors.Join(retErr, fmt.Errorf("cleaning up STACKIT server %s: %w", server.ID, err))
			}
		}
	}()

	labels := a.resourceLabels()
	bootVolume := map[string]interface{}{
		"size":                a.opts.DiskSize,
		"source":              map[string]string{"id": a.opts.Image, "type": "image"},
		"deleteOnTermination": true,
	}
	if a.opts.DiskPerformanceClass != "" {
		bootVolume["performanceClass"] = a.opts.DiskPerformanceClass
	}
	payload := map[string]interface{}{
		"name":        name,
		"machineType": a.opts.MachineType,
		"userData":    base64.StdEncoding.EncodeToString([]byte(userData)),
		"networking":  map[string]string{"networkId": network},
		"labels":      labels,
		"bootVolume":  bootVolume,
	}
	if a.opts.AvailabilityZone != "" {
		payload["availabilityZone"] = a.opts.AvailabilityZone
	}
	if keyName != "" {
		payload["keypairName"] = keyName
	}
	if len(securityGroups) > 0 {
		payload["securityGroups"] = securityGroups
	}
	var created serverResponse
	if err := a.request(ctx, http.MethodPost, a.projectPath("servers"), payload, &created); err != nil {
		return nil, err
	}
	server.ID = created.ID
	if server.ID == "" {
		return nil, errors.New("STACKIT created a server without returning its ID")
	}
	path := a.projectPath("servers/" + url.PathEscape(server.ID))
	var nicID string
	err := a.poll(ctx, func() (bool, error) {
		var current serverResponse
		if err := a.request(ctx, http.MethodGet, path+"?details=true", nil, &current); err != nil {
			return false, err
		}
		if current.Status == "ERROR" || current.Status == "DELETED" || current.PowerStatus == "ERROR" || current.PowerStatus == "CRASHED" {
			return false, fmt.Errorf("STACKIT server %s failed (status %s, power status %s)", server.ID, current.Status, current.PowerStatus)
		}
		if current.Status != "ACTIVE" || (current.PowerStatus != "" && current.PowerStatus != "RUNNING") {
			return false, nil
		}
		for _, nic := range current.NICs {
			if nic.NetworkID == network && nic.ID != "" && net.ParseIP(nic.IPv4).To4() != nil {
				nicID = nic.ID
				server.PrivateIP = nic.IPv4
				return true, nil
			}
		}
		return false, nil
	})
	if err != nil {
		return nil, fmt.Errorf("waiting for STACKIT server %s: %w", server.ID, err)
	}
	if err := a.ensureResourceLabels(ctx, path, labels); err != nil {
		return nil, err
	}

	var publicIP publicIPResponse
	if err := a.request(ctx, http.MethodPost, a.projectPath("public-ips"), map[string]interface{}{
		"networkInterface": nicID,
		"labels":           labels,
	}, &publicIP); err != nil {
		return nil, fmt.Errorf("allocating STACKIT public IP: %w", err)
	}
	server.PublicIPID = publicIP.ID
	server.PublicIP = publicIP.IP
	if server.PublicIPID == "" || net.ParseIP(server.PublicIP).To4() == nil {
		return nil, errors.New("STACKIT returned an incomplete public IP allocation")
	}
	if err := a.ensureResourceLabels(ctx, a.projectPath("public-ips/"+url.PathEscape(server.PublicIPID)), labels); err != nil {
		return nil, err
	}
	return server, nil
}

// DeleteServer removes the machine and its public IP. The boot volume is deleted
// by STACKIT because creation sets deleteOnTermination. Cleanup tolerates 404s.
func (a *API) DeleteServer(server *Server) error {
	if server == nil {
		return nil
	}
	var errs []error
	if server.ID != "" {
		ctx, cancel := context.WithTimeout(context.Background(), a.operationTimeout)
		path := a.projectPath("servers/" + url.PathEscape(server.ID))
		err := a.delete(ctx, path)
		if err == nil {
			err = a.poll(ctx, func() (bool, error) {
				var current serverResponse
				err := a.request(ctx, http.MethodGet, path, nil, &current)
				if statusIs(err, http.StatusNotFound) {
					return true, nil
				}
				return current.Status == "DELETED", err
			})
		}
		cancel()
		if err != nil {
			errs = append(errs, fmt.Errorf("deleting STACKIT server %s: %w", server.ID, err))
		}
	}
	if server.PublicIPID != "" {
		// Attempt IP cleanup even when server deletion fails. A separate timeout
		// leaves time to release it after a stalled server deletion.
		ctx, cancel := context.WithTimeout(context.Background(), a.operationTimeout)
		path := a.projectPath("public-ips/" + url.PathEscape(server.PublicIPID))
		err := a.poll(ctx, func() (bool, error) {
			err := a.request(ctx, http.MethodPatch, path, map[string]interface{}{"networkInterface": nil}, nil)
			if statusIs(err, http.StatusNotFound) {
				return true, nil
			}
			return err == nil, err
		})
		cancel()
		if err != nil {
			errs = append(errs, fmt.Errorf("detaching STACKIT public IP %s: %w", server.PublicIPID, err))
		}
		ctx, cancel = context.WithTimeout(context.Background(), a.operationTimeout)
		if err := a.delete(ctx, path); err != nil {
			errs = append(errs, fmt.Errorf("deleting STACKIT public IP %s: %w", server.PublicIPID, err))
		}
		cancel()
	}
	return errors.Join(errs...)
}

func (a *API) GetConsoleOutput(id string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), a.operationTimeout)
	defer cancel()
	var result struct {
		Output string `json:"output"`
	}
	// A length of zero requests the complete console log.
	err := a.request(ctx, http.MethodGet, a.projectPath("servers/"+url.PathEscape(id)+"/log?length=0"), nil, &result)
	return result.Output, err
}
