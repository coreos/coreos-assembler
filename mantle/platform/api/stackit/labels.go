// Copyright The Mantle Authors.
// SPDX-License-Identifier: Apache-2.0

package stackit

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// Include project and region because key pairs are scoped to the account.
// These labels also keep ore gc separate from resources made by other tools.
func (a *API) resourceLabels() map[string]string {
	return map[string]string{
		"created-by":      "mantle",
		"mantle-platform": "stackit",
		"mantle-project":  a.opts.Project,
		"mantle-region":   a.opts.Region,
		"created-at":      strconv.FormatInt(time.Now().Unix(), 10),
	}
}

// STACKIT can lose labels during resource creation. Verify them after creation
// and patch only our keys, preserving unrelated labels.
func (a *API) ensureResourceLabels(ctx context.Context, path string, labels map[string]string) error {
	err := a.poll(ctx, func() (bool, error) {
		var resource struct {
			Labels map[string]string `json:"labels"`
		}
		if err := a.request(ctx, http.MethodGet, path, nil, &resource); err != nil {
			if statusIs(err, http.StatusNotFound) {
				return false, nil
			}
			return false, err
		}
		missing := make(map[string]string)
		for key, value := range labels {
			if resource.Labels[key] != value {
				missing[key] = value
			}
		}
		if len(missing) == 0 {
			return true, nil
		}
		err := a.request(ctx, http.MethodPatch, path, map[string]interface{}{"labels": missing}, nil)
		return err == nil, err
	})
	if err != nil {
		return fmt.Errorf("verifying STACKIT resource labels for %s: %w", path, err)
	}
	return nil
}
