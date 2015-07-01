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
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/coreos/mantle/platform"
)

// run etcd on each cluster machine
func startEtcd2(m platform.Machine) error {
	etcdStart := "sudo systemctl start etcd2.service"
	_, err := m.SSH(etcdStart)
	if err != nil {
		return fmt.Errorf("start etcd2.service on %v failed: %s", m.IP(), err)
	}
	return nil
}

// stop etcd on each cluster machine
func stopEtcd2(m platform.Machine) error {
	// start etcd instance
	etcdStop := "sudo systemctl stop etcd2.service"
	_, err := m.SSH(etcdStop)
	if err != nil {
		return fmt.Errorf("stop etcd.2service on failed: %s", err)
	}
	return nil
}

// setKeys sets n random keys and values across each machine in a
// cluster and returns these values to later be checked with checkKeys.
// If all the values don't get set due to a machine that is down and
// error is NOT returned. An error is returned if no keys are able to be
// set.
func setKeys(cluster platform.Cluster, n int) (map[string]string, error) {
	var written = map[string]string{}
	for _, m := range cluster.Machines() {
		for i := 0; i < n; i++ {
			// random key and value, may overwrwite previous sets if
			// collision which is fine
			key := strconv.Itoa(rand.Int())[0:3]
			value := strconv.Itoa(rand.Int())[0:3]

			cmd := cluster.NewCommand("curl", "-w", "%{http_code}", "-s", fmt.Sprintf("http://%v:2379/v2/keys/%v", m.IP(), key), "-XPUT", "-d", "value="+value)
			b, err := cmd.Output()
			if err != nil {
				continue
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
func checkKeys(cluster platform.Cluster, keyMap map[string]string) error {
	for i, m := range cluster.Machines() {
		for k, v := range keyMap {
			cmd := cluster.NewCommand("curl", fmt.Sprintf("http://%v:2379/v2/keys/%v?quorum=true", m.IP(), k))
			b, err := cmd.Output()
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

// replace default binary for etcd2.service with given binary
func replaceEtcd2Bin(m platform.Machine, newPath string) error {
	if !filepath.IsAbs(newPath) {
		return fmt.Errorf("newPath must be an absolute filepath")
	}

	override := "\"[Service]\nExecStart=\nExecStart=" + newPath
	override += "\nEnvironment=ETCD_SNAPSHOT_COUNT=10" + "\"" // make it easy to trigger snapshots

	_, err := m.SSH(fmt.Sprintf("echo %v | sudo tee /run/systemd/system/etcd2.service.d/99-exec.conf", override))
	if err != nil {
		return err
	}
	_, err = m.SSH("sudo systemctl daemon-reload")
	if err != nil {
		return err
	}
	return nil
}

func checkEtcdVersion(cluster platform.Cluster, m platform.Machine, expected string) error {
	const (
		retries   = 5
		retryWait = 3 * time.Second
	)
	var err error
	var b []byte

	for i := 0; i < retries; i++ {
		cmd := cluster.NewCommand("curl", "-L", fmt.Sprintf("http://%v:2379/version", m.IP()))
		b, err = cmd.Output()
		if err != nil {
			plog.Infof("retrying version check, hit failure %v", err)
			time.Sleep(retryWait)
			continue
		}
		break
	}
	if err != nil {
		return fmt.Errorf("curling version: %v", err)
	}

	plog.Infof("got version: %s", b)

	if string(b) != expected {
		return fmt.Errorf("expected %v, got %s", expected, b)
	}
	return nil
}

// poll cluster-health until result
func getClusterHealth(m platform.Machine, csize int) error {
	const (
		retries   = 5
		retryWait = 3 * time.Second
	)
	var err error
	var b []byte

	for i := 0; i < retries; i++ {
		b, err = m.SSH("etcdctl cluster-health")
		if err != nil {
			plog.Debugf("retrying health check, hit failure %v", err)
			time.Sleep(retryWait)
			continue
		}

		// repsonse should include "healthy" for each machine and for cluster
		if strings.Count(string(b), "healthy") == csize+1 {
			plog.Infof("cluster healthy")
			return nil
		}
	}

	if err != nil {
		return fmt.Errorf("health polling failed: %v: %s", err, b)
	} else {
		return fmt.Errorf("status unhealthy or incomplete: %s", b)
	}
}
