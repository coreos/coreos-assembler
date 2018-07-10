package misc

import (
	"fmt"

	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/register"
)

var (
	urlsToFetch = []string{
		"https://www.example.com/",
		"https://www.wikipedia.org/",
		"https://start.fedoraproject.org/",
	}
)

func init() {
	register.Register(&register.Test{
		Run:              TestTLSFetchURLs,
		ClusterSize:      1,
		Name:             "coreos.tls.fetch-urls",
		ExcludePlatforms: []string{"qemu"},  // Networking outside cluster required
		ExcludeDistros:   []string{"rhcos"}, // wget not included in RHCOS
	})
}

func TestTLSFetchURLs(c cluster.TestCluster) {
	m := c.Machines()[0]

	for _, url := range urlsToFetch {
		c.MustSSH(m, fmt.Sprintf("curl -s -S -m 30 --retry 2 %s", url))
		c.MustSSH(m, fmt.Sprintf("wget -nv -T 30 -t 2 --delete-after %s 2> >(grep -v -- '->' >&2)", url))
	}
}
