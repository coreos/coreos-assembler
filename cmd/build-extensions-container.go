package main

import (
	"fmt"
	"github.com/coreos/coreos-assembler-schema/cosa"
	"github.com/coreos/coreos-assembler/internal/pkg/cosash"

	"crypto/sha256"
	"encoding/json"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
)

func buildExtensionContainer() error {
	lastBuild, buildPath, err := cosa.ReadBuild("builds", "", "")
	if err != nil {
		return err
	}
	buildID := lastBuild.BuildID
	fmt.Printf("Generating extensions container for build: %s\n", buildID)

	arch := cosa.BuilderArch()
	sh, err := cosash.NewCosaSh()
	if err != nil {
		return err
	}
	if _, err := sh.PrepareBuild(); err != nil {
		return err
	}
	targetname := "extensions-container-" + buildID + "." + arch + ".ociarchive"
	process := "runvm -- /usr/lib/coreos-assembler/build-extensions-oscontainer.sh " + arch + " $tmp_builddir/" + targetname
	if err := sh.Process(process); err != nil {
		return err
	}
	// Find the temporary directory allocated by the shell process, and put the OCI archive in its final place
	tmpdir, err := sh.ProcessWithReply("echo $tmp_builddir>&3\n")
	if err != nil {
		return err
	}
	targetPath := filepath.Join(buildPath, targetname)
	err = os.Rename(filepath.Join(tmpdir, targetname), targetPath)
	if err != nil {
		return err
	}
	// Gather metadata of the OCI archive (sha256, size)
	file, err := os.Open(targetPath)
	if err != nil {
		return err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return err
	}
	stat, err := file.Stat()
	if err != nil {
		return err
	}
	sha256sum := fmt.Sprintf("%x", hash.Sum(nil))

	// Update the meta.json to include metadata for our OCI archive
	metapath := filepath.Join(buildPath, "meta.json")
	jsonFile, err := os.Open(metapath)
	if err != nil {
		fmt.Println(err)
	}
	defer jsonFile.Close()
	jsonBytes, err := ioutil.ReadAll(jsonFile)
	if err != nil {
		return err
	}
	var cosaBuild cosa.Build
	err = json.Unmarshal(jsonBytes, &cosaBuild)
	if err != nil {
		return err
	}

	cosaBuild.BuildArtifacts.ExtensionsContainer = &cosa.Artifact{
		Path:            targetname,
		Sha256:          sha256sum,
		SizeInBytes:     float64(stat.Size()),
		SkipCompression: false,
	}

	newBytes, err := json.MarshalIndent(cosaBuild, "", "    ")
	if err != nil {
		return err
	}

	err = ioutil.WriteFile(metapath, newBytes, 0644)
	if err != nil {
		return err
	}
	return nil
}
