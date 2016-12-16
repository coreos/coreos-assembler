package bootkube

import (
	"github.com/coreos/pkg/capnslog"

	"github.com/coreos/mantle/kola/register"
)

var plog = capnslog.NewPackageLogger("github.com/coreos-inc/pluton", "tests/bootkube")

func init() {
	register.Register(&register.Test{
		Name:      "bootkube.smoke",
		Run:       bootkubeSmoke,
		Platforms: []string{"gce"},
	})

	register.Register(&register.Test{
		Name:      "bootkube.destruction",
		Run:       bootkubeDestruction,
		Platforms: []string{"gce"},
	})

	register.Register(&register.Test{
		Name:      "conformance.bootkube",
		Run:       conformanceBootkube,
		Platforms: []string{"gce"},
	})
}
