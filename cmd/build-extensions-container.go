package main

import (
	"fmt"
	"os/exec"

	"github.com/coreos/coreos-assembler/internal/pkg/cosash"
	cosa "github.com/coreos/coreos-assembler/pkg/builds"
	"github.com/pkg/errors"

	"crypto/sha256"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"time"
)

func buildExtensionContainer() error {
	cosaBuild, buildPath, err := cosa.ReadBuild("builds", "", "")
	if err != nil {
		return err
	}
	buildID := cosaBuild.BuildID
	fmt.Printf("Generating extensions container for build: %s\n", buildID)

	arch := cosa.BuilderArch()
	sh, err := cosash.NewCosaSh()
	if err != nil {
		return err
	}
	if _, err := sh.PrepareBuild("extensions-container"); err != nil {
		return errors.Wrapf(err, "calling prepare_build")
	}
	targetname := cosaBuild.Name + "-" + buildID + "-extensions-container" + "." + arch + ".ociarchive"
	process := "runvm -chardev \"file,id=ociarchiveout,path=${tmp_builddir}/\"" + targetname +
		" -device \"virtserialport,chardev=ociarchiveout,name=ociarchiveout\"" +
		" -- /usr/lib/coreos-assembler/build-extensions-container.sh " + arch +
		" /dev/virtio-ports/ociarchiveout " + buildID
	if err := sh.Process(process); err != nil {
		return errors.Wrapf(err, "calling build-extensions-container.sh")
	}
	// Find the temporary directory allocated by the shell process, and put the OCI archive in its final place
	tmpdir, err := sh.ProcessWithReply("echo $tmp_builddir>&3\n")
	if err != nil {
		return errors.Wrapf(err, "querying tmpdir")
	}
	targetPath := filepath.Join(buildPath, targetname)
	if err := exec.Command("/usr/lib/coreos-assembler/finalize-artifact", filepath.Join(tmpdir, targetname), targetPath).Run(); err != nil {
		return errors.Wrapf(err, "finalizing artifact")
	}

	fmt.Printf("Built %s\n", targetPath)

	// Gather metadata of the OCI archive (sha256, size)
	file, err := os.Open(targetPath)
	if err != nil {
		return errors.Wrapf(err, "opening %s", targetPath)
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return errors.Wrapf(err, "hashing %s", targetPath)
	}
	stat, err := file.Stat()
	if err != nil {
		return errors.Wrapf(err, "stat(%s)", targetPath)
	}
	sha256sum := fmt.Sprintf("%x", hash.Sum(nil))

	cosaBuild.BuildArtifacts.ExtensionsContainer = &cosa.Artifact{
		Path:            targetname,
		Sha256:          sha256sum,
		SizeInBytes:     float64(stat.Size()),
		SkipCompression: true,
	}
	cosaBuild.MetaStamp = float64(time.Now().UnixNano())

	newBytes, err := json.MarshalIndent(cosaBuild, "", "    ")
	if err != nil {
		return err
	}
	extensions_container_meta_path := filepath.Join(buildPath, "meta.extensions-container.json")
	err = os.WriteFile(extensions_container_meta_path, newBytes, 0644)
	if err != nil {
		return errors.Wrapf(err, "writing %s", extensions_container_meta_path)
	}
	defer os.Remove(extensions_container_meta_path)
	workdir, err := filepath.Abs(".")
	if err != nil {
		return err
	}
	abs_new_json, err := filepath.Abs(extensions_container_meta_path)
	if err != nil {
		return err
	}
	// Calling `cosa meta` as it locks the file and we need to make sure no other process writes to the file at the same time.
	// Golang does not appear to have a public api to lock files at the moment. https://github.com/coreos/coreos-assembler/issues/3149
	if err := exec.Command("cosa", "meta", "--workdir", workdir, "--build", buildID, "--artifact-json", abs_new_json).Run(); err != nil {
		return errors.Wrapf(err, "calling `cosa meta`")
	}
	return nil
}
