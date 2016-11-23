package bootkube

import (
	"github.com/coreos/pkg/capnslog"

	"github.com/coreos/mantle/kola/register"
)

var plog = capnslog.NewPackageLogger("github.com/coreos-inc/k8s-kola", "tests/bootkube")

func init() {
	register.Register(&register.Test{
		Name:        "bootkube.smoke",
		Run:         bootkubeSmoke,
		Platforms:   []string{"gce"},
		ClusterSize: 0,
	})
	register.Register(&register.Test{
		Name:        "bootkube.destruction",
		Run:         bootkubeDestruction,
		Platforms:   []string{"gce"},
		ClusterSize: 0,
	})

}
