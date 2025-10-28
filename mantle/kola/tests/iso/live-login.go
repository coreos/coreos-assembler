package testiso

import (
	"bufio"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/coreos/coreos-assembler/mantle/kola"
	"github.com/coreos/coreos-assembler/mantle/platform"
	"github.com/pkg/errors"

	"github.com/coreos/coreos-assembler/mantle/kola/cluster"
	"github.com/coreos/coreos-assembler/mantle/kola/register"
	"github.com/coreos/coreos-assembler/mantle/platform/conf"
	"github.com/coreos/coreos-assembler/mantle/platform/machine/qemu"
)

func init() {
	register.RegisterTest(&register.Test{
		Run:                  isoLiveLogin,
		ClusterSize:          0,
		Name:                 "iso.live-login",
		Description:          "Verify ISO live login works.",
		Flags:                []register.Flag{},
		Platforms:            []string{"qemu"},
		ExcludeArchitectures: []string{},
	})
	register.RegisterTest(&register.Test{
		Run:                  isoLiveLoginUefi,
		ClusterSize:          0,
		Name:                 "iso.live-login.uefi",
		Description:          "Verify ISO live login works.",
		Flags:                []register.Flag{},
		Platforms:            []string{"qemu"},
		ExcludeArchitectures: []string{"s390x", "ppcfw"},
	})
	register.RegisterTest(&register.Test{
		Run:                  isoLiveLoginUefiSecure,
		ClusterSize:          0,
		Name:                 "iso.live-login.uefi-secure",
		Description:          "Verify ISO live login works.",
		Flags:                []register.Flag{},
		Platforms:            []string{"qemu"},
		ExcludeArchitectures: []string{"s390x", "ppcfw"},
	})
}

func testLiveLogin(c cluster.TestCluster, enableUefi bool, enableUefiSecure bool) {
	if kola.CosaBuild.Meta.BuildArtifacts.LiveIso == nil || kola.CosaBuild.Meta.BuildArtifacts.LiveKernel == nil {
		c.Fatalf("Build %s is missing live artifacts\n", kola.CosaBuild.Meta.Name)
	}

	butane := conf.Butane(`
variant: fcos
version: 1.1.0`)

	errchan := make(chan error)
	overrideFW := func(builder *platform.QemuBuilder) error {
		switch {
		case enableUefiSecure:
			builder.Firmware = "uefi-secure"
		case enableUefi:
			builder.Firmware = "uefi"
		}
		return nil
	}
	setupDisks := func(_ platform.QemuMachineOptions, builder *platform.QemuBuilder) error {
		// https://github.com/coreos/fedora-coreos-config/blob/testing-devel/overlay.d/05core/usr/lib/systemd/system/coreos-liveiso-success.service
		output, err := builder.VirtioChannelRead("coreos.liveiso-success")
		if err != nil {
			return errors.Wrap(err, "setting up virtio-serial channel")
		}

		// Read line in a goroutine and send errors to channel
		go func() {
			exp := "coreos-liveiso-success"
			line, err := bufio.NewReader(output).ReadString('\n')
			if err != nil {
				if err == io.EOF {
					// this may be from QEMU getting killed or exiting; wait a bit
					// to give a chance for .Wait() above to feed the channel with a
					// better error
					time.Sleep(1 * time.Second)
					errchan <- fmt.Errorf("Got EOF from completion channel, %s expected", exp)
				} else {
					errchan <- errors.Wrapf(err, "reading from completion channel")
				}
				return
			}
			line = strings.TrimSpace(line)
			if line != exp {
				errchan <- fmt.Errorf("Unexpected string from completion channel: %q, expected: %q", line, exp)
				return
			}
			// OK!
			errchan <- nil
		}()

		isopath := filepath.Join(kola.CosaBuild.Dir, kola.CosaBuild.Meta.BuildArtifacts.LiveIso.Path)
		return builder.AddIso(isopath, "", false)
	}

	switch pc := c.Cluster.(type) {
	case *qemu.Cluster:
		callacks := qemu.BuilderCallbacks{SetupDisks: setupDisks, OverrideDefaults: overrideFW}
		_, err := pc.NewMachineWithQemuOptionsAndBuilderCallbacks(butane, platform.QemuMachineOptions{}, callacks)
		if err != nil {
			c.Fatalf("Unable to create test machine: %v", err)
		}
	default:
		c.Fatalf("Unsupported cluster type")
	}

	err := <-errchan
	if err != nil {
		c.Fatal(err)
	}
}

func isoLiveLogin(c cluster.TestCluster) {
	testLiveLogin(c, false, false)
}

func isoLiveLoginUefi(c cluster.TestCluster) {
	testLiveLogin(c, true, false)
}
func isoLiveLoginUefiSecure(c cluster.TestCluster) {
	testLiveLogin(c, false, true)
}
