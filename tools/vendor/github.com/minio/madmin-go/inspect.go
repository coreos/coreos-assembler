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
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// InspectOptions provides options to Inspect.
type InspectOptions struct {
	Volume, File string
}

// Inspect makes an admin call to download a raw files from disk.
func (adm *AdminClient) Inspect(ctx context.Context, d InspectOptions) (key [32]byte, c io.ReadCloser, err error) {
	path := fmt.Sprintf(adminAPIPrefix + "/inspect-data")
	q := make(url.Values)
	q.Set("volume", d.Volume)
	q.Set("file", d.File)
	resp, err := adm.executeMethod(ctx,
		http.MethodGet, requestData{
			relPath: path,
			queryValues:q,
		},
	)

	if err != nil {
		closeResponse(resp)
		return key, nil, err
	}


	if resp.StatusCode != http.StatusOK {
		return key, nil, httpRespToErrorResponse(resp)
	}

	if resp.Body == nil {
		return key, nil, errors.New("body is nil")
	}
	_, err = io.ReadFull(resp.Body, key[:1])
	if err != nil {
		closeResponse(resp)
		return key, nil, err
	}
	// This is the only version we know.
	if key[0] != 1 {
		return key, nil, errors.New("unknown data version")
	}
	// Read key...
	_, err = io.ReadFull(resp.Body, key[:])
	if err != nil {
		closeResponse(resp)
		return key, nil, err
	}

	// Return body
	return key, resp.Body, nil
}
