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
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ServiceType represents service type
type ServiceType string

const (
	// ReplicationService specifies replication service
	ReplicationService ServiceType = "replication"
)

// IsValid returns true if ARN type represents replication
func (t ServiceType) IsValid() bool {
	return t == ReplicationService
}

// ARN is a struct to define arn.
type ARN struct {
	Type   ServiceType
	ID     string
	Region string
	Bucket string
}

// Empty returns true if arn struct is empty
func (a ARN) Empty() bool {
	return !a.Type.IsValid()
}
func (a ARN) String() string {
	return fmt.Sprintf("arn:minio:%s:%s:%s:%s", a.Type, a.Region, a.ID, a.Bucket)
}

// ParseARN return ARN struct from string in arn format.
func ParseARN(s string) (*ARN, error) {
	// ARN must be in the format of arn:minio:<Type>:<REGION>:<ID>:<remote-bucket>
	if !strings.HasPrefix(s, "arn:minio:") {
		return nil, fmt.Errorf("Invalid ARN %s", s)
	}

	tokens := strings.Split(s, ":")
	if len(tokens) != 6 {
		return nil, fmt.Errorf("Invalid ARN %s", s)
	}

	if tokens[4] == "" || tokens[5] == "" {
		return nil, fmt.Errorf("Invalid ARN %s", s)
	}

	return &ARN{
		Type:   ServiceType(tokens[2]),
		Region: tokens[3],
		ID:     tokens[4],
		Bucket: tokens[5],
	}, nil
}

// BucketTarget represents the target bucket and site association.
type BucketTarget struct {
	SourceBucket        string        `json:"sourcebucket"`
	Endpoint            string        `json:"endpoint"`
	Credentials         *Credentials  `json:"credentials"`
	TargetBucket        string        `json:"targetbucket"`
	Secure              bool          `json:"secure"`
	Path                string        `json:"path,omitempty"`
	API                 string        `json:"api,omitempty"`
	Arn                 string        `json:"arn,omitempty"`
	Type                ServiceType   `json:"type"`
	Region              string        `json:"omitempty"`
	BandwidthLimit      int64         `json:"bandwidthlimit,omitempty"`
	ReplicationSync     bool          `json:"replicationSync"`
	StorageClass        string        `json:"storageclass,omitempty"`
	HealthCheckDuration time.Duration `json:"healthCheckDuration,omitempty"`
	DisableProxy        bool          `json:"disableProxy"`
	ResetBeforeDate     time.Time     `json:"resetBeforeDate,omitempty"`
	ResetID             string        `json:"resetID,omitempty"`
}

// Clone returns shallow clone of BucketTarget without secret key in credentials
func (t *BucketTarget) Clone() BucketTarget {
	return BucketTarget{
		SourceBucket:        t.SourceBucket,
		Endpoint:            t.Endpoint,
		TargetBucket:        t.TargetBucket,
		Credentials:         &Credentials{AccessKey: t.Credentials.AccessKey},
		Secure:              t.Secure,
		Path:                t.Path,
		API:                 t.API,
		Arn:                 t.Arn,
		Type:                t.Type,
		Region:              t.Region,
		BandwidthLimit:      t.BandwidthLimit,
		ReplicationSync:     t.ReplicationSync,
		StorageClass:        t.StorageClass, // target storage class
		HealthCheckDuration: t.HealthCheckDuration,
		DisableProxy:        t.DisableProxy,
		ResetBeforeDate:     t.ResetBeforeDate,
		ResetID:             t.ResetID,
	}
}

// URL returns target url
func (t BucketTarget) URL() *url.URL {
	scheme := "http"
	if t.Secure {
		scheme = "https"
	}
	return &url.URL{
		Scheme: scheme,
		Host:   t.Endpoint,
	}
}

// Empty returns true if struct is empty.
func (t BucketTarget) Empty() bool {
	return t.String() == "" || t.Credentials == nil
}

func (t *BucketTarget) String() string {
	return fmt.Sprintf("%s %s", t.Endpoint, t.TargetBucket)
}

// BucketTargets represents a slice of bucket targets by type and endpoint
type BucketTargets struct {
	Targets []BucketTarget
}

// Empty returns true if struct is empty.
func (t BucketTargets) Empty() bool {
	if len(t.Targets) == 0 {
		return true
	}
	empty := true
	for _, t := range t.Targets {
		if !t.Empty() {
			return false
		}
	}
	return empty
}

// ListRemoteTargets - gets target(s) for this bucket
func (adm *AdminClient) ListRemoteTargets(ctx context.Context, bucket, arnType string) (targets []BucketTarget, err error) {
	queryValues := url.Values{}
	queryValues.Set("bucket", bucket)
	queryValues.Set("type", arnType)

	reqData := requestData{
		relPath:     adminAPIPrefix + "/list-remote-targets",
		queryValues: queryValues,
	}

	// Execute GET on /minio/admin/v3/list-remote-targets
	resp, err := adm.executeMethod(ctx, http.MethodGet, reqData)

	defer closeResponse(resp)
	if err != nil {
		return targets, err
	}

	if resp.StatusCode != http.StatusOK {
		return targets, httpRespToErrorResponse(resp)
	}

	b, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return targets, err
	}
	if err = json.Unmarshal(b, &targets); err != nil {
		return targets, err
	}
	return targets, nil
}

// SetRemoteTarget sets up a remote target for this bucket
func (adm *AdminClient) SetRemoteTarget(ctx context.Context, bucket string, target *BucketTarget) (string, error) {
	data, err := json.Marshal(target)
	if err != nil {
		return "", err
	}
	encData, err := EncryptData(adm.getSecretKey(), data)
	if err != nil {
		return "", err
	}
	queryValues := url.Values{}
	queryValues.Set("bucket", bucket)

	reqData := requestData{
		relPath:     adminAPIPrefix + "/set-remote-target",
		queryValues: queryValues,
		content:     encData,
	}

	// Execute PUT on /minio/admin/v3/set-remote-target to set a target for this bucket of specific arn type.
	resp, err := adm.executeMethod(ctx, http.MethodPut, reqData)

	defer closeResponse(resp)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		return "", httpRespToErrorResponse(resp)
	}
	b, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	var arn string
	if err = json.Unmarshal(b, &arn); err != nil {
		return "", err
	}
	return arn, nil
}

// TargetUpdateType -  type of update on the remote target
type TargetUpdateType int

const (
	// CredentialsUpdateType update creds
	CredentialsUpdateType TargetUpdateType = 1 + iota
	// SyncUpdateType update synchronous replication setting
	SyncUpdateType
	// ProxyUpdateType update proxy setting
	ProxyUpdateType
	// BandwidthLimitUpdateType update bandwidth limit
	BandwidthLimitUpdateType
	// HealthCheckDurationUpdateType update health check duration
	HealthCheckDurationUpdateType
	// PathUpdateType update Path
	PathUpdateType
	// ResetUpdateType sets ResetBeforeDate and ResetID on a bucket target
	ResetUpdateType
)

// GetTargetUpdateOps returns a slice of update operations being
// performed with `mc admin bucket remote edit`
func GetTargetUpdateOps(values url.Values) []TargetUpdateType {
	var ops []TargetUpdateType
	if values.Get("update") != "true" {
		return ops
	}
	if values.Get("creds") == "true" {
		ops = append(ops, CredentialsUpdateType)
	}
	if values.Get("sync") == "true" {
		ops = append(ops, SyncUpdateType)
	}
	if values.Get("proxy") == "true" {
		ops = append(ops, ProxyUpdateType)
	}
	if values.Get("healthcheck") == "true" {
		ops = append(ops, HealthCheckDurationUpdateType)
	}
	if values.Get("bandwidth") == "true" {
		ops = append(ops, BandwidthLimitUpdateType)
	}
	if values.Get("path") == "true" {
		ops = append(ops, PathUpdateType)
	}
	return ops
}

// UpdateRemoteTarget updates credentials for a remote bucket target
func (adm *AdminClient) UpdateRemoteTarget(ctx context.Context, target *BucketTarget, ops ...TargetUpdateType) (string, error) {
	if target == nil {
		return "", fmt.Errorf("target cannot be nil")
	}
	data, err := json.Marshal(target)
	if err != nil {
		return "", err
	}
	encData, err := EncryptData(adm.getSecretKey(), data)
	if err != nil {
		return "", err
	}
	queryValues := url.Values{}
	queryValues.Set("bucket", target.SourceBucket)
	queryValues.Set("update", "true")

	for _, op := range ops {
		switch op {
		case CredentialsUpdateType:
			queryValues.Set("creds", "true")
		case SyncUpdateType:
			queryValues.Set("sync", "true")
		case ProxyUpdateType:
			queryValues.Set("proxy", "true")
		case BandwidthLimitUpdateType:
			queryValues.Set("bandwidth", "true")
		case HealthCheckDurationUpdateType:
			queryValues.Set("healthcheck", "true")
		case PathUpdateType:
			queryValues.Set("path", "true")
		}
	}

	reqData := requestData{
		relPath:     adminAPIPrefix + "/set-remote-target",
		queryValues: queryValues,
		content:     encData,
	}

	// Execute PUT on /minio/admin/v3/set-remote-target to set a target for this bucket of specific arn type.
	resp, err := adm.executeMethod(ctx, http.MethodPut, reqData)

	defer closeResponse(resp)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		return "", httpRespToErrorResponse(resp)
	}
	b, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	var arn string
	if err = json.Unmarshal(b, &arn); err != nil {
		return "", err
	}
	return arn, nil
}

// RemoveRemoteTarget removes a remote target associated with particular ARN for this bucket
func (adm *AdminClient) RemoveRemoteTarget(ctx context.Context, bucket, arn string) error {
	queryValues := url.Values{}
	queryValues.Set("bucket", bucket)
	queryValues.Set("arn", arn)

	reqData := requestData{
		relPath:     adminAPIPrefix + "/remove-remote-target",
		queryValues: queryValues,
	}

	// Execute PUT on /minio/admin/v3/remove-remote-target to remove a target for this bucket
	// with specific ARN
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
