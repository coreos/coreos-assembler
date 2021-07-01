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
	"io/ioutil"
	"net/http"
	"net/url"
	"time"
)

// AccountAccess contains information about
type AccountAccess struct {
	Read  bool `json:"read"`
	Write bool `json:"write"`
}

// BucketAccessInfo represents bucket usage of a bucket, and its relevant
// access type for an account
type BucketAccessInfo struct {
	Name    string        `json:"name"`
	Size    uint64        `json:"size"`
	Created time.Time     `json:"created"`
	Access  AccountAccess `json:"access"`
}

// AccountInfo represents the account usage info of an
// account across buckets.
type AccountInfo struct {
	AccountName string
	Policy      json.RawMessage // Use iam/policy.Parse to parse the result, to be done by the caller.
	Buckets     []BucketAccessInfo
}

// AccountInfo returns the usage info for the authenticating account.
func (adm *AdminClient) AccountInfo(ctx context.Context) (AccountInfo, error) {
	resp, err := adm.executeMethod(ctx, http.MethodGet, requestData{relPath: adminAPIPrefix + "/accountinfo"})
	defer closeResponse(resp)
	if err != nil {
		return AccountInfo{}, err
	}

	// Check response http status code
	if resp.StatusCode != http.StatusOK {
		return AccountInfo{}, httpRespToErrorResponse(resp)
	}

	// Unmarshal the server's json response
	var accountInfo AccountInfo

	respBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return AccountInfo{}, err
	}

	err = json.Unmarshal(respBytes, &accountInfo)
	if err != nil {
		return AccountInfo{}, err
	}

	return accountInfo, nil
}

// AccountStatus - account status.
type AccountStatus string

// Account status per user.
const (
	AccountEnabled  AccountStatus = "enabled"
	AccountDisabled AccountStatus = "disabled"
)

// UserInfo carries information about long term users.
type UserInfo struct {
	SecretKey  string        `json:"secretKey,omitempty"`
	PolicyName string        `json:"policyName,omitempty"`
	Status     AccountStatus `json:"status"`
	MemberOf   []string      `json:"memberOf,omitempty"`
}

// RemoveUser - remove a user.
func (adm *AdminClient) RemoveUser(ctx context.Context, accessKey string) error {
	queryValues := url.Values{}
	queryValues.Set("accessKey", accessKey)

	reqData := requestData{
		relPath:     adminAPIPrefix + "/remove-user",
		queryValues: queryValues,
	}

	// Execute DELETE on /minio/admin/v3/remove-user to remove a user.
	resp, err := adm.executeMethod(ctx, http.MethodDelete, reqData)

	defer closeResponse(resp)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		return httpRespToErrorResponse(resp)
	}

	return nil
}

// ListUsers - list all users.
func (adm *AdminClient) ListUsers(ctx context.Context) (map[string]UserInfo, error) {
	reqData := requestData{
		relPath: adminAPIPrefix + "/list-users",
	}

	// Execute GET on /minio/admin/v3/list-users
	resp, err := adm.executeMethod(ctx, http.MethodGet, reqData)

	defer closeResponse(resp)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, httpRespToErrorResponse(resp)
	}

	data, err := DecryptData(adm.getSecretKey(), resp.Body)
	if err != nil {
		return nil, err
	}

	var users = make(map[string]UserInfo)
	if err = json.Unmarshal(data, &users); err != nil {
		return nil, err
	}

	return users, nil
}

// GetUserInfo - get info on a user
func (adm *AdminClient) GetUserInfo(ctx context.Context, name string) (u UserInfo, err error) {
	queryValues := url.Values{}
	queryValues.Set("accessKey", name)

	reqData := requestData{
		relPath:     adminAPIPrefix + "/user-info",
		queryValues: queryValues,
	}

	// Execute GET on /minio/admin/v3/user-info
	resp, err := adm.executeMethod(ctx, http.MethodGet, reqData)

	defer closeResponse(resp)
	if err != nil {
		return u, err
	}

	if resp.StatusCode != http.StatusOK {
		return u, httpRespToErrorResponse(resp)
	}

	b, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return u, err
	}

	if err = json.Unmarshal(b, &u); err != nil {
		return u, err
	}

	return u, nil
}

// SetUser - sets a user info.
func (adm *AdminClient) SetUser(ctx context.Context, accessKey, secretKey string, status AccountStatus) error {
	data, err := json.Marshal(UserInfo{
		SecretKey: secretKey,
		Status:    status,
	})
	if err != nil {
		return err
	}
	econfigBytes, err := EncryptData(adm.getSecretKey(), data)
	if err != nil {
		return err
	}

	queryValues := url.Values{}
	queryValues.Set("accessKey", accessKey)

	reqData := requestData{
		relPath:     adminAPIPrefix + "/add-user",
		queryValues: queryValues,
		content:     econfigBytes,
	}

	// Execute PUT on /minio/admin/v3/add-user to set a user.
	resp, err := adm.executeMethod(ctx, http.MethodPut, reqData)

	defer closeResponse(resp)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		return httpRespToErrorResponse(resp)
	}

	return nil
}

// AddUser - adds a user.
func (adm *AdminClient) AddUser(ctx context.Context, accessKey, secretKey string) error {
	return adm.SetUser(ctx, accessKey, secretKey, AccountEnabled)
}

// SetUserStatus - adds a status for a user.
func (adm *AdminClient) SetUserStatus(ctx context.Context, accessKey string, status AccountStatus) error {
	queryValues := url.Values{}
	queryValues.Set("accessKey", accessKey)
	queryValues.Set("status", string(status))

	reqData := requestData{
		relPath:     adminAPIPrefix + "/set-user-status",
		queryValues: queryValues,
	}

	// Execute PUT on /minio/admin/v3/set-user-status to set status.
	resp, err := adm.executeMethod(ctx, http.MethodPut, reqData)

	defer closeResponse(resp)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		return httpRespToErrorResponse(resp)
	}

	return nil
}

// AddServiceAccountReq is the request options of the add service account admin call
type AddServiceAccountReq struct {
	Policy     json.RawMessage `json:"policy,omitempty"` // Parsed value from iam/policy.Parse()
	TargetUser string          `json:"targetUser,omitempty"`
	AccessKey  string          `json:"accessKey,omitempty"`
	SecretKey  string          `json:"secretKey,omitempty"`
}

// AddServiceAccountResp is the response body of the add service account admin call
type AddServiceAccountResp struct {
	Credentials Credentials `json:"credentials"`
}

// AddServiceAccount - creates a new service account belonging to the user sending
// the request while restricting the service account permission by the given policy document.
func (adm *AdminClient) AddServiceAccount(ctx context.Context, opts AddServiceAccountReq) (Credentials, error) {
	data, err := json.Marshal(opts)
	if err != nil {
		return Credentials{}, err
	}

	econfigBytes, err := EncryptData(adm.getSecretKey(), data)
	if err != nil {
		return Credentials{}, err
	}

	reqData := requestData{
		relPath: adminAPIPrefix + "/add-service-account",
		content: econfigBytes,
	}

	// Execute PUT on /minio/admin/v3/add-service-account to set a user.
	resp, err := adm.executeMethod(ctx, http.MethodPut, reqData)
	defer closeResponse(resp)
	if err != nil {
		return Credentials{}, err
	}

	if resp.StatusCode != http.StatusOK {
		return Credentials{}, httpRespToErrorResponse(resp)
	}

	data, err = DecryptData(adm.getSecretKey(), resp.Body)
	if err != nil {
		return Credentials{}, err
	}

	var serviceAccountResp AddServiceAccountResp
	if err = json.Unmarshal(data, &serviceAccountResp); err != nil {
		return Credentials{}, err
	}
	return serviceAccountResp.Credentials, nil
}

// UpdateServiceAccountReq is the request options of the edit service account admin call
type UpdateServiceAccountReq struct {
	NewPolicy    json.RawMessage `json:"newPolicy,omitempty"` // Parsed policy from iam/policy.Parse
	NewSecretKey string          `json:"newSecretKey,omitempty"`
	NewStatus    string          `json:"newStatus,omityempty"`
}

// UpdateServiceAccount - edit an existing service account
func (adm *AdminClient) UpdateServiceAccount(ctx context.Context, accessKey string, opts UpdateServiceAccountReq) error {
	data, err := json.Marshal(opts)
	if err != nil {
		return err
	}

	econfigBytes, err := EncryptData(adm.getSecretKey(), data)
	if err != nil {
		return err
	}

	queryValues := url.Values{}
	queryValues.Set("accessKey", accessKey)

	reqData := requestData{
		relPath:     adminAPIPrefix + "/update-service-account",
		content:     econfigBytes,
		queryValues: queryValues,
	}

	// Execute POST on /minio/admin/v3/update-service-account to edit a service account
	resp, err := adm.executeMethod(ctx, http.MethodPost, reqData)
	defer closeResponse(resp)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusNoContent {
		return httpRespToErrorResponse(resp)
	}

	return nil
}

// ListServiceAccountsResp is the response body of the list service accounts call
type ListServiceAccountsResp struct {
	Accounts []string `json:"accounts"`
}

// ListServiceAccounts - list service accounts belonging to the specified user
func (adm *AdminClient) ListServiceAccounts(ctx context.Context, user string) (ListServiceAccountsResp, error) {
	queryValues := url.Values{}
	queryValues.Set("user", user)

	reqData := requestData{
		relPath:     adminAPIPrefix + "/list-service-accounts",
		queryValues: queryValues,
	}

	// Execute GET on /minio/admin/v3/list-service-accounts
	resp, err := adm.executeMethod(ctx, http.MethodGet, reqData)
	defer closeResponse(resp)
	if err != nil {
		return ListServiceAccountsResp{}, err
	}

	if resp.StatusCode != http.StatusOK {
		return ListServiceAccountsResp{}, httpRespToErrorResponse(resp)
	}

	data, err := DecryptData(adm.getSecretKey(), resp.Body)
	if err != nil {
		return ListServiceAccountsResp{}, err
	}

	var listResp ListServiceAccountsResp
	if err = json.Unmarshal(data, &listResp); err != nil {
		return ListServiceAccountsResp{}, err
	}
	return listResp, nil
}

// InfoServiceAccountResp is the response body of the info service account call
type InfoServiceAccountResp struct {
	ParentUser    string `json:"parentUser"`
	AccountStatus string `json:"accountStatus"`
	ImpliedPolicy bool   `json:"impliedPolicy"`
	Policy        string `json:"policy"`
}

// InfoServiceAccount - returns the info of service account belonging to the specified user
func (adm *AdminClient) InfoServiceAccount(ctx context.Context, accessKey string) (InfoServiceAccountResp, error) {
	queryValues := url.Values{}
	queryValues.Set("accessKey", accessKey)

	reqData := requestData{
		relPath:     adminAPIPrefix + "/info-service-account",
		queryValues: queryValues,
	}

	// Execute GET on /minio/admin/v3/info-service-account
	resp, err := adm.executeMethod(ctx, http.MethodGet, reqData)
	defer closeResponse(resp)
	if err != nil {
		return InfoServiceAccountResp{}, err
	}

	if resp.StatusCode != http.StatusOK {
		return InfoServiceAccountResp{}, httpRespToErrorResponse(resp)
	}

	data, err := DecryptData(adm.getSecretKey(), resp.Body)
	if err != nil {
		return InfoServiceAccountResp{}, err
	}

	var infoResp InfoServiceAccountResp
	if err = json.Unmarshal(data, &infoResp); err != nil {
		return InfoServiceAccountResp{}, err
	}
	return infoResp, nil
}

// DeleteServiceAccount - delete a specified service account. The server will reject
// the request if the service account does not belong to the user initiating the request
func (adm *AdminClient) DeleteServiceAccount(ctx context.Context, serviceAccount string) error {
	queryValues := url.Values{}
	queryValues.Set("accessKey", serviceAccount)

	reqData := requestData{
		relPath:     adminAPIPrefix + "/delete-service-account",
		queryValues: queryValues,
	}

	// Execute DELETE on /minio/admin/v3/delete-service-account
	resp, err := adm.executeMethod(ctx, http.MethodDelete, reqData)
	defer closeResponse(resp)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusNoContent {
		return httpRespToErrorResponse(resp)
	}

	return nil
}
