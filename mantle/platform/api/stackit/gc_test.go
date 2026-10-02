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
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

func oldGCLabels() map[string]string {
	return map[string]string{
		"created-by": "mantle", "mantle-platform": "stackit",
		"mantle-project": testProject, "mantle-region": "eu01",
		"created-at": strconv.FormatInt(time.Now().Add(-24*time.Hour).Unix(), 10),
	}
}

func gcFixture(id string) map[string]interface{} {
	return map[string]interface{}{"id": id, "name": id, "labels": oldGCLabels()}
}

func gcFixtures(t *testing.T, resources ...map[string]interface{}) string {
	t.Helper()
	body, err := json.Marshal(map[string]interface{}{"items": resources})
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func gcSelectorCheck(t *testing.T, required bool) func(*http.Request) {
	return func(req *http.Request) {
		t.Helper()
		selector := req.URL.Query().Get("label_selector")
		if required {
			for _, scope := range []string{"created-by=mantle", "mantle-platform=stackit", "mantle-project=" + testProject, "mantle-region=eu01"} {
				if !strings.Contains(selector, scope) {
					t.Errorf("selector %q is missing %q", selector, scope)
				}
			}
		} else if selector != "" {
			t.Error("dependency inventory must include unowned servers")
		}
	}
}

func TestGCProtectsUnownedYoungAndInUseResources(t *testing.T) {
	old := gcFixture("old-server")
	old["nics"] = []gcNIC{{ID: "old-nic", NetworkID: "old-network", SecurityGroups: []string{"old-group"}}}
	old["keypairName"] = "old-key"
	old["imageId"] = "old-image"
	young := gcFixture("young-server")
	young["labels"].(map[string]string)["created-at"] = strconv.FormatInt(time.Now().Unix(), 10)
	young["nics"] = []gcNIC{{ID: "young-nic", NetworkID: "young-network", SecurityGroups: []string{"young-group"}}}
	young["keypairName"] = "young-key"
	young["imageId"] = "young-image"
	unowned := gcFixture("unowned-server")
	delete(unowned, "labels")
	unowned["nics"] = []gcNIC{{ID: "unowned-nic", NetworkID: "unowned-network", SecurityGroups: []string{"unowned-group"}}}
	unowned["keypairName"] = "unowned-key"
	unowned["bootVolume"] = map[string]interface{}{"source": map[string]string{"type": "image", "id": "unowned-image"}}
	oldIP := gcFixture("old-ip")
	oldIP["networkInterface"] = "old-nic"
	youngIP := gcFixture("young-ip")
	youngIP["networkInterface"] = "young-nic"
	unknownIP := gcFixture("unknown-ip")
	unknownIP["networkInterface"] = "unknown-nic"
	freeIP := gcFixture("free-ip")
	unownedIP := gcFixture("unowned-ip")
	delete(unownedIP, "labels")
	failedNetwork := map[string]interface{}{"id": "unowned-failed-network", "status": "ERROR"}
	exchanges := []apiExchange{
		{http.MethodGet, testRegionPath + "/servers", http.StatusOK, gcFixtures(t, old, young, unowned), gcSelectorCheck(t, false)},
		// Simulate a service ignoring labelSelector: local checks must still
		// protect young and unowned resources.
		{http.MethodGet, testRegionPath + "/servers", http.StatusOK, gcFixtures(t, old, young, unowned), gcSelectorCheck(t, true)},
		{http.MethodDelete, testRegionPath + "/servers/old-server", http.StatusNoContent, "", nil},
		{http.MethodGet, testRegionPath + "/servers/old-server", http.StatusNotFound, "", nil},
		{http.MethodGet, testRegionPath + "/public-ips", http.StatusOK, gcFixtures(t, oldIP, youngIP, unknownIP, freeIP, unownedIP), gcSelectorCheck(t, true)},
		{http.MethodPatch, testRegionPath + "/public-ips/old-ip", http.StatusNoContent, "", func(req *http.Request) {
			body := requestJSON(t, req)
			if value, present := body["networkInterface"]; !present || value != nil {
				t.Error("old server IP must be detached before deletion")
			}
		}},
		{http.MethodDelete, testRegionPath + "/public-ips/old-ip", http.StatusNoContent, "", nil},
		{http.MethodDelete, testRegionPath + "/public-ips/free-ip", http.StatusNoContent, "", nil},
		{http.MethodGet, testRegionPath + "/security-groups", http.StatusOK, gcFixtures(t, gcFixture("old-group"), gcFixture("young-group"), gcFixture("unowned-group"), gcFixture(testSecurityGroup)), gcSelectorCheck(t, true)},
		{http.MethodDelete, testRegionPath + "/security-groups/old-group", http.StatusNoContent, "", nil},
		{http.MethodGet, testRegionPath + "/networks", http.StatusOK, gcFixtures(t, gcFixture("old-network"), gcFixture("young-network"), gcFixture("unowned-network"), gcFixture(testNetwork), failedNetwork), gcSelectorCheck(t, true)},
		{http.MethodDelete, testRegionPath + "/networks/old-network", http.StatusNoContent, "", nil},
		{http.MethodGet, "/v2/keypairs", http.StatusOK, gcFixtures(t, gcFixture("old-key"), gcFixture("young-key"), gcFixture("unowned-key")), gcSelectorCheck(t, true)},
		{http.MethodDelete, "/v2/keypairs/old-key", http.StatusNoContent, "", nil},
		{http.MethodGet, testRegionPath + "/images", http.StatusOK, gcFixtures(t, gcFixture("old-image"), gcFixture("young-image"), gcFixture("unowned-image"), gcFixture(testImage)), gcSelectorCheck(t, true)},
		{http.MethodDelete, testRegionPath + "/images/old-image", http.StatusNoContent, "", nil},
	}
	api := newTestAPI(t, exchanges...)
	if err := api.GC(context.Background(), 5*time.Hour); err != nil {
		t.Fatal(err)
	}
}

func TestGCEligibilityRequiresCompleteOwnership(t *testing.T) {
	api := newTestAPI(t)
	cutoff := time.Now().Add(-5 * time.Hour)
	if !api.gcEligible(gcResource{Labels: oldGCLabels()}, cutoff) {
		t.Fatal("old owned resource must be eligible")
	}
	for _, field := range []string{"created-by", "mantle-platform", "mantle-project", "mantle-region", "created-at"} {
		for _, value := range []string{"", "other"} {
			labels := oldGCLabels()
			labels[field] = value
			if api.gcEligible(gcResource{Labels: labels}, cutoff) {
				t.Errorf("resource with %s=%q must be preserved", field, value)
			}
		}
	}
	for _, timestamp := range []string{"0", "-1", strconv.FormatInt(time.Now().Unix(), 10), strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10)} {
		labels := oldGCLabels()
		labels["created-at"] = timestamp
		if api.gcEligible(gcResource{Labels: labels}, cutoff) {
			t.Errorf("resource with created-at=%q must be preserved", timestamp)
		}
	}
}

func TestGCContinuesAfterIndependentFailures(t *testing.T) {
	api := newTestAPI(t,
		apiExchange{http.MethodGet, testRegionPath + "/servers", http.StatusOK, gcFixtures(t), nil},
		apiExchange{http.MethodGet, testRegionPath + "/servers", http.StatusOK, gcFixtures(t), nil},
		apiExchange{http.MethodGet, testRegionPath + "/public-ips", http.StatusForbidden, "", nil},
		apiExchange{http.MethodGet, testRegionPath + "/security-groups", http.StatusOK, gcFixtures(t, gcFixture("in-use-group")), nil},
		apiExchange{http.MethodDelete, testRegionPath + "/security-groups/in-use-group", http.StatusConflict, "", nil},
		apiExchange{http.MethodGet, testRegionPath + "/networks", http.StatusOK, gcFixtures(t), nil},
		apiExchange{http.MethodGet, "/v2/keypairs", http.StatusOK, gcFixtures(t, gcFixture("old-key")), nil},
		apiExchange{http.MethodDelete, "/v2/keypairs/old-key", http.StatusNotFound, "", nil},
		apiExchange{http.MethodGet, testRegionPath + "/images", http.StatusOK, gcFixtures(t, gcFixture("old-image")), nil},
		apiExchange{http.MethodDelete, testRegionPath + "/images/old-image", http.StatusNoContent, "", nil},
	)
	err := api.GC(context.Background(), 5*time.Hour)
	if err == nil || !strings.Contains(err.Error(), "public-ips") || !strings.Contains(err.Error(), "still in use") {
		t.Fatalf("GC must report independent failures: %v", err)
	}
}

func TestGCInventoryFailurePreservesDependencies(t *testing.T) {
	api := newTestAPI(t,
		apiExchange{http.MethodGet, testRegionPath + "/servers", http.StatusForbidden, "", nil},
		apiExchange{http.MethodGet, testRegionPath + "/servers", http.StatusOK, gcFixtures(t), nil},
	)
	if err := api.GC(context.Background(), 5*time.Hour); err == nil {
		t.Fatal("failed inventory must prevent dependent resource deletion")
	}
}

func TestGCBootVolumeImageDependencies(t *testing.T) {
	for _, knownSource := range []bool{true, false} {
		t.Run(strconv.FormatBool(knownSource), func(t *testing.T) {
			server := map[string]interface{}{"id": "unowned-server", "bootVolume": map[string]string{"id": "boot-volume"}}
			volume := `{}`
			if knownSource {
				volume = `{"source":{"type":"image","id":"used-image"}}`
			}
			exchanges := []apiExchange{
				{http.MethodGet, testRegionPath + "/servers", http.StatusOK, gcFixtures(t, server), nil},
				{http.MethodGet, testRegionPath + "/servers", http.StatusOK, gcFixtures(t), nil},
				{http.MethodGet, testRegionPath + "/volumes/boot-volume", http.StatusOK, volume, nil},
				{http.MethodGet, testRegionPath + "/public-ips", http.StatusOK, gcFixtures(t), nil},
				{http.MethodGet, testRegionPath + "/security-groups", http.StatusOK, gcFixtures(t), nil},
				{http.MethodGet, testRegionPath + "/networks", http.StatusOK, gcFixtures(t), nil},
				{http.MethodGet, "/v2/keypairs", http.StatusOK, gcFixtures(t), nil},
			}
			if knownSource {
				exchanges = append(exchanges,
					apiExchange{http.MethodGet, testRegionPath + "/images", http.StatusOK, gcFixtures(t, gcFixture("used-image"), gcFixture("unused-image")), nil},
					apiExchange{http.MethodDelete, testRegionPath + "/images/unused-image", http.StatusNoContent, "", nil})
			}
			api := newTestAPI(t, exchanges...)
			if err := api.GC(context.Background(), 5*time.Hour); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestGCRejectsNegativeDuration(t *testing.T) {
	api := newTestAPI(t)
	if err := api.GC(context.Background(), -time.Hour); err == nil {
		t.Fatal("negative grace period must be rejected before API requests")
	}
}
