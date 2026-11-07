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
	"net/url"
	"strconv"
	"strings"
	"time"
)

type gcNIC struct {
	ID             string   `json:"nicId"`
	NetworkID      string   `json:"networkId"`
	SecurityGroups []string `json:"securityGroups"`
}

type gcResource struct {
	ID               string            `json:"id"`
	Name             string            `json:"name"`
	Labels           map[string]string `json:"labels"`
	Status           string            `json:"status"`
	Protected        bool              `json:"protected"`
	NetworkInterface *string           `json:"networkInterface"`
	KeypairName      string            `json:"keypairName"`
	ImageID          string            `json:"imageId"`
	SecurityGroups   []string          `json:"securityGroups"`
	NICs             []gcNIC           `json:"nics"`
	BootVolume       struct {
		ID     string `json:"id"`
		Source struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		} `json:"source"`
	} `json:"bootVolume"`
}

func (a *API) gcEligible(resource gcResource, cutoff time.Time) bool {
	labels := resource.Labels
	if labels["created-by"] != "mantle" || labels["mantle-platform"] != "stackit" ||
		labels["mantle-project"] != a.opts.Project || labels["mantle-region"] != a.opts.Region {
		return false
	}
	// A missing or invalid ownership timestamp fails closed. Provider timestamps
	// do not prove that a resource was created by this integration.
	createdAt, err := strconv.ParseInt(labels["created-at"], 10, 64)
	return err == nil && createdAt > 0 && time.Unix(createdAt, 0).Before(cutoff)
}

func (a *API) gcPath(resource string) string {
	if resource == "keypairs" {
		return "/v2/keypairs"
	}
	return a.projectPath(resource)
}

func (a *API) gcList(ctx context.Context, resource string, filter bool) ([]gcResource, error) {
	ctx, cancel := context.WithTimeout(ctx, a.operationTimeout)
	defer cancel()
	query := url.Values{}
	if filter {
		query.Set("label_selector", strings.Join([]string{
			"created-by=mantle", "mantle-platform=stackit",
			"mantle-project=" + a.opts.Project, "mantle-region=" + a.opts.Region,
		}, ","))
	}
	if resource == "servers" {
		query.Set("details", "true")
	}
	var result struct {
		Items []gcResource `json:"items"`
	}
	// The IaaS v2 list schemas return a complete items array; they have no
	// pagination parameters or continuation tokens.
	err := a.request(ctx, http.MethodGet, a.gcPath(resource)+"?"+query.Encode(), nil, &result)
	return result.Items, err
}

func (a *API) gcDelete(ctx context.Context, path string) error {
	ctx, cancel := context.WithTimeout(ctx, a.operationTimeout)
	defer cancel()
	return a.poll(ctx, func() (bool, error) {
		err := a.request(ctx, http.MethodDelete, path, nil, nil)
		if statusIs(err, http.StatusNotFound) {
			return true, nil
		}
		if statusIs(err, http.StatusConflict) {
			// In-use resources may conflict indefinitely; preserve them and
			// continue collecting the remaining resources.
			return false, fmt.Errorf("resource is still in use: %v", err)
		}
		return err == nil, err
	})
}

// GC removes old resources created by mantle in this project and region. Every
// candidate is checked locally even when the service accepts a label selector.
// Resources referenced by surviving servers, and explicitly supplied resources,
// are preserved regardless of their age.
func (a *API) GC(ctx context.Context, gracePeriod time.Duration) error {
	if gracePeriod < 0 {
		return errors.New("STACKIT garbage collection duration must not be negative")
	}
	cutoff := time.Now().Add(-gracePeriod)
	var errs []error
	// Include unowned and young servers when finding resources still in use.
	// A filtered candidate list alone cannot protect those dependencies.
	servers, inventoryErr := a.gcList(ctx, "servers", false)
	if inventoryErr != nil {
		errs = append(errs, fmt.Errorf("listing STACKIT server dependencies: %w", inventoryErr))
	}
	candidates, err := a.gcList(ctx, "servers", true)
	if err != nil {
		errs = append(errs, fmt.Errorf("listing STACKIT servers: %w", err))
	}
	deletedServers := map[string]bool{}
	deletedNICs := map[string]bool{}
	serverInventory := map[string]gcResource{}
	for _, server := range servers {
		serverInventory[server.ID] = server
	}
	for _, server := range candidates {
		if server.ID == "" || !a.gcEligible(server, cutoff) {
			continue
		}
		if inventoryErr == nil {
			current, found := serverInventory[server.ID]
			if !found || !a.gcEligible(current, cutoff) {
				continue
			}
		}
		path := a.gcPath("servers") + "/" + url.PathEscape(server.ID)
		serverCtx, serverCancel := context.WithTimeout(ctx, a.operationTimeout)
		err := a.gcDelete(serverCtx, path)
		if err == nil {
			err = a.poll(serverCtx, func() (bool, error) {
				var current struct {
					Status string `json:"status"`
				}
				err := a.request(serverCtx, http.MethodGet, path, nil, &current)
				if statusIs(err, http.StatusNotFound) {
					return true, nil
				}
				return current.Status == "DELETED", err
			})
		}
		serverCancel()
		if err != nil {
			errs = append(errs, fmt.Errorf("collecting STACKIT server %s: %w", server.ID, err))
			continue
		}
		deletedServers[server.ID] = true
		for _, nic := range server.NICs {
			deletedNICs[nic.ID] = true
		}
	}
	if inventoryErr != nil {
		// Without the complete inventory, even a labeled old key or image may
		// be used by an unowned server. Skip all dependent deletions.
		return errors.Join(errs...)
	}
	inUse := map[string]map[string]bool{}
	imageDependenciesKnown := true
	for _, resource := range []string{"nics", "networks", "security-groups", "keypairs", "images"} {
		inUse[resource] = map[string]bool{}
	}
	for _, server := range servers {
		if deletedServers[server.ID] {
			for _, nic := range server.NICs {
				deletedNICs[nic.ID] = true
			}
			continue
		}
		inUse["keypairs"][server.KeypairName] = true
		inUse["images"][server.ImageID] = true
		if server.BootVolume.Source.Type == "image" && server.BootVolume.Source.ID != "" {
			inUse["images"][server.BootVolume.Source.ID] = true
		} else if server.ImageID == "" {
			// A volume-backed server may only expose the boot volume ID.
			// Resolve its source before deciding that an image is unused.
			if server.BootVolume.ID == "" {
				imageDependenciesKnown = false
			} else {
				var volume struct {
					Source struct {
						ID   string `json:"id"`
						Type string `json:"type"`
					} `json:"source"`
				}
				volumeCtx, volumeCancel := context.WithTimeout(ctx, a.operationTimeout)
				err := a.request(volumeCtx, http.MethodGet, a.projectPath("volumes/"+url.PathEscape(server.BootVolume.ID)), nil, &volume)
				volumeCancel()
				if err != nil {
					errs = append(errs, fmt.Errorf("resolving STACKIT boot volume %s: %w", server.BootVolume.ID, err))
				}
				if err != nil || volume.Source.Type != "image" || volume.Source.ID == "" {
					imageDependenciesKnown = false
				} else {
					inUse["images"][volume.Source.ID] = true
				}
			}
		}
		for _, group := range server.SecurityGroups {
			inUse["security-groups"][group] = true
		}
		for _, nic := range server.NICs {
			inUse["nics"][nic.ID] = true
			inUse["networks"][nic.NetworkID] = true
			for _, group := range nic.SecurityGroups {
				inUse["security-groups"][group] = true
			}
		}
	}
	// Explicit resources belong to the caller even if they once carried
	// ownership labels from an earlier mantle invocation.
	inUse["networks"][a.opts.Network] = true
	inUse["images"][a.opts.Image] = true
	for _, group := range a.opts.SecurityGroups {
		inUse["security-groups"][group] = true
	}
	for _, kind := range []string{"public-ips", "security-groups", "networks", "keypairs", "images"} {
		if kind == "images" && !imageDependenciesKnown {
			continue
		}
		resources, err := a.gcList(ctx, kind, true)
		if err != nil {
			errs = append(errs, fmt.Errorf("listing STACKIT %s: %w", kind, err))
			continue
		}
		for _, resource := range resources {
			id := resource.ID
			if kind == "keypairs" {
				id = resource.Name
			}
			if id == "" || resource.Protected || !a.gcEligible(resource, cutoff) || inUse[kind][id] {
				continue
			}
			path := a.gcPath(kind) + "/" + url.PathEscape(id)
			if kind == "public-ips" && resource.NetworkInterface != nil && *resource.NetworkInterface != "" {
				nicID := *resource.NetworkInterface
				// Never detach an address from an unknown or surviving server.
				if inUse["nics"][nicID] || !deletedNICs[nicID] {
					continue
				}
				ipCtx, ipCancel := context.WithTimeout(ctx, a.operationTimeout)
				err := a.poll(ipCtx, func() (bool, error) {
					err := a.request(ipCtx, http.MethodPatch, path, map[string]interface{}{"networkInterface": nil}, nil)
					if statusIs(err, http.StatusNotFound) {
						return true, nil
					}
					return err == nil, err
				})
				ipCancel()
				if err != nil {
					errs = append(errs, fmt.Errorf("detaching STACKIT public IP %s: %w", id, err))
					continue
				}
			}
			if err := a.gcDelete(ctx, path); err != nil {
				errs = append(errs, fmt.Errorf("collecting STACKIT %s %s: %w", kind, id, err))
			}
		}
	}
	return errors.Join(errs...)
}
