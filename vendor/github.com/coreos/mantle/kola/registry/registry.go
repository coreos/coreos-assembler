package registry

// Tests imported for registration side effects. These make up the OS test suite and is explicitly imported from the main package.
import (
	_ "github.com/coreos/mantle/kola/tests/coretest"
	_ "github.com/coreos/mantle/kola/tests/docker"
	_ "github.com/coreos/mantle/kola/tests/etcd"
	_ "github.com/coreos/mantle/kola/tests/flannel"
	_ "github.com/coreos/mantle/kola/tests/fleet"
	_ "github.com/coreos/mantle/kola/tests/ignition/v1"
	_ "github.com/coreos/mantle/kola/tests/ignition/v2"
	_ "github.com/coreos/mantle/kola/tests/kubernetes"
	_ "github.com/coreos/mantle/kola/tests/locksmith"
	_ "github.com/coreos/mantle/kola/tests/metadata"
	_ "github.com/coreos/mantle/kola/tests/misc"
	_ "github.com/coreos/mantle/kola/tests/rkt"
	_ "github.com/coreos/mantle/kola/tests/systemd"
)
