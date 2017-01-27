package pluton

import "github.com/coreos/mantle/platform"

// A Manager provides higher level management of the underlying Cluster
// platform.
type Manager interface {
	AddMasters(n int) ([]platform.Machine, error)
	AddWorkers(n int) ([]platform.Machine, error)
}
