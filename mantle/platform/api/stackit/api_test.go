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
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/coreos/coreos-assembler/mantle/platform"
)

const testProject = "11111111-1111-4111-8111-111111111111"
const testImage = "22222222-2222-4222-8222-222222222222"
const testNetwork = "33333333-3333-4333-8333-333333333333"
const testServer = "44444444-4444-4444-8444-444444444444"
const testPublicIP = "55555555-5555-4555-8555-555555555555"
const testSecurityGroup = "66666666-6666-4666-8666-666666666666"
const testRegionPath = "/v2/projects/" + testProject + "/regions/eu01"
const testServerPath = testRegionPath + "/servers/" + testServer
const testPublicIPPath = testRegionPath + "/public-ips/" + testPublicIP
const testReadyServer = `{"id":"` + testServer + `","status":"ACTIVE","nics":[` +
	`{"nicId":"wrong-nic","networkId":"other-network","ipv4":"10.0.0.1"},` +
	`{"nicId":"test-nic","networkId":"` + testNetwork + `","ipv4":"10.0.0.2"}]}`

type testTransport func(*http.Request) (*http.Response, error)

func (f testTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type apiExchange struct {
	method string
	path   string
	status int
	body   string
	check  func(*http.Request)
}

func testOptions() *Options {
	return &Options{
		Options:        &platform.Options{},
		Project:        testProject,
		Region:         "eu01",
		Image:          testImage,
		MachineType:    "g2i.2",
		Network:        testNetwork,
		SecurityGroups: []string{testSecurityGroup},
		DiskSize:       16,
	}
}

func newTestAPI(t *testing.T, exchanges ...apiExchange) *API {
	t.Helper()
	next := 0
	api := &API{
		opts:             testOptions(),
		baseURL:          "https://iaas.api.stackit.cloud",
		token:            "test-token",
		pollInterval:     time.Millisecond,
		operationTimeout: time.Second,
	}
	api.client = &http.Client{Transport: testTransport(func(req *http.Request) (*http.Response, error) {
		if req.Header.Get("Authorization") != "Bearer test-token" {
			t.Error("request does not use the configured bearer token")
		}
		if next >= len(exchanges) {
			t.Errorf("unexpected request: %s %s", req.Method, req.URL.Path)
			return nil, fmt.Errorf("unexpected request")
		}
		want := exchanges[next]
		next++
		if req.Method != want.method || req.URL.Path != want.path {
			t.Errorf("request %d: got %s %s, want %s %s", next, req.Method, req.URL.Path, want.method, want.path)
		}
		if want.check != nil {
			want.check(req)
		}
		return &http.Response{
			StatusCode: want.status,
			Status:     fmt.Sprintf("%d %s", want.status, http.StatusText(want.status)),
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(want.body)),
			Request:    req,
		}, nil
	})}
	t.Cleanup(func() {
		if next != len(exchanges) {
			t.Errorf("received %d API requests, want %d", next, len(exchanges))
		}
	})
	return api
}

func requestJSON(t *testing.T, req *http.Request) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		t.Fatalf("decoding request JSON: %v", err)
	}
	return body
}

func TestServerLifecycle(t *testing.T) {
	for _, test := range []struct {
		name      string
		keyName   string
		placement bool
	}{
		{name: "key=flight-key", keyName: "flight-key"},
		{name: "key="},
		{name: "custom placement and cluster network", keyName: "flight-key", placement: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			userData := `{"ignition":{"version":"3.5.0"}}`
			networkID := testNetwork
			securityGroups := []string{testSecurityGroup}
			readyServer := testReadyServer
			if test.placement {
				networkID = "77777777-7777-4777-8777-777777777777"
				securityGroups = []string{"88888888-8888-4888-8888-888888888888", "99999999-9999-4999-8999-999999999999"}
				// The default network's NIC is listed first; the cluster override
				// must determine both the private IP and public IP attachment.
				readyServer = strings.Replace(testReadyServer, testNetwork, networkID, 1)
				readyServer = strings.Replace(readyServer, "other-network", testNetwork, 1)
			}
			wantGroups := make([]any, len(securityGroups))
			for i, group := range securityGroups {
				wantGroups[i] = group
			}
			api := newTestAPI(t,
				apiExchange{http.MethodPost, testRegionPath + "/servers", http.StatusCreated, `{"id":"` + testServer + `"}`, func(req *http.Request) {
					body := requestJSON(t, req)
					want := map[string]any{
						"name": "kola-test", "machineType": "g2i.2", "userData": base64.StdEncoding.EncodeToString([]byte(userData)),
						"networking":     map[string]any{"networkId": networkID},
						"securityGroups": wantGroups,
						"bootVolume": map[string]any{
							"size": float64(16), "deleteOnTermination": true,
							"source": map[string]any{"id": testImage, "type": "image"},
						},
					}
					if test.placement {
						want["availabilityZone"] = "eu01-2"
						want["bootVolume"].(map[string]any)["performanceClass"] = "storage_premium_perf2"
					} else if _, ok := body["availabilityZone"]; ok {
						t.Error("availabilityZone must be omitted unless explicitly selected")
					}
					for key, value := range want {
						if !reflect.DeepEqual(body[key], value) {
							t.Errorf("server payload %s = %#v, want %#v", key, body[key], value)
						}
					}
					if test.keyName == "" {
						if _, ok := body["keypairName"]; ok {
							t.Error("keypairName must be omitted when no metadata SSH key is requested")
						}
					} else if body["keypairName"] != test.keyName {
						t.Errorf("keypairName = %#v, want %q", body["keypairName"], test.keyName)
					}
				}},
				apiExchange{http.MethodGet, testServerPath, http.StatusOK, `{"status":"CREATING"}`, nil},
				apiExchange{http.MethodGet, testServerPath, http.StatusServiceUnavailable, "", nil},
				apiExchange{http.MethodGet, testServerPath, http.StatusOK, strings.Replace(readyServer, `"status":"ACTIVE"`, `"status":"ACTIVE","powerStatus":"STOPPED"`, 1), nil},
				apiExchange{http.MethodGet, testServerPath, http.StatusOK, readyServer, func(req *http.Request) {
					if req.URL.Query().Get("details") != "true" {
						t.Error("server readiness request must include NIC details")
					}
				}},
				apiExchange{http.MethodGet, testServerPath, http.StatusOK, `{}`, nil},
				apiExchange{http.MethodPatch, testServerPath, http.StatusOK, `{}`, nil},
				apiExchange{http.MethodPost, testRegionPath + "/public-ips", http.StatusCreated, `{"id":"` + testPublicIP + `","ip":"192.0.2.1"}`, func(req *http.Request) {
					if requestJSON(t, req)["networkInterface"] != "test-nic" {
						t.Error("public IP must attach to the NIC on the configured network")
					}
				}},
				apiExchange{http.MethodGet, testPublicIPPath, http.StatusOK, `{}`, nil},
				apiExchange{http.MethodPatch, testPublicIPPath, http.StatusOK, `{}`, nil},
				apiExchange{http.MethodDelete, testServerPath, http.StatusNoContent, "", nil},
				apiExchange{http.MethodGet, testServerPath, http.StatusOK, `{"status":"DELETING"}`, nil},
				apiExchange{http.MethodGet, testServerPath, http.StatusNotFound, "", nil},
				apiExchange{http.MethodPatch, testPublicIPPath, http.StatusNoContent, "", func(req *http.Request) {
					body := requestJSON(t, req)
					if value, ok := body["networkInterface"]; !ok || value != nil {
						t.Error("public IP must be detached before release")
					}
				}},
				apiExchange{http.MethodDelete, testPublicIPPath, http.StatusNoContent, "", nil},
			)
			originalNetwork := api.opts.Network
			originalGroups := append([]string(nil), api.opts.SecurityGroups...)
			var server *Server
			var err error
			if test.placement {
				api.opts.AvailabilityZone = "eu01-2"
				api.opts.DiskPerformanceClass = "storage_premium_perf2"
				server, err = api.CreateServerWithNetwork("kola-test", test.keyName, userData, networkID, securityGroups)
			} else {
				server, err = api.CreateServer("kola-test", test.keyName, userData)
			}
			if err != nil {
				t.Fatal(err)
			}
			if api.opts.Network != originalNetwork || !reflect.DeepEqual(api.opts.SecurityGroups, originalGroups) {
				t.Error("server creation mutated the shared network or security group options")
			}
			want := &Server{ID: testServer, PrivateIP: "10.0.0.2", PublicIP: "192.0.2.1", PublicIPID: testPublicIP}
			if !reflect.DeepEqual(server, want) {
				t.Errorf("server = %#v, want %#v", server, want)
			}
			if err := api.DeleteServer(server); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCreateServerFailureCleanup(t *testing.T) {
	for _, test := range []struct {
		name       string
		response   string
		allocation *apiExchange
		publicIPID string
		timeout    bool
		wantError  string
	}{
		{name: "terminal server error", response: `{"status":"ERROR"}`, wantError: "failed"},
		{name: "server deleted during creation", response: `{"status":"DELETED"}`, wantError: "failed"},
		{name: "server crashed", response: `{"status":"ACTIVE","powerStatus":"CRASHED"}`, wantError: "failed"},
		{name: "readiness deadline", response: `{"status":"CREATING"}`, timeout: true, wantError: "deadline"},
		{name: "allocation rejected", response: testReadyServer, allocation: &apiExchange{http.MethodPost, testRegionPath + "/public-ips", http.StatusForbidden, "", nil}, wantError: "allocating"},
		{name: "incomplete allocation", response: testReadyServer, allocation: &apiExchange{http.MethodPost, testRegionPath + "/public-ips", http.StatusCreated, `{"id":"` + testPublicIP + `"}`, nil}, publicIPID: testPublicIP, wantError: "incomplete"},
	} {
		t.Run(test.name, func(t *testing.T) {
			exchanges := []apiExchange{
				{http.MethodPost, testRegionPath + "/servers", http.StatusCreated, `{"id":"` + testServer + `"}`, nil},
				{http.MethodGet, testServerPath, http.StatusOK, test.response, nil},
			}
			if test.allocation != nil {
				exchanges = append(exchanges,
					apiExchange{http.MethodGet, testServerPath, http.StatusOK, `{}`, nil},
					apiExchange{http.MethodPatch, testServerPath, http.StatusOK, `{}`, nil},
					*test.allocation,
				)
			}
			exchanges = append(exchanges,
				apiExchange{http.MethodDelete, testServerPath, http.StatusNoContent, "", func(req *http.Request) {
					if err := req.Context().Err(); err != nil {
						t.Errorf("cleanup reused the expired creation context: %v", err)
					}
				}},
				apiExchange{http.MethodGet, testServerPath, http.StatusNotFound, "", nil},
			)
			if test.publicIPID != "" {
				exchanges = append(exchanges,
					apiExchange{http.MethodPatch, testPublicIPPath, http.StatusNoContent, "", nil},
					apiExchange{http.MethodDelete, testPublicIPPath, http.StatusNoContent, "", nil},
				)
			}
			api := newTestAPI(t, exchanges...)
			if test.timeout {
				api.operationTimeout = 50 * time.Millisecond
				api.pollInterval = time.Second
			}
			server, err := api.CreateServer("kola-test", "", "user data")
			if server != nil || err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("CreateServer returned %#v, %v; want error containing %q", server, err, test.wantError)
			}
			if test.timeout && !errors.Is(err, context.DeadlineExceeded) {
				t.Errorf("expected deadline error, got %v", err)
			}
		})
	}
}

func TestDeleteServerCleanup(t *testing.T) {
	t.Run("transient detach failure", func(t *testing.T) {
		api := newTestAPI(t,
			apiExchange{http.MethodPatch, testPublicIPPath, http.StatusConflict, "", nil},
			apiExchange{http.MethodPatch, testPublicIPPath, http.StatusServiceUnavailable, "", nil},
			apiExchange{http.MethodPatch, testPublicIPPath, http.StatusNoContent, "", nil},
			apiExchange{http.MethodDelete, testPublicIPPath, http.StatusNoContent, "", nil},
		)
		if err := api.DeleteServer(&Server{PublicIPID: testPublicIP}); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("detach deadline still releases IP", func(t *testing.T) {
		api := newTestAPI(t,
			apiExchange{http.MethodPatch, testPublicIPPath, http.StatusConflict, "", nil},
			apiExchange{http.MethodDelete, testPublicIPPath, http.StatusNoContent, "", func(req *http.Request) {
				if err := req.Context().Err(); err != nil {
					t.Errorf("public IP release reused the expired detach context: %v", err)
				}
			}},
		)
		api.operationTimeout = 50 * time.Millisecond
		api.pollInterval = time.Second
		if err := api.DeleteServer(&Server{PublicIPID: testPublicIP}); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected detach deadline to be reported, got %v", err)
		}
	})
	t.Run("already absent", func(t *testing.T) {
		api := newTestAPI(t,
			apiExchange{http.MethodDelete, testServerPath, http.StatusNotFound, "", nil},
			apiExchange{http.MethodGet, testServerPath, http.StatusNotFound, "", nil},
			apiExchange{http.MethodPatch, testPublicIPPath, http.StatusNotFound, "", nil},
			apiExchange{http.MethodDelete, testPublicIPPath, http.StatusNotFound, "", nil},
		)
		if err := api.DeleteServer(&Server{ID: testServer, PublicIPID: testPublicIP}); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("server deletion failed", func(t *testing.T) {
		api := newTestAPI(t,
			apiExchange{http.MethodDelete, testServerPath, http.StatusForbidden, "", nil},
			apiExchange{http.MethodPatch, testPublicIPPath, http.StatusNoContent, "", nil},
			apiExchange{http.MethodDelete, testPublicIPPath, http.StatusNoContent, "", nil},
		)
		if err := api.DeleteServer(&Server{ID: testServer, PublicIPID: testPublicIP}); err == nil {
			t.Fatal("server deletion failure must be reported after attempting public IP cleanup")
		}
	})
}

func TestKeypairsConsoleAndErrorRedaction(t *testing.T) {
	api := newTestAPI(t,
		apiExchange{http.MethodPost, "/v2/keypairs", http.StatusCreated, `{}`, func(req *http.Request) {
			body := requestJSON(t, req)
			if body["name"] != "flight-key" || body["publicKey"] != "ssh-ed25519 test-key" {
				t.Errorf("unexpected keypair payload: %#v", body)
			}
		}},
		apiExchange{http.MethodGet, "/v2/keypairs/flight-key", http.StatusOK, `{}`, nil},
		apiExchange{http.MethodPatch, "/v2/keypairs/flight-key", http.StatusOK, `{}`, nil},
		apiExchange{http.MethodGet, testServerPath + "/log", http.StatusOK, `{"output":"boot log\n"}`, func(req *http.Request) {
			if req.URL.Query().Get("length") != "0" {
				t.Error("console request must ask for the complete log")
			}
		}},
		apiExchange{http.MethodDelete, "/v2/keypairs/flight-key", http.StatusNotFound, "", nil},
		apiExchange{http.MethodPost, "/v2/keypairs", http.StatusUnauthorized, `{"message":"test-token secret response body"}`, nil},
	)
	if err := api.AddKey("flight-key", "ssh-ed25519 test-key"); err != nil {
		t.Fatal(err)
	}
	if output, err := api.GetConsoleOutput(testServer); err != nil || output != "boot log\n" {
		t.Fatalf("console output = %q, %v", output, err)
	}
	if err := api.DeleteKey("flight-key"); err != nil {
		t.Fatal(err)
	}
	err := api.AddKey("flight-key", "ssh-ed25519 test-key")
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("expected authentication failure, got %v", err)
	}
	if strings.Contains(err.Error(), "test-token") || strings.Contains(err.Error(), "secret response body") {
		t.Error("API error exposed sensitive response content")
	}
}

func TestTokenFileOverridesEnvironmentAndRefreshes(t *testing.T) {
	t.Setenv("STACKIT_SERVICE_ACCOUNT_TOKEN", "environment-token")
	opts := testOptions()
	opts.TokenFile = filepath.Join(t.TempDir(), "token")
	writeToken := func(token string) {
		t.Helper()
		if err := os.WriteFile(opts.TokenFile, []byte(token+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	writeToken("file-token")
	api, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	wantToken := "file-token"
	api.client.Transport = testTransport(func(req *http.Request) (*http.Response, error) {
		if req.Header.Get("Authorization") != "Bearer "+wantToken {
			t.Error("request did not use the current token file contents")
		}
		return &http.Response{StatusCode: http.StatusCreated, Body: io.NopCloser(strings.NewReader("{}")), Header: make(http.Header)}, nil
	})
	if err := api.AddKey("first", "ssh-ed25519 test"); err != nil {
		t.Fatal(err)
	}
	wantToken = "refreshed-token"
	writeToken(wantToken)
	if err := api.AddKey("second", "ssh-ed25519 test"); err != nil {
		t.Fatal(err)
	}
}

func TestNewValidatesAuthentication(t *testing.T) {
	clearAuthEnvironment(t)
	for _, test := range []struct {
		name    string
		token   string
		file    bool
		content string
	}{
		{name: "missing token"},
		{name: "multiline token", token: "secret-token\nsecond-line"},
		{name: "empty file overrides environment", token: "secret-token", file: true},
		{name: "multiline token file", file: true, content: "secret-token\nsecond-line"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("STACKIT_SERVICE_ACCOUNT_TOKEN", test.token)
			opts := testOptions()
			if test.file {
				opts.TokenFile = filepath.Join(t.TempDir(), "token")
				if err := os.WriteFile(opts.TokenFile, []byte(test.content), 0600); err != nil {
					t.Fatal(err)
				}
			}
			_, err := New(opts)
			if err == nil {
				t.Fatal("invalid authentication was accepted")
			}
			if strings.Contains(err.Error(), "secret-token") {
				t.Error("authentication error exposed the token")
			}
		})
	}
	t.Run("missing file overrides environment", func(t *testing.T) {
		t.Setenv("STACKIT_SERVICE_ACCOUNT_TOKEN", "secret-token")
		opts := testOptions()
		opts.TokenFile = filepath.Join(t.TempDir(), "missing-token")
		if _, err := New(opts); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("expected missing token file error, got %v", err)
		}
	})
}
