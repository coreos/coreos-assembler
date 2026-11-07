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
	"net/http"
	"reflect"
	"strings"
	"testing"
)

const testNetworkPath = testRegionPath + "/networks/" + testNetwork
const testSecurityGroupPath = testRegionPath + "/security-groups/" + testSecurityGroup

func TestManagedNetworkLifecycle(t *testing.T) {
	api := newTestAPI(t,
		apiExchange{http.MethodPost, testRegionPath + "/networks", http.StatusCreated, `{"id":"` + testNetwork + `"}`, func(req *http.Request) {
			body := requestJSON(t, req)
			if body["name"] != "kola-cluster" || body["dhcp"] != true || body["routed"] != true {
				t.Fatalf("network must enable DHCP and routing: %#v", body)
			}
			if body["labels"] == nil {
				t.Error("network creation omitted ownership labels")
			}
		}},
		apiExchange{http.MethodGet, testNetworkPath, http.StatusNotFound, "", nil},
		apiExchange{http.MethodGet, testNetworkPath, http.StatusOK, `{"status":"CREATING"}`, nil},
		apiExchange{http.MethodGet, testNetworkPath, http.StatusOK, `{"status":"CREATED"}`, nil},
		apiExchange{http.MethodGet, testNetworkPath, http.StatusOK, `{"labels":{}}`, nil},
		apiExchange{http.MethodPatch, testNetworkPath, http.StatusNoContent, "", nil},
		apiExchange{http.MethodDelete, testNetworkPath, http.StatusNoContent, "", nil},
		apiExchange{http.MethodGet, testNetworkPath, http.StatusOK, `{"status":"DELETING"}`, nil},
		apiExchange{http.MethodGet, testNetworkPath, http.StatusNotFound, "", nil},
	)
	network, err := api.CreateNetwork("kola-cluster")
	if err != nil {
		t.Fatal(err)
	}
	if network.ID != testNetwork || network.Status != "CREATED" {
		t.Fatalf("unexpected network: %#v", network)
	}
	if err := api.DeleteNetwork(network.ID); err != nil {
		t.Fatal(err)
	}
}

func TestManagedNetworkFailureRollback(t *testing.T) {
	for _, labelFailure := range []bool{false, true} {
		name := "creation failed"
		if labelFailure {
			name = "labeling failed"
		}
		t.Run(name, func(t *testing.T) {
			exchanges := []apiExchange{
				{http.MethodPost, testRegionPath + "/networks", http.StatusCreated, `{"id":"` + testNetwork + `"}`, nil},
			}
			if labelFailure {
				exchanges = append(exchanges,
					apiExchange{http.MethodGet, testNetworkPath, http.StatusOK, `{"status":"CREATED"}`, nil},
					apiExchange{http.MethodGet, testNetworkPath, http.StatusForbidden, "", nil},
				)
			} else {
				exchanges = append(exchanges, apiExchange{http.MethodGet, testNetworkPath, http.StatusOK, `{"status":"FAILED"}`, nil})
			}
			exchanges = append(exchanges,
				apiExchange{http.MethodDelete, testNetworkPath, http.StatusNoContent, "", nil},
				apiExchange{http.MethodGet, testNetworkPath, http.StatusNotFound, "", nil},
			)
			api := newTestAPI(t, exchanges...)
			if _, err := api.CreateNetwork("kola-cluster"); err == nil {
				t.Fatal("expected failure after rolling back the network")
			}
		})
	}
	t.Run("cleanup error is retained", func(t *testing.T) {
		api := newTestAPI(t,
			apiExchange{http.MethodPost, testRegionPath + "/networks", http.StatusCreated, `{"id":"` + testNetwork + `"}`, nil},
			apiExchange{http.MethodGet, testNetworkPath, http.StatusOK, `{"status":"FAILED"}`, nil},
			apiExchange{http.MethodDelete, testNetworkPath, http.StatusForbidden, "", nil},
		)
		_, err := api.CreateNetwork("kola-cluster")
		if err == nil || !strings.Contains(err.Error(), "FAILED") || !strings.Contains(err.Error(), "cleaning up") {
			t.Fatalf("expected original and cleanup errors, got %v", err)
		}
	})
}

func TestManagedSecurityGroupRules(t *testing.T) {
	for _, test := range []struct {
		name          string
		rulesResponse string
		defaultEgress bool
	}{
		{"without default egress", `{"rules":[]}`, false},
		{"with default egress", `{"rules":[{"direction":"egress","ethertype":"IPv4","protocol":null}]}`, true},
		{"restricted egress protocol object", `{"rules":[{"direction":"egress","ethertype":"IPv4","protocol":{"name":"tcp","number":6}}]}`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			exchanges := []apiExchange{
				{http.MethodPost, testRegionPath + "/security-groups", http.StatusCreated, `{"id":"` + testSecurityGroup + `"}`, func(req *http.Request) {
					body := requestJSON(t, req)
					if body["stateful"] != true || body["labels"] == nil {
						t.Fatalf("security group must be stateful and labeled: %#v", body)
					}
				}},
				{http.MethodGet, testSecurityGroupPath, http.StatusOK, `{"labels":{}}`, nil},
				{http.MethodPatch, testSecurityGroupPath, http.StatusOK, `{}`, nil},
				{http.MethodGet, testSecurityGroupPath, http.StatusOK, test.rulesResponse, nil},
			}
			wantRules := []map[string]any{
				{"direction": "ingress", "ethertype": "IPv4", "protocol": "tcp", "ipRange": "192.0.2.0/24", "portRange": map[string]any{"min": float64(22), "max": float64(22)}},
				{"direction": "ingress", "ethertype": "IPv4", "protocol": "tcp", "remoteSecurityGroupId": testSecurityGroup},
				{"direction": "ingress", "ethertype": "IPv4", "protocol": "udp", "remoteSecurityGroupId": testSecurityGroup},
				{"direction": "ingress", "ethertype": "IPv4", "protocol": "icmp", "remoteSecurityGroupId": testSecurityGroup},
			}
			if !test.defaultEgress {
				wantRules = append(wantRules, map[string]any{"direction": "egress", "ethertype": "IPv4", "ipRange": "0.0.0.0/0"})
			}
			for _, want := range wantRules {
				exchanges = append(exchanges, apiExchange{http.MethodPost, testSecurityGroupPath + "/rules", http.StatusCreated, `{}`, func(req *http.Request) {
					if got := requestJSON(t, req); !reflect.DeepEqual(got, want) {
						t.Errorf("rule = %#v, want %#v", got, want)
					}
				}})
			}
			api := newTestAPI(t, exchanges...)
			api.opts.SSHSourceCIDR = "192.0.2.0/24"
			group, err := api.CreateSecurityGroup("kola-cluster")
			if err != nil {
				t.Fatal(err)
			}
			if group.ID != testSecurityGroup {
				t.Fatalf("security group ID = %q", group.ID)
			}
		})
	}
}

func TestManagedSecurityGroupFailureRollback(t *testing.T) {
	api := newTestAPI(t,
		apiExchange{http.MethodPost, testRegionPath + "/security-groups", http.StatusCreated, `{"id":"` + testSecurityGroup + `"}`, nil},
		apiExchange{http.MethodGet, testSecurityGroupPath, http.StatusOK, `{"labels":{}}`, nil},
		apiExchange{http.MethodPatch, testSecurityGroupPath, http.StatusOK, `{}`, nil},
		apiExchange{http.MethodGet, testSecurityGroupPath, http.StatusOK, `{"rules":[]}`, nil},
		apiExchange{http.MethodPost, testSecurityGroupPath + "/rules", http.StatusCreated, `{}`, nil},
		apiExchange{http.MethodPost, testSecurityGroupPath + "/rules", http.StatusForbidden, "", nil},
		apiExchange{http.MethodDelete, testSecurityGroupPath, http.StatusNoContent, "", nil},
	)
	if _, err := api.CreateSecurityGroup("kola-cluster"); err == nil {
		t.Fatal("expected rule creation failure after rolling back the group")
	}
}

func TestManagedSecurityGroupRejectsInvalidSSHSource(t *testing.T) {
	for _, source := range []string{"bad-cidr", "2001:db8::/32"} {
		t.Run(source, func(t *testing.T) {
			api := newTestAPI(t)
			api.opts.SSHSourceCIDR = source
			if _, err := api.CreateSecurityGroup("kola-cluster"); err == nil {
				t.Fatal("invalid IPv4 source accepted")
			}
		})
	}
}
