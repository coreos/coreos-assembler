// Copyright 2017 CoreOS, Inc.
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

package upstream

import (
	"fmt"
	"os"

	"github.com/coreos/mantle/pluton"
)

// Runs conformance tests on a master node of a Cluster
// TODO(pb): find a way to stream to a custom output file for the calling test.
// Also only print the last few lines to stdout
func RunConformanceTests(c *pluton.Cluster) error {
	const (
		kcPath  = "/etc/kubernetes/kubeconfig"
		goImage = "docker://golang:1.7.4"
		repo    = "github.com/coreos/kubernetes"
	)

	cmds := []string{
		fmt.Sprintf("git clone https://%s /home/core/k8s", repo),
		"mkdir /home/core/artifacts",
	}
	for _, cmd := range cmds {
		if _, err := c.Masters[0].SSH(cmd); err != nil {
			return err
		}
	}

	runConformance := fmt.Sprintf(`sudo /usr/bin/rkt --insecure-options=image run \
		--volume=kc,kind=host,source=%s \
		--volume=k8s,kind=host,source=/home/core/k8s \
		--mount volume=kc,target=/kubeconfig \
		--mount volume=k8s,target=/go/src/k8s.io/kubernetes \
		%s --exec /bin/bash -- -c "\
		apt-get update && \
		apt-get install -y rsync && \
		go get -u github.com/jteeuwen/go-bindata/go-bindata && \
		cd /go/src/k8s.io/kubernetes && \
 		git checkout %s && \
		make all WHAT=cmd/kubectl && \
 		make all WHAT=vendor/github.com/onsi/ginkgo/ginkgo && \
		make all WHAT=test/e2e/e2e.test && \
		KUBECONFIG=/kubeconfig KUBERNETES_PROVIDER=skeleton KUBERNETES_CONFORMANCE_TEST=Y go run hack/e2e.go \
 		-v --test -check_version_skew=false --test_args='--ginkgo.focus=\[Conformance\]'"`,
		kcPath, goImage, c.Info.Version)

	// stream conformance output
	client, err := c.Masters[0].SSHClient()
	if err != nil {
		return err
	}
	defer client.Close()
	session, err := client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()

	session.Stderr = os.Stderr
	session.Stdout = os.Stdout

	if err := session.Run(runConformance); err != nil {
		return fmt.Errorf("Error in conformance run: %s", err)
	}

	return nil
}
