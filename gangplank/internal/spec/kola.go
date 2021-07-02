package spec

import (
	"fmt"
	"strings"

	"github.com/spf13/pflag"
)

type kolaTests map[string]Stage

// kolaTestDefinitions contain a map of the kola tests.
var kolaTestDefinitions = kolaTests{
	"basicBios": {
		ID:               "Kola Basic BIOS Test",
		PostCommands:     []string{"cosa kola run --qemu-nvme=true basic"},
		RequireArtifacts: []string{"qemu"},
		ExecutionOrder:   2,
	},
	"basicQemu": {
		ID:               "Kola Basic Qemu",
		PostCommands:     []string{"cosa kola --basic-qemu-scenarios"},
		RequireArtifacts: []string{"qemu"},
		ExecutionOrder:   2,
	},
	"basicUEFI": {
		ID:               "Basic UEFI Test",
		PostCommands:     []string{"cosa kola run --qemu-firmware=uefi basic"},
		RequireArtifacts: []string{"qemu"},
		ExecutionOrder:   2,
	},
	"external": {
		ID:               "Enternal Kola Test",
		PostCommands:     []string{"cosa kola run 'ext.*'"},
		RequireArtifacts: []string{"qemu"},
		ExecutionOrder:   2,
	},
	"long": {
		ID:             "Kola Long Tests",
		PostCommands:   []string{"cosa kola run --parallel 3"},
		ExecutionOrder: 2,
	},
	"upgrade": {
		ID:             "Kola Upgrade Test",
		PostCommands:   []string{"kola run-upgrade --output-dir tmp/kola-upgrade"},
		ExecutionOrder: 2,
	},

	// Metal and live-ISO tests
	"iso": {
		ID:               "Kola ISO Testing",
		PostCommands:     []string{"kola testiso -S"},
		ExecutionOrder:   4,
		RequireArtifacts: []string{"live-iso"},
	},
	"metal4k": {
		ID:               "Kola ISO Testing 4K Disks",
		PostCommands:     []string{"kola testiso -S --qemu-native-4k --scenarios iso-install --output-dir tmp/kola-metal4k"},
		ExecutionOrder:   4,
		RequireArtifacts: []string{"live-iso"},
	},
}

// AddKolaTestFlags adds a StringVar flag for populating supported supported test into a
// string slice.
func AddKolaTestFlags(targetVar *[]string, fs *pflag.FlagSet) {
	tests := []string{}
	for k := range kolaTestDefinitions {
		tests = append(tests, k)
	}
	fs.StringSliceVar(targetVar, "kolaTest",
		[]string{}, fmt.Sprintf("Kola Tests to run [%s]", strings.Join(tests, ",")))
}
