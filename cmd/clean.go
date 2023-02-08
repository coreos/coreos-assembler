// See usage below
package main

import (
	"fmt"

	"github.com/coreos/coreos-assembler/internal/pkg/bashexec"
	"github.com/coreos/coreos-assembler/internal/pkg/cosash"
)

func runClean(argv []string) error {
	const cleanUsage = `Usage: coreos-assembler clean --help
coreos-assembler clean [--all]

Delete all build artifacts.  Use --all to also clean the cache/ directory.
`

	all := false
	for _, arg := range argv {
		switch arg {
		case "h":
		case "--help":
			fmt.Print(cleanUsage)
			return nil
		case "-a":
		case "--all":
			all = true
		default:
			return fmt.Errorf("unrecognized option: %s", arg)
		}
	}

	sh, err := cosash.NewCosaSh()
	if err != nil {
		return err
	}
	// XXX: why do we need to prepare_build here?
	if _, err := sh.PrepareBuild(""); err != nil {
		return err
	}

	if all {
		priv, err := sh.HasPrivileges()
		if err != nil {
			return err
		}
		cmd := "rm -rf cache/*"
		if priv {
			cmd = fmt.Sprintf("sudo %s", cmd)
		}
		if err := bashexec.Run("cleanup cache", cmd); err != nil {
			return err
		}
	} else {
		fmt.Println("Note: retaining cache/")
	}
	return bashexec.Run("cleanup", "rm -rf builds/* tmp/*")
}
