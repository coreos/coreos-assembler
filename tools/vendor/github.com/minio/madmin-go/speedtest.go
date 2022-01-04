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
	"strconv"
	"time"
)

// SpeedTestStatServer - stats of a server
type SpeedTestStatServer struct {
	Endpoint         string `json:"endpoint"`
	ThroughputPerSec uint64 `json:"throughputPerSec"`
	ObjectsPerSec    uint64 `json:"objectsPerSec"`
	Err              string `json:"err"`
}

// SpeedTestStats - stats of all the servers
type SpeedTestStats struct {
	ThroughputPerSec uint64                `json:"throughputPerSec"`
	ObjectsPerSec    uint64                `json:"objectsPerSec"`
	Servers          []SpeedTestStatServer `json:"servers"`
}

// SpeedTestResult - result of the speedtest() call
type SpeedTestResult struct {
	Version    string `json:"version"`
	Servers    int    `json:"servers"`
	Disks      int    `json:"disks"`
	Size       int    `json:"size"`
	Concurrent int    `json:"concurrent"`
	PUTStats   SpeedTestStats
	GETStats   SpeedTestStats
}

// SpeedtestOpts provide configurable options for speedtest
type SpeedtestOpts struct {
	Size        int           // Object size used in speed test
	Concurrency int           // Concurrency used in speed test
	Duration    time.Duration // Total duration of the speed test
	Autotune    bool          // Enable autotuning
}

// Speedtest - perform speedtest on the MinIO servers
func (adm *AdminClient) Speedtest(ctx context.Context, opts SpeedtestOpts) (chan SpeedTestResult, error) {
	queryVals := make(url.Values)
	queryVals.Set("size", strconv.Itoa(opts.Size))
	queryVals.Set("duration", opts.Duration.String())
	queryVals.Set("concurrent", strconv.Itoa(opts.Concurrency))
	if opts.Autotune {
		queryVals.Set("autotune", "true")
	}
	resp, err := adm.executeMethod(ctx,
		http.MethodPost, requestData{
			relPath:     adminAPIPrefix + "/speedtest",
			queryValues: queryVals,
		})
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, httpRespToErrorResponse(resp)
	}
	ch := make(chan SpeedTestResult)
	go func() {
		defer closeResponse(resp)
		defer close(ch)
		var result SpeedTestResult
		for {
			dec := json.NewDecoder(resp.Body)
			if err := dec.Decode(&result); err != nil {
				return
			}
			select {
			case ch <- result:
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch, nil
}
