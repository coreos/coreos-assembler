package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func buildWithBuildah() error {
	fmt.Println("Building with container runtime (buildah)...")
	tempdir, err := os.MkdirTemp("/srv/tmp", "build-oci-")
	if err != nil {
		return fmt.Errorf("failed to create temporary directory: %w", err)
	}
	defer os.RemoveAll(tempdir)

	tmpOciArchivePath := filepath.Join(tempdir, "out.ociarchive")

	buildCmd := []string{"/bin/sh", "-c", fmt.Sprintf(
		`cd src/config && \
			buildah build --security-opt=label=disable --cap-add=all --device /dev/fuse \
				--build-arg-file build-args.conf \
				-v $PWD:/run/src \
				-t oci-archive:%s .`,
		tmpOciArchivePath,
	)}

	cmd := exec.Command("/usr/lib/coreos-assembler/cmd-supermin-run", buildCmd...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to build OCI image: %w", err)
	}

	cmd = exec.Command("/usr/lib/coreos-assembler/cmd-import", fmt.Sprintf("oci-archive:%s", tmpOciArchivePath))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to import OCI image: %w", err)
	}
	return nil
}
