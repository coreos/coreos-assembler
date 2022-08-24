// See usage below
package main

import (
	"fmt"
	"os"
)

func runUpdateVariant(argv []string) error {
	const updateVariantUsage = `Usage: coreos-assembler update-variant --help <variant> <version>

Update symlinks for manifests in the config repo to use the specified version
for the given variant.

Use the "default" variant to update the default manifests with a variant suffix.

Example to update the rhel-coreos-9 variant to RHEL 9.2:
$ coreos-assembler update-variant rhel-coreos-9 rhel-9.2

Example to set SCOS as the default manifest:
$ coreos-assembler update-variant default scos
`
	for _, arg := range argv {
		switch arg {
		case "-h":
		case "--help":
			fmt.Print(updateVariantUsage)
			return nil
		}
	}

	if len(argv) != 2 {
		fmt.Print(updateVariantUsage)
		return nil
	}
	variant := argv[0]
	version := argv[1]

	var suffix string
	if variant == "default" {
		suffix = ""
	} else {
		suffix = fmt.Sprintf("-%s", variant)
	}

	manifests := [3]string{"image", "extensions", "manifest"}
	for _, m := range manifests {
		target := fmt.Sprintf("%s-%s.yaml", m, version)
		linkname := fmt.Sprintf("%s%s.yaml", m, suffix)
		_, err := os.Stat(fmt.Sprintf("src/config/%s", target))
		if err != nil {
			return err
		}
		err = os.Remove(fmt.Sprintf("src/config/%s", linkname))
		if err != nil {
			return err
		}
		err = os.Symlink(target, fmt.Sprintf("src/config/%s", linkname))
		if err != nil {
			return err
		}
	}
	return nil
}
