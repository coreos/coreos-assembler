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
	"errors"
	"reflect"
	"testing"

	"github.com/coreos/coreos-assembler/mantle/platform/api/stackit"
)

type fakeNetworkAPI struct {
	calls              []string
	createNetworkError error
	createGroupError   error
	deleteNetworkError error
	deleteGroupError   error
}

func (a *fakeNetworkAPI) CreateNetwork(name string) (*stackit.Network, error) {
	a.calls = append(a.calls, "create-network:"+name)
	return &stackit.Network{ID: "managed-network"}, a.createNetworkError
}

func (a *fakeNetworkAPI) CreateSecurityGroup(name string) (*stackit.SecurityGroup, error) {
	a.calls = append(a.calls, "create-group:"+name)
	return &stackit.SecurityGroup{ID: "managed-group"}, a.createGroupError
}

func (a *fakeNetworkAPI) DeleteNetwork(id string) error {
	a.calls = append(a.calls, "delete-network:"+id)
	return a.deleteNetworkError
}

func (a *fakeNetworkAPI) DeleteSecurityGroup(id string) error {
	a.calls = append(a.calls, "delete-group:"+id)
	return a.deleteGroupError
}

func TestClusterNetworkingOwnership(t *testing.T) {
	for _, test := range []struct {
		name      string
		opts      stackit.Options
		network   string
		groups    []string
		wantCalls []string
	}{
		{
			name: "managed", network: "managed-network", groups: []string{"managed-group"},
			wantCalls: []string{"create-network:cluster", "create-group:cluster", "delete-group:managed-group", "delete-network:managed-network"},
		},
		{
			name: "provided", opts: stackit.Options{Network: "existing-network", SecurityGroups: []string{"existing-group"}},
			network: "existing-network", groups: []string{"existing-group"},
		},
		{
			name: "provided network", opts: stackit.Options{Network: "existing-network"},
			network: "existing-network", groups: []string{"managed-group"},
			wantCalls: []string{"create-group:cluster", "delete-group:managed-group"},
		},
		{
			name: "provided group", opts: stackit.Options{SecurityGroups: []string{"existing-group"}},
			network: "managed-network", groups: []string{"existing-group"},
			wantCalls: []string{"create-network:cluster", "delete-network:managed-network"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			api := &fakeNetworkAPI{}
			networking, err := newClusterNetworking(api, "cluster", &test.opts)
			if err != nil {
				t.Fatal(err)
			}
			if networking.networkID != test.network || !reflect.DeepEqual(networking.securityGroups, test.groups) {
				t.Fatalf("unexpected network selection: %q %v", networking.networkID, networking.securityGroups)
			}
			if err := networking.destroy(); err != nil {
				t.Fatal(err)
			}
			if err := networking.destroy(); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(api.calls, test.wantCalls) {
				t.Fatalf("calls = %v, want %v", api.calls, test.wantCalls)
			}
		})
	}
}

func TestClusterNetworkingSetupRollback(t *testing.T) {
	createError := errors.New("security group creation failed")
	deleteError := errors.New("network deletion failed")
	api := &fakeNetworkAPI{createGroupError: createError, deleteNetworkError: deleteError}
	_, err := newClusterNetworking(api, "cluster", &stackit.Options{})
	if !errors.Is(err, createError) || !errors.Is(err, deleteError) {
		t.Fatalf("expected creation and rollback errors, got %v", err)
	}
	want := []string{"create-network:cluster", "create-group:cluster", "delete-network:managed-network"}
	if !reflect.DeepEqual(api.calls, want) {
		t.Fatalf("calls = %v, want %v", api.calls, want)
	}

	api = &fakeNetworkAPI{createGroupError: createError}
	_, err = newClusterNetworking(api, "cluster", &stackit.Options{Network: "existing-network"})
	if !errors.Is(err, createError) || !reflect.DeepEqual(api.calls, []string{"create-group:cluster"}) {
		t.Fatalf("rollback touched an existing network: calls %v, error %v", api.calls, err)
	}
}

func TestClusterNetworkingCleanupAttemptsBothResources(t *testing.T) {
	groupError := errors.New("security group deletion failed")
	networkError := errors.New("network deletion failed")
	api := &fakeNetworkAPI{deleteGroupError: groupError, deleteNetworkError: networkError}
	networking, err := newClusterNetworking(api, "cluster", &stackit.Options{})
	if err != nil {
		t.Fatal(err)
	}
	err = networking.destroy()
	if !errors.Is(err, groupError) || !errors.Is(err, networkError) {
		t.Fatalf("expected both cleanup errors, got %v", err)
	}
	api.deleteGroupError, api.deleteNetworkError = nil, nil
	if err := networking.destroy(); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"create-network:cluster", "create-group:cluster",
		"delete-group:managed-group", "delete-network:managed-network",
		"delete-group:managed-group", "delete-network:managed-network",
	}
	if !reflect.DeepEqual(api.calls, want) {
		t.Fatalf("calls = %v, want %v", api.calls, want)
	}
}
