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
)

// Pool represents pool specific information
type Pool struct {
	Number int      `json:"number"`
	Local  string   `json:"local"`
	Remote []string `json:"remote"`
}

// Pools is a collection of all the pools
type Pools map[string]Pool

// ListPools returns list of pools currently configured and being used
// on the cluster.
func (adm *AdminClient) ListPools(ctx context.Context) (Pools, error) {
	resp, err := adm.executeMethod(ctx, http.MethodGet, requestData{
		relPath: adminAPIPrefix + "/pool/list", // GET <endpoint>/<admin-API>/pool/list
	})
	if err != nil {
		return nil, err
	}
	defer closeResponse(resp)
	if resp.StatusCode != http.StatusOK {
		return nil, httpRespToErrorResponse(resp)
	}
	var pools = make(Pools)
	if err = json.NewDecoder(resp.Body).Decode(&pools); err != nil {
		return nil, err
	}
	return pools, nil
}
