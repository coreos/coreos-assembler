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
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stackitcloud/stackit-sdk-go/core/clients"
)

func clearAuthEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"STACKIT_SERVICE_ACCOUNT_KEY", "STACKIT_SERVICE_ACCOUNT_KEY_PATH",
		"STACKIT_PRIVATE_KEY", "STACKIT_PRIVATE_KEY_PATH", "STACKIT_SERVICE_ACCOUNT_TOKEN",
		"STACKIT_SERVICE_ACCOUNT_EMAIL", "STACKIT_TOKEN_BASEURL", "STACKIT_IDP_TOKEN_ENDPOINT",
	} {
		t.Setenv(name, "")
	}
	t.Setenv("STACKIT_CREDENTIALS_PATH", filepath.Join(t.TempDir(), "missing-credentials"))
	t.Setenv("STACKIT_FEDERATED_TOKEN_FILE", filepath.Join(t.TempDir(), "missing-federated-token"))
}

func writeAuthFile(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "credentials")
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func makeServiceAccountKey(t *testing.T) (string, *rsa.PrivateKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	privatePEM := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
	data, err := json.Marshal(map[string]any{
		"id": testProject,
		"credentials": map[string]any{
			"aud": "stackit", "iss": "kola@example.invalid", "sub": testProject, "kid": "test-key",
			"privateKey": privatePEM, "tokenEndpoint": "https://token.example.invalid/token",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(data), key
}

func signedAccessToken(t *testing.T, key *rsa.PrivateKey, expires time.Time) string {
	t.Helper()
	token, err := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(expires)}).SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func authResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func newAuthTestAPI(opts *Options, transport http.RoundTripper) *API {
	return &API{
		opts: opts,
		client: &http.Client{
			Transport: transport,
			Timeout:   time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		baseURL:          "https://iaas.api.stackit.cloud",
		operationTimeout: 500 * time.Millisecond,
	}
}

func TestSDKKeyDiscoveryAndRefresh(t *testing.T) {
	keyJSON, privateKey := makeServiceAccountKey(t)
	for _, source := range []string{"explicit path", "environment key", "environment path", "credentials key", "credentials path"} {
		t.Run(source, func(t *testing.T) {
			clearAuthEnvironment(t)
			// SDK key discovery takes precedence over a static environment token.
			t.Setenv("STACKIT_SERVICE_ACCOUNT_TOKEN", "unused-environment-token")
			opts := testOptions()
			keyPath := writeAuthFile(t, keyJSON)
			switch source {
			case "explicit path":
				opts.ServiceAccountKeyPath = keyPath
			case "environment key":
				t.Setenv("STACKIT_SERVICE_ACCOUNT_KEY", keyJSON)
			case "environment path":
				t.Setenv("STACKIT_SERVICE_ACCOUNT_KEY_PATH", keyPath)
			case "credentials key", "credentials path":
				credentials := map[string]string{"STACKIT_SERVICE_ACCOUNT_KEY": keyJSON}
				if source == "credentials path" {
					credentials = map[string]string{"STACKIT_SERVICE_ACCOUNT_KEY_PATH": keyPath}
				}
				data, err := json.Marshal(credentials)
				if err != nil {
					t.Fatal(err)
				}
				t.Setenv("STACKIT_CREDENTIALS_PATH", writeAuthFile(t, string(data)))
			}

			tokenRequests, apiRequests := 0, 0
			accessToken := signedAccessToken(t, privateKey, time.Now().Add(-time.Minute))
			api := newAuthTestAPI(opts, testTransport(func(req *http.Request) (*http.Response, error) {
				if req.URL.Host == "token.example.invalid" {
					tokenRequests++
					if req.Method != http.MethodPost || req.Header.Get("Authorization") != "" {
						t.Error("token exchange must POST the signed assertion without a bearer token")
					}
					if err := req.ParseForm(); err != nil {
						t.Fatal(err)
					}
					if req.Form.Get("grant_type") != "urn:ietf:params:oauth:grant-type:jwt-bearer" {
						t.Error("unexpected token grant type")
					}
					assertion, err := jwt.Parse(req.Form.Get("assertion"), func(*jwt.Token) (any, error) {
						return &privateKey.PublicKey, nil
					}, jwt.WithValidMethods([]string{"RS512"}), jwt.WithIssuer("kola@example.invalid"), jwt.WithAudience("stackit"))
					if err != nil || !assertion.Valid {
						t.Fatal("token exchange did not contain a valid signed service account assertion")
					}
					if tokenRequests == 2 {
						accessToken = signedAccessToken(t, privateKey, time.Now().Add(time.Hour))
					}
					return authResponse(http.StatusOK, fmt.Sprintf(`{"access_token":%q,"expires_in":3600}`, accessToken)), nil
				}
				apiRequests++
				if req.Header.Get("Authorization") != "Bearer "+accessToken {
					t.Error("IaaS request did not use the SDK access token")
				}
				return authResponse(http.StatusOK, "{}"), nil
			}))
			if err := api.configureAuthentication(); err != nil {
				t.Fatal(err)
			}
			if !api.sdkAuthentication || tokenRequests != 0 {
				t.Fatal("key authentication must defer its first token exchange until a request")
			}
			flow := api.client.Transport.(*sdkAuthTransport).rt.(*clients.KeyFlow)
			flowConfig := flow.GetConfig()
			if flowConfig.BackgroundTokenRefreshContext != nil || flowConfig.AuthHTTPClient.Timeout != api.operationTimeout {
				t.Fatal("SDK auth client must use bounded request-driven refresh")
			}
			for i := 0; i < 3; i++ {
				if err := api.request(context.Background(), http.MethodGet, testRegionPath+"/servers", nil, nil); err != nil {
					t.Fatal(err)
				}
			}
			if tokenRequests != 2 || apiRequests != 3 {
				t.Errorf("got %d token exchanges and %d API requests; want expired token refresh followed by cached reuse", tokenRequests, apiRequests)
			}
		})
	}
}

func TestSDKTokenDiscovery(t *testing.T) {
	for _, source := range []string{"environment", "credentials", "raw file override"} {
		t.Run(source, func(t *testing.T) {
			clearAuthEnvironment(t)
			opts := testOptions()
			switch source {
			case "environment":
				t.Setenv("STACKIT_SERVICE_ACCOUNT_TOKEN", "  selected-token\n")
			case "credentials":
				t.Setenv("STACKIT_CREDENTIALS_PATH", writeAuthFile(t, `{"STACKIT_SERVICE_ACCOUNT_TOKEN":"selected-token"}`))
			case "raw file override":
				t.Setenv("STACKIT_SERVICE_ACCOUNT_TOKEN", "unused-environment-token")
				t.Setenv("STACKIT_SERVICE_ACCOUNT_KEY", "invalid-unused-key")
				opts.TokenFile = writeAuthFile(t, "selected-token\n")
			}
			api := newAuthTestAPI(opts, testTransport(func(req *http.Request) (*http.Response, error) {
				if req.Header.Get("Authorization") != "Bearer selected-token" {
					t.Error("request did not use the selected static token")
				}
				return authResponse(http.StatusOK, "{}"), nil
			}))
			if err := api.configureAuthentication(); err != nil {
				t.Fatal(err)
			}
			if api.sdkAuthentication {
				t.Fatal("static tokens must retain raw token validation")
			}
			if err := api.request(context.Background(), http.MethodGet, testRegionPath+"/servers", nil, nil); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSDKAuthValidation(t *testing.T) {
	for _, test := range []struct {
		name  string
		setup func(*testing.T, *Options)
	}{
		{"conflicting explicit sources", func(t *testing.T, opts *Options) {
			opts.TokenFile = "token-file"
			opts.ServiceAccountKeyPath = "key-file"
		}},
		{"explicit key does not fall back to token", func(t *testing.T, opts *Options) {
			t.Setenv("STACKIT_SERVICE_ACCOUNT_TOKEN", "secret-token")
			opts.ServiceAccountKeyPath = writeAuthFile(t, "secret-invalid-key")
		}},
		{"invalid credentials token", func(t *testing.T, opts *Options) {
			t.Setenv("STACKIT_CREDENTIALS_PATH", writeAuthFile(t, `{"STACKIT_SERVICE_ACCOUNT_TOKEN":"secret-token\nsecond-line"}`))
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			clearAuthEnvironment(t)
			opts := testOptions()
			test.setup(t, opts)
			_, err := New(opts)
			if err == nil {
				t.Fatal("invalid authentication configuration was accepted")
			}
			if strings.Contains(err.Error(), "secret-token") || strings.Contains(err.Error(), "secret-invalid-key") {
				t.Error("authentication error disclosed credential contents")
			}
		})
	}
}

func TestSDKTokenEndpointErrorsAndRedirects(t *testing.T) {
	keyJSON, _ := makeServiceAccountKey(t)
	for _, status := range []int{http.StatusUnauthorized, http.StatusTemporaryRedirect} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			clearAuthEnvironment(t)
			opts := testOptions()
			opts.ServiceAccountKeyPath = writeAuthFile(t, keyJSON)
			requests := 0
			api := newAuthTestAPI(opts, testTransport(func(req *http.Request) (*http.Response, error) {
				requests++
				if req.URL.Host != "token.example.invalid" {
					t.Error("token exchange followed a redirect or continued after authentication failed")
				}
				response := authResponse(status, `{"error":"secret-token-response"}`)
				response.Header.Set("Location", "https://other.example.invalid/token")
				return response, nil
			}))
			if err := api.configureAuthentication(); err != nil {
				t.Fatal(err)
			}
			err := api.request(context.Background(), http.MethodGet, testRegionPath+"/servers", nil, nil)
			if err == nil || !strings.Contains(err.Error(), fmt.Sprint(status)) {
				t.Fatalf("expected sanitized HTTP %d error, got %v", status, err)
			}
			if strings.Contains(err.Error(), "secret-token-response") || requests != 1 {
				t.Error("token endpoint error leaked its response or followed a redirect")
			}
		})
	}
}
