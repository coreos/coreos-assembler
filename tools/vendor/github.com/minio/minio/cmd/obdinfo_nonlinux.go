// +build !linux

/*
 * MinIO Cloud Storage, (C) 2020 MinIO, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 */

package cmd

import (
	"context"
	"net/http"
	"runtime"

	"github.com/minio/minio/pkg/madmin"
)

func getLocalDiskHwOBD(ctx context.Context, r *http.Request) madmin.ServerDiskHwOBDInfo {
	addr := r.Host
	if globalIsDistErasure {
		addr = GetLocalPeer(globalEndpoints)
	}

	return madmin.ServerDiskHwOBDInfo{
		Addr:  addr,
		Error: "unsupported platform: " + runtime.GOOS,
	}
}

func getLocalOsInfoOBD(ctx context.Context, r *http.Request) madmin.ServerOsOBDInfo {
	addr := r.Host
	if globalIsDistErasure {
		addr = GetLocalPeer(globalEndpoints)
	}

	return madmin.ServerOsOBDInfo{
		Addr:  addr,
		Error: "unsupported platform: " + runtime.GOOS,
	}
}
