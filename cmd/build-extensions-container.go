package main

import (
	"fmt"
	"os/exec"

	cosamodel "github.com/coreos/coreos-assembler/internal/pkg/cosa"
	"github.com/coreos/coreos-assembler/internal/pkg/cosash"
	cosa "github.com/coreos/coreos-assembler/pkg/builds"
	"github.com/pkg/errors"
	"gopkg.in/yaml.v2"

	"crypto/sha256"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"time"
)

// hotfix is an element in hotfixes.yaml which is a repo-locked RPM set.
type hotfix struct {
	// URL for associated bug
	Link string `json:"link"`
	// The operating system major version (e.g. 8 or 9)
	OsMajor string `json:"osmajor"`
	// Repo used to download packages
	Repo string `json:"repo"`
	// Names of associated packages
	Packages []string `json:"packages"`
}

type hotfixData struct {
	Hotfixes []hotfix `json:"hotfixes"`
}

// downloadHotfixes basically just accepts as input a declarative JSON file
// format describing hotfixes, which are repo-locked RPM packages we want to download
// but without any dependencies.
func downloadHotfixes(srcdir, configpath, destdir string) error {
	contents, err := os.ReadFile(configpath)
	if err != nil {
		return err
	}

	var h hotfixData
	if err := yaml.Unmarshal(contents, &h); err != nil {
		return fmt.Errorf("failed to deserialize hotfixes: %w", err)
	}

	fmt.Println("Downloading hotfixes")

	for _, fix := range h.Hotfixes {
		fmt.Printf("Downloading content for hotfix: %s\n", fix.Link)
		// Only enable the repos required for download
		reposdir := filepath.Join(srcdir, "yumrepos")
		argv := []string{"--disablerepo=*", fmt.Sprintf("--enablerepo=%s", fix.Repo), "--setopt=reposdir=" + reposdir, "download"}
		argv = append(argv, fix.Packages...)
		cmd := exec.Command("dnf", argv...)
		cmd.Dir = destdir
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to invoke dnf download: %w", err)
		}
	}

	serializedHotfixes, err := json.Marshal(h)
	if err != nil {
		return err
	}
	err = os.WriteFile(filepath.Join(destdir, "hotfixes.json"), serializedHotfixes, 0o644)
	if err != nil {
		return err
	}

	return nil
}

func generateHotfixes() (string, error) {
	hotfixesTmpdir, err := os.MkdirTemp("", "")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(hotfixesTmpdir)

	variant, err := cosamodel.GetVariant()
	if err != nil {
		return "", err
	}

	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}

	srcdir := filepath.Join(wd, "src")
	p := fmt.Sprintf("%s/config/hotfixes-%s.yaml", srcdir, variant)
	if _, err := os.Stat(p); err == nil {
		err := downloadHotfixes(srcdir, p, hotfixesTmpdir)
		if err != nil {
			return "", fmt.Errorf("failed to download hotfixes: %w", err)
		}
	} else {
		fmt.Printf("No %s found\n", p)
	}

	out := filepath.Join(wd, "tmp/hotfixes.tar")

	// Serialize the hotfix RPMs into a tarball which we can pass via a virtio
	// device to the qemu process.
	cmd := exec.Command("tar", "-c", "-C", hotfixesTmpdir, "-f", out, ".")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err != nil {
		return "", err
	}

	return out, nil
}

func buildExtensionContainer() error {
	cosaBuild, buildPath, err := cosa.ReadBuild("builds", "", "")
	if err != nil {
		return err
	}
	buildID := cosaBuild.BuildID
	fmt.Printf("Generating extensions container for build: %s\n", buildID)

	hotfixPath, err := generateHotfixes()
	if err != nil {
		return fmt.Errorf("generating hotfixes failed: %w", err)
	}

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
		" -drive file=" + hotfixPath + ",if=none,id=hotfixes,format=raw,media=disk,read-only=on" +
		" -device virtio-blk,serial=hotfixes,drive=hotfixes" +
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
