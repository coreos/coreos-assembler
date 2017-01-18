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
		Name:      "bootkube.experimentaletcd.smoke",
		Run:       bootkubeSmokeEtcd,
		Platforms: []string{"gce"},
	})

	register.Register(&register.Test{
		Name:      "bootkube.destruct.reboot",
		Run:       rebootMaster,
		Platforms: []string{"gce"},
	})

	register.Register(&register.Test{
		Name:      "bootkube.destruct.delete",
		Run:       deleteAPIServer,
		Platforms: []string{"gce"},
	})

	register.Register(&register.Test{
		Name:      "conformance.bootkube",
		Run:       conformanceBootkube,
		Platforms: []string{"gce"},
	})
}
