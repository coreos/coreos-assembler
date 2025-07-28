package main

import (
	"fmt"
	"os"
	"os/exec"
)

func buildContainerNative() error {
	fmt.Println("Building container natively...")
	tempdir, err := os.MkdirTemp("/srv/tmp", "build-oci-")
	if err != nil {
		return fmt.Errorf("failed to create temporary directory: %w", err)
	}
	defer os.RemoveAll(tempdir)

	podmanCmd := []string{"buildah", "build", "--security-opt=label=disable", "--cap-add=all", "--device", "/dev/fuse",
		"--build-arg-file", "src/config/build-args.conf",
		"-v", "/srv/cache:/cache",
		"-v", "/srv/src/config:/run/src",
		"-v", "/srv/tmp:/run/tmp",
		"-t", fmt.Sprintf("oci-archive:%s/out.ociarchive", tempdir),
		"src/config"}

	cmd := exec.Command("/usr/lib/coreos-assembler/cmd-supermin-run", podmanCmd...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to build OCI image: %w", err)
	}

	cmd = exec.Command("/usr/lib/coreos-assembler/cmd-import", fmt.Sprintf("oci-archive:%s/out.ociarchive", tempdir))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to import OCI image: %w", err)
	}
	return nil
}
