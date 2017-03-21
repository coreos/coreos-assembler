package harness

import (
	"github.com/coreos/mantle/harness"
	"github.com/coreos/mantle/pluton"
)

var Tests harness.Tests

func Register(test pluton.Test) {
	Tests.Add(test.Name, func(h *harness.H) {
		runTest(test, h)
	})
}
