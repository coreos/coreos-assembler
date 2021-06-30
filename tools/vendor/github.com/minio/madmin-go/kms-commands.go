//
// MinIO Object Storage (c) 2021 MinIO, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//

package madmin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
)

// KMSStatus contains various informations about
// the KMS connected to a MinIO server - like
// the KMS endpoints and the default key ID.
type KMSStatus struct {
	Name         string               `json:"name"`           // Name or type of the KMS
	DefaultKeyID string               `json:"default-key-id"` // The key ID used when no explicit key is specified
	Endpoints    map[string]ItemState `json:"endpoints"`      // List of KMS endpoints and their status (online/offline)
}

// KMSStatus returns status information about the KMS connected
// to the MinIO server, if configured.
func (adm *AdminClient) KMSStatus(ctx context.Context) (KMSStatus, error) {
	resp, err := adm.executeMethod(ctx, http.MethodGet, requestData{
		relPath: adminAPIPrefix + "/kms/status", // GET <endpoint>/<admin-API>/kms/status
	})
	if err != nil {
		return KMSStatus{}, err
	}
	defer closeResponse(resp)
	if resp.StatusCode != http.StatusOK {
		return KMSStatus{}, httpRespToErrorResponse(resp)
	}
	var status KMSStatus
	if err = json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return KMSStatus{}, err
	}
	return status, nil
}

// CreateKey tries to create a new master key with the given keyID
// at the KMS connected to a MinIO server.
func (adm *AdminClient) CreateKey(ctx context.Context, keyID string) error {
	// POST /minio/admin/v3/kms/key/create?key-id=<keyID>
	qv := url.Values{}
	qv.Set("key-id", keyID)
	reqData := requestData{
		relPath:     adminAPIPrefix + "/kms/key/create",
		queryValues: qv,
	}

	resp, err := adm.executeMethod(ctx, http.MethodPost, reqData)
	if err != nil {
		return err
	}
	defer closeResponse(resp)
	if resp.StatusCode != http.StatusOK {
		return httpRespToErrorResponse(resp)
	}
	return nil
}

// GetKeyStatus requests status information about the key referenced by keyID
// from the KMS connected to a MinIO by performing a Admin-API request.
// It basically hits the `/minio/admin/v3/kms/key/status` API endpoint.
func (adm *AdminClient) GetKeyStatus(ctx context.Context, keyID string) (*KMSKeyStatus, error) {
	// GET /minio/admin/v3/kms/key/status?key-id=<keyID>
	qv := url.Values{}
	qv.Set("key-id", keyID)
	reqData := requestData{
		relPath:     adminAPIPrefix + "/kms/key/status",
		queryValues: qv,
	}

	resp, err := adm.executeMethod(ctx, http.MethodGet, reqData)
	if err != nil {
		return nil, err
	}
	defer closeResponse(resp)
	if resp.StatusCode != http.StatusOK {
		return nil, httpRespToErrorResponse(resp)
	}
	var keyInfo KMSKeyStatus
	if err = json.NewDecoder(resp.Body).Decode(&keyInfo); err != nil {
		return nil, err
	}
	return &keyInfo, nil
}

// KMSKeyStatus contains some status information about a KMS master key.
// The MinIO server tries to access the KMS and perform encryption and
// decryption operations. If the MinIO server can access the KMS and
// all master key operations succeed it returns a status containing only
// the master key ID but no error.
type KMSKeyStatus struct {
	KeyID         string `json:"key-id"`
	EncryptionErr string `json:"encryption-error,omitempty"` // An empty error == success
	DecryptionErr string `json:"decryption-error,omitempty"` // An empty error == success
}
