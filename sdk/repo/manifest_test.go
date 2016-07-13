// Copyright 2016 CoreOS, Inc.
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

package repo

import (
	"encoding/xml"
	"testing"

	"github.com/kylelemons/godebug/diff"
)

// Test manifest document, this is mostly identical to the output of
// `repo manifest -r` except self-closing tags not used (Go doesn't
// output them) and attribute order is a bit different, Go uses struct
// order but Python alphabetizes.
const testManifest = `<?xml version="1.0" encoding="UTF-8"?>
<manifest>
  <notice>Your sources have been synced successfully.</notice>
  <remote name="cros" fetch="https://chromium.googlesource.com/" review="gerrit.chromium.org/gerrit"></remote>
  <remote name="github" fetch=".."></remote>
  <remote name="private" fetch="ssh://git@github.com"></remote>
  <default remote="github" revision="refs/heads/master" sync-j="4"></default>
  <project name="appc/acbuild" path="src/third_party/appc-acbuild" revision="dd71391585dd0e96e56877d650ff1030cd7d9b01" groups="minilayout" upstream="refs/heads/master"></project>
  <project name="appc/spec" path="src/third_party/appc-spec" revision="62d46939da30111dc3eae51dd36ad5cd146dd964" groups="minilayout" upstream="refs/heads/master"></project>
  <project name="chromiumos/platform/crostestutils" path="src/platform/crostestutils" remote="cros" revision="35331923b30e031a3b3573533bb3b411453d1273" groups="minilayout" upstream="refs/heads/master"></project>
  <project name="chromiumos/platform/factory-utils" path="src/platform/factory-utils" remote="cros" revision="f2e4d8c1e0753c385f34d7be8b3f4ceb3ab17abe" groups="minilayout" upstream="refs/heads/master"></project>
  <project name="chromiumos/repohooks" path="src/repohooks" remote="cros" revision="7a610e823d287f3a1f796100b2a3d11da83de89e" groups="minilayout" upstream="refs/heads/master"></project>
  <project name="chromiumos/third_party/pyelftools" path="chromite/third_party/pyelftools" remote="cros" revision="bdc1d380acd88d4bfaf47265008091483b0d614e" groups="minilayout" upstream="refs/heads/master"></project>
  <project name="coreos/baselayout" path="src/third_party/baselayout" revision="fa6fe343b60a6ca694137048278d06aeeba051b6" groups="minilayout" upstream="refs/heads/master"></project>
  <project name="coreos/bootengine" path="src/third_party/bootengine" revision="09766b249af4190eef69cccd6609cebab7f6a8b4" groups="minilayout" upstream="refs/heads/master"></project>
  <project name="coreos/chromite" path="chromite" revision="f3db21adb76ea48390c5bacc6ae4b70f1037f657" groups="minilayout" upstream="refs/heads/master">
    <copyfile src="AUTHORS" dest="AUTHORS"></copyfile>
    <copyfile src="LICENSE" dest="LICENSE"></copyfile>
  </project>
  <project name="coreos/coreos-buildbot" path="src/third_party/coreos-buildbot" revision="3e4b20f67839aa541839eca6b4b7274d5ad1932c" groups="minilayout" upstream="refs/heads/master"></project>
  <project name="coreos/coreos-cloudinit" path="src/third_party/coreos-cloudinit" revision="b3f805dee6a4aa5ed298a1f370284df470eecf43" groups="minilayout" upstream="refs/heads/master"></project>
  <project name="coreos/coreos-metadata" path="src/third_party/coreos-metadata" revision="d976d664051f5b95ab60f7f1770b1b2bcc2877b2" groups="minilayout" upstream="refs/heads/master"></project>
  <project name="coreos/coreos-overlay" path="src/third_party/coreos-overlay" revision="c6e011295c7e6c8878f95206c706d53d9294122d" groups="minilayout" upstream="refs/heads/master"></project>
  <project name="coreos/coretest" path="src/third_party/coretest" revision="991faaf28eb21f185fed0708b526849a8bc128e6" groups="minilayout" upstream="refs/heads/master"></project>
  <project name="coreos/dev-util" path="src/platform/dev" revision="072c33135839b692c6ceb37765e2e0f1a65b416c" groups="minilayout" upstream="refs/heads/master"></project>
  <project name="coreos/docker" path="src/third_party/docker" revision="9a9bbacae56d55b45c39751148d967e7d5dfcdfc" groups="minilayout" upstream="refs/heads/master"></project>
  <project name="coreos/efunctions" path="src/third_party/efunctions" revision="ecef964cb1eed5c8482ab4c75a23de35fd390584" groups="minilayout" upstream="refs/heads/master"></project>
  <project name="coreos/etcd" path="src/third_party/etcd" revision="bfcd39335c6c27d84164c1b1d9e9d65c2e8f39b6" groups="minilayout" upstream="refs/heads/master"></project>
  <project name="coreos/etcdctl" path="src/third_party/etcdctl" revision="4c3f5c9fb3441991abf950651be977c3e0eef30e" groups="minilayout" upstream="refs/heads/master"></project>
  <project name="coreos/fleet" path="src/third_party/fleet" revision="d605dc00bf2fd4e66f4f79d6ddc56170f53865da" groups="minilayout" upstream="refs/heads/master"></project>
  <project name="coreos/grub" path="src/third_party/grub" revision="4ccc609994fe2f5e0911b91a11ad9e4289dc3a04" groups="minilayout" upstream="refs/heads/master"></project>
  <project name="coreos/ignition" path="src/third_party/ignition" revision="d4250a015b0d9d9c48338a3644ff3c007dfc7e7d" groups="minilayout" upstream="refs/heads/master"></project>
  <project name="coreos/init" path="src/third_party/init" revision="69492d452bc51c4edaa888c69f1fc97fab68c065" groups="minilayout" upstream="refs/heads/master"></project>
  <project name="coreos/installer" path="src/platform/installer" revision="95815a7cc15abea574e1b06d9fd403b90b29ba01" groups="minilayout" upstream="refs/heads/master"></project>
  <project name="coreos/locksmith" path="src/third_party/locksmith" revision="816e4c4cb05525d43c8aad919eddcc32b4e91619" groups="minilayout" upstream="refs/heads/master"></project>
  <project name="coreos/mantle" path="src/third_party/mantle" revision="a6b9288b9078fc02f8ab2f376175abaa20deac5c" groups="minilayout" upstream="refs/heads/master"></project>
  <project name="coreos/mayday" path="src/third_party/mayday" revision="85f8b48da25fd6e3c36a9aa1f7d90c19078777ab" groups="minilayout" upstream="refs/heads/master"></project>
  <project name="coreos/nss-altfiles" path="src/third_party/nss-altfiles" revision="508d986e38c70bd0636740d287d2fe807822fb57" groups="minilayout" upstream="refs/heads/master"></project>
  <project name="coreos/portage-stable" path="src/third_party/portage-stable" revision="b9a47e57b74de596df9bf5da28b85aad105781e3" groups="minilayout" upstream="refs/heads/master"></project>
  <project name="coreos/rkt" path="src/third_party/rkt" revision="debc46e5c8b4f1e8519033f5c5ffefe07f7bc3fe" groups="minilayout" upstream="refs/heads/master"></project>
  <project name="coreos/scripts" path="src/scripts" revision="b77aa4d24876ed86de834e5dc3c715eaae1ddc92" groups="minilayout" upstream="refs/heads/master"></project>
  <project name="coreos/sdnotify-proxy" path="src/third_party/sdnotify-proxy" revision="bfd0269267d91f3bbe89db49ec8ea8903ae8aa3c" groups="minilayout" upstream="refs/heads/master"></project>
  <project name="coreos/seismograph" path="src/third_party/seismograph" revision="a96246842fe43d410cb8a69daef0d96c8fd56a21" groups="minilayout" upstream="refs/heads/master"></project>
  <project name="coreos/shim" path="src/third_party/shim" revision="03a1513b0985fd682b13a8d29fe3f1314a704c66" groups="minilayout" upstream="refs/heads/master"></project>
  <project name="coreos/sysroot-wrappers" path="src/third_party/sysroot-wrappers" revision="437a7a86a482348828423ffd016b379fb70b0445" groups="minilayout" upstream="refs/heads/master"></project>
  <project name="coreos/systemd" path="src/third_party/systemd" revision="e859aa9e993453be321450148d45d08fcc55c3f5" groups="minilayout" upstream="refs/heads/master"></project>
  <project name="coreos/toolbox" path="src/third_party/toolbox" revision="45f497d12139b6d823a070cfab7724ead0b8bedd" groups="minilayout" upstream="refs/heads/master"></project>
  <project name="coreos/update_engine" path="src/third_party/update_engine" revision="fb89e2500e5b1f31d227db5f82b30c1a87113a12" groups="minilayout" upstream="refs/heads/master"></project>
  <project name="coreos/updateservicectl" path="src/third_party/updateservicectl" revision="0842a025368e7ad9903bc70fcf5aaf06e1f39652" groups="minilayout" upstream="refs/heads/master"></project>
  <repo-hooks in-project="chromiumos/repohooks" enabled-list="pre-upload"></repo-hooks>
</manifest>`

func TestMarshal(t *testing.T) {
	var manifest Manifest
	if err := xml.Unmarshal([]byte(testManifest), &manifest); err != nil {
		t.Fatal(err)
	}

	out, err := xml.MarshalIndent(&manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}

	testResult := xml.Header + string(out)
	if d := diff.Diff(testManifest, testResult); d != "" {
		t.Fatalf("Unexpected XML:\n%s", d)
	}
}
