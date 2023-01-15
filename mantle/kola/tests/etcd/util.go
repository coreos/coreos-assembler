// Copyright 2015 CoreOS, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package etcd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"time"

	"github.com/coreos/coreos-assembler/mantle/kola/cluster"
	"github.com/coreos/coreos-assembler/mantle/platform"
	"github.com/coreos/coreos-assembler/mantle/util"
)

// GetClusterHealth polls etcdctl cluster-health command until success
// or maximum retries have been reached. Can be effectively used to
// block a test until the etcd cluster is up and running.
func GetClusterHealth(c cluster.TestCluster, m platform.Machine, csize int) error {
	var err error
	var b []byte

	checker := func() error {
		b, err := c.SSH(m, "etcdctl cluster-health")
		if err != nil {
			return err
		}

		// repsonse should include "healthy" for each machine and for cluster
		if strings.Count(string(b), "healthy") != (csize*2)+1 {
			return fmt.Errorf("unexpected etcdctl output")
		}

		plog.Infof("cluster healthy")
		return nil
	}

	err = util.Retry(15, 10*time.Second, checker)
	if err != nil {
		return fmt.Errorf("health polling failed: %v: %s", err, b)
	}

	return nil
}

// setKeys sets n random keys and values across each machine in a
// cluster and returns these values to later be checked with checkKeys.
// If all the values don't get set due to a machine that is down and
// error is NOT returned. An error is returned if no keys are able to be
// set.
func setKeys(c cluster.TestCluster, n int) (map[string]string, error) {
	var written = map[string]string{}
	for _, m := range c.Machines() {
		for i := 0; i < n; i++ {
			// random key and value, may overwrwite previous sets if
			// collision which is fine
			key := strconv.Itoa(rand.Int())[0:3]
			value := strconv.Itoa(rand.Int())[0:3]

			b, err := c.SSHf(m, "curl -s -w %%{http_code} -s http://127.0.0.1:2379/v2/keys/%v -XPUT -d value=%v", key, value)
			if err != nil {
				return nil, err
			}

			// check for 201 or 200 resp header
			if !bytes.HasSuffix(b, []byte("200")) && !bytes.HasSuffix(b, []byte("201")) {
				continue
			}

			written[key] = value
		}
	}
	if len(written) == 0 {
		return nil, fmt.Errorf("failed to write any keys")
	}

	plog.Infof("wrote %v keys", len(written))
	return written, nil
}

// checkKeys tests that each node in the cluster has the full provided
// key set in keyMap. Quorum get must be used.
func checkKeys(c cluster.TestCluster, keyMap map[string]string) error {
	for i, m := range c.Machines() {
		for k, v := range keyMap {
			cmd := fmt.Sprintf("curl -s http://127.0.0.1:2379/v2/keys/%v?quorum=true", k)

			b, err := c.SSH(m, cmd)
			if err != nil {
				return fmt.Errorf("error curling key: %v", err)
			}

			var jsonMap map[string]interface{}
			err = json.Unmarshal(b, &jsonMap)
			if err != nil {
				return err
			}

			// error code?
			errorCode, ok := jsonMap["errorCode"]
			if ok {
				msg := jsonMap["message"]
				return fmt.Errorf("machine %v errorCode %v: %v: %s", i, errorCode, msg, b)
			}

			node, ok := jsonMap["node"]
			if !ok {
				return fmt.Errorf("retrieving key in CheckKeys, no node in resp")
			}

			n := node.(map[string]interface{})
			value, ok := n["value"]
			if !ok {
				return fmt.Errorf("retrieving key in CheckKeys, no value in resp")
			}

			if value != v {
				return fmt.Errorf("checkKeys got incorrect value! expected:%v got: %v", v, value)
			}
		}
	}
	plog.Infof("checked %v keys", len(keyMap))
	return nil
}
