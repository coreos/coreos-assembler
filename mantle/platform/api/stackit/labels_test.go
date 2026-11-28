// Copyright The Mantle Authors.
// SPDX-License-Identifier: Apache-2.0

package stackit

import (
	"context"
	"net/http"
	"reflect"
	"testing"
)

func TestRepairResourceLabels(t *testing.T) {
	api := newTestAPI(t,
		apiExchange{http.MethodGet, testServerPath, http.StatusNotFound, "", nil},
		apiExchange{http.MethodGet, testServerPath, http.StatusOK, `{"labels":{"created-by":"mantle","mantle-project":"wrong-project","unrelated":"preserve"}}`, nil},
		apiExchange{http.MethodPatch, testServerPath, http.StatusNoContent, "", func(req *http.Request) {
			want := map[string]interface{}{"labels": map[string]interface{}{"mantle-project": testProject, "created-at": "1700000000"}}
			if got := requestJSON(t, req); !reflect.DeepEqual(got, want) {
				t.Errorf("label repair = %#v, want %#v", got, want)
			}
		}},
	)
	if err := api.ensureResourceLabels(context.Background(), testServerPath, map[string]string{
		"created-by": "mantle", "mantle-project": testProject, "created-at": "1700000000",
	}); err != nil {
		t.Fatal(err)
	}
}

func TestMatchingResourceLabelsNeedNoPatch(t *testing.T) {
	api := newTestAPI(t, apiExchange{http.MethodGet, testServerPath, http.StatusOK, `{"labels":{"created-by":"mantle","unrelated":"preserve"}}`, nil})
	if err := api.ensureResourceLabels(context.Background(), testServerPath, map[string]string{"created-by": "mantle"}); err != nil {
		t.Fatal(err)
	}
}

func TestMachineSettingsValidatedBeforeCreation(t *testing.T) {
	api := newTestAPI(t)
	api.opts.Image = ""
	if _, err := api.CreateServer("test", "", ""); err == nil {
		t.Fatal("creating a server without an image must fail before any request")
	}
	api.opts.Image = testImage
	api.opts.Network = ""
	if _, err := api.CreateServer("test", "", ""); err == nil {
		t.Fatal("creating a server without a network must fail before any request")
	}
}
