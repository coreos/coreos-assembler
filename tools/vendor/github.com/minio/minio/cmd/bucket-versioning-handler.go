// Copyright (c) 2015-2021 MinIO, Inc.
//
// This file is part of MinIO Object Storage stack
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package cmd

import (
	"encoding/xml"
	"io"
	"net/http"

	humanize "github.com/dustin/go-humanize"
	"github.com/gorilla/mux"
	"github.com/minio/minio/internal/bucket/versioning"
	"github.com/minio/minio/internal/logger"
	"github.com/minio/pkg/bucket/policy"
)

const (
	bucketVersioningConfig = "versioning.xml"

	// Maximum size of bucket versioning configuration payload sent to the PutBucketVersioningHandler.
	maxBucketVersioningConfigSize = 1 * humanize.MiByte
)

// PutBucketVersioningHandler - PUT Bucket Versioning.
// ----------
func (api objectAPIHandlers) PutBucketVersioningHandler(w http.ResponseWriter, r *http.Request) {
	ctx := newContext(r, w, "PutBucketVersioning")

	defer logger.AuditLog(ctx, w, r, mustGetClaimsFromToken(r))

	vars := mux.Vars(r)
	bucket := vars["bucket"]

	objectAPI := api.ObjectAPI()
	if objectAPI == nil {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(ErrServerNotInitialized), r.URL)
		return
	}

	if s3Error := checkRequestAuthType(ctx, r, policy.PutBucketVersioningAction, bucket, ""); s3Error != ErrNone {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(s3Error), r.URL)
		return
	}

	v, err := versioning.ParseConfig(io.LimitReader(r.Body, maxBucketVersioningConfigSize))
	if err != nil {
		writeErrorResponse(ctx, w, toAPIError(ctx, err), r.URL)
		return
	}

	if rcfg, _ := globalBucketObjectLockSys.Get(bucket); rcfg.LockEnabled && v.Suspended() {
		writeErrorResponse(ctx, w, APIError{
			Code:           "InvalidBucketState",
			Description:    "An Object Lock configuration is present on this bucket, so the versioning state cannot be changed.",
			HTTPStatusCode: http.StatusConflict,
		}, r.URL)
		return
	}
	if _, err := getReplicationConfig(ctx, bucket); err == nil && v.Suspended() {
		writeErrorResponse(ctx, w, APIError{
			Code:           "InvalidBucketState",
			Description:    "A replication configuration is present on this bucket, so the versioning state cannot be changed.",
			HTTPStatusCode: http.StatusConflict,
		}, r.URL)
		return
	}

	configData, err := xml.Marshal(v)
	if err != nil {
		writeErrorResponse(ctx, w, toAPIError(ctx, err), r.URL)
		return
	}

	if err = globalBucketMetadataSys.Update(bucket, bucketVersioningConfig, configData); err != nil {
		writeErrorResponse(ctx, w, toAPIError(ctx, err), r.URL)
		return
	}

	writeSuccessResponseHeadersOnly(w)
}

// GetBucketVersioningHandler - GET Bucket Versioning.
// ----------
func (api objectAPIHandlers) GetBucketVersioningHandler(w http.ResponseWriter, r *http.Request) {
	ctx := newContext(r, w, "GetBucketVersioning")

	defer logger.AuditLog(ctx, w, r, mustGetClaimsFromToken(r))

	vars := mux.Vars(r)
	bucket := vars["bucket"]

	objectAPI := api.ObjectAPI()
	if objectAPI == nil {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(ErrServerNotInitialized), r.URL)
		return
	}

	if s3Error := checkRequestAuthType(ctx, r, policy.GetBucketVersioningAction, bucket, ""); s3Error != ErrNone {
		writeErrorResponse(ctx, w, errorCodes.ToAPIErr(s3Error), r.URL)
		return
	}

	// Check if bucket exists.
	if _, err := objectAPI.GetBucketInfo(ctx, bucket); err != nil {
		writeErrorResponse(ctx, w, toAPIError(ctx, err), r.URL)
		return
	}

	config, err := globalBucketVersioningSys.Get(bucket)
	if err != nil {
		writeErrorResponse(ctx, w, toAPIError(ctx, err), r.URL)
		return
	}

	configData, err := xml.Marshal(config)
	if err != nil {
		writeErrorResponse(ctx, w, toAPIError(ctx, err), r.URL)
		return
	}

	// Write bucket versioning configuration to client
	writeSuccessResponseXML(w, configData)

}
