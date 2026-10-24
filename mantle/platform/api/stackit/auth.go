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
	"net/http"
	"strings"

	"github.com/stackitcloud/stackit-sdk-go/core/auth"
	"github.com/stackitcloud/stackit-sdk-go/core/clients"
	"github.com/stackitcloud/stackit-sdk-go/core/config"
	"github.com/stackitcloud/stackit-sdk-go/core/oapierror"
)

func (a *API) configureAuthentication() error {
	if a.opts.TokenFile != "" {
		if a.opts.ServiceAccountKeyPath != "" {
			return errors.New("--stackit-token-file and --stackit-service-account-key-path are mutually exclusive")
		}
		// An explicitly selected raw token file retains precedence over SDK
		// credential discovery and is read again for each API request.
		_, err := a.bearerToken()
		return err
	}

	cfg := &config.Configuration{
		ServiceAccountKeyPath: a.opts.ServiceAccountKeyPath,
		HTTPClient:            a.client,
	}
	rt, err := auth.SetupAuth(cfg)
	if err != nil {
		// SDK diagnostics can contain credential file contents. Keep setup
		// errors actionable without exposing the credentials themselves.
		return errors.New("configuring STACKIT authentication: no valid credentials; check the service account key or token and the STACKIT SDK credential settings")
	}

	switch flow := rt.(type) {
	case *clients.TokenFlow:
		// SDK discovery includes tokens from STACKIT_SERVICE_ACCOUNT_TOKEN and
		// the standard credentials file. Apply the same validation as raw tokens.
		a.token = strings.TrimSpace(cfg.Token)
		_, err := a.bearerToken()
		return err
	case *clients.KeyFlow:
		keyConfig := flow.GetConfig()
		if keyConfig.ServiceAccountKey == nil || keyConfig.ServiceAccountKey.Credentials == nil {
			return errors.New("STACKIT service account key is missing its credentials")
		}
		// SetupAuth otherwise creates a separate HTTP client with SDK defaults.
		// Retain our timeout and redirect policy for the token exchange too.
		tokenClient := *a.client
		if a.operationTimeout > 0 && (tokenClient.Timeout == 0 || a.operationTimeout < tokenClient.Timeout) {
			tokenClient.Timeout = a.operationTimeout
		}
		keyConfig.AuthHTTPClient = &tokenClient
		keyConfig.BackgroundTokenRefreshContext = nil
		if err := flow.Init(&keyConfig); err != nil {
			return errors.New("configuring STACKIT service account key authentication failed")
		}
	case *clients.WorkloadIdentityFederationFlow:
		wifConfig := flow.GetConfig()
		tokenClient := *a.client
		if a.operationTimeout > 0 && (tokenClient.Timeout == 0 || a.operationTimeout < tokenClient.Timeout) {
			tokenClient.Timeout = a.operationTimeout
		}
		wifConfig.AuthHTTPClient = &tokenClient
		wifConfig.BackgroundTokenRefreshContext = nil
		if err := flow.Init(&wifConfig); err != nil {
			return errors.New("configuring STACKIT workload identity authentication failed")
		}
	}
	a.client.Transport = &sdkAuthTransport{rt: rt}
	a.sdkAuthentication = true
	return nil
}

// sdkAuthTransport keeps token endpoint response bodies out of errors returned
// by the SDK. API responses continue through request's ordinary status handling.
type sdkAuthTransport struct {
	rt http.RoundTripper
}

func (t *sdkAuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.rt.RoundTrip(req.Clone(req.Context()))
	if err == nil {
		return resp, nil
	}
	if errors.Is(err, context.Canceled) {
		return nil, context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return nil, context.DeadlineExceeded
	}
	var apiErr *oapierror.GenericOpenAPIError
	if errors.As(err, &apiErr) {
		return nil, fmt.Errorf("STACKIT authentication: HTTP %d %s", apiErr.StatusCode, http.StatusText(apiErr.StatusCode))
	}
	return nil, errors.New("STACKIT authenticated request failed; check the credentials and token endpoint")
}
