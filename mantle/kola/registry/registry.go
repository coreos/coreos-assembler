package registry

// Tests imported for registration side effects. These make up the OS test suite and is explicitly imported from the main package.
import (
	_ "github.com/coreos/coreos-assembler/mantle/kola/tests/coretest"
	_ "github.com/coreos/coreos-assembler/mantle/kola/tests/crio"
	_ "github.com/coreos/coreos-assembler/mantle/kola/tests/etcd"
	_ "github.com/coreos/coreos-assembler/mantle/kola/tests/fips"
	_ "github.com/coreos/coreos-assembler/mantle/kola/tests/ignition"
	_ "github.com/coreos/coreos-assembler/mantle/kola/tests/metadata"
	_ "github.com/coreos/coreos-assembler/mantle/kola/tests/misc"
	_ "github.com/coreos/coreos-assembler/mantle/kola/tests/ostree"
	_ "github.com/coreos/coreos-assembler/mantle/kola/tests/podman"
	_ "github.com/coreos/coreos-assembler/mantle/kola/tests/rhcos"
	_ "github.com/coreos/coreos-assembler/mantle/kola/tests/rpmostree"
	_ "github.com/coreos/coreos-assembler/mantle/kola/tests/upgrade"
)
