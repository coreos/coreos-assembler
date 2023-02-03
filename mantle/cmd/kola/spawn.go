// Copyright 2015-2018 CoreOS, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"

	"github.com/coreos/coreos-assembler/mantle/kola"
	"github.com/coreos/coreos-assembler/mantle/platform"
	"github.com/coreos/coreos-assembler/mantle/platform/conf"
	"github.com/coreos/coreos-assembler/mantle/platform/machine/qemu"
)

var (
	cmdSpawn = &cobra.Command{
		RunE:         runSpawn,
		PreRunE:      preRun,
		Use:          "spawn",
		Short:        "spawn a CoreOS instance",
		SilenceUsage: true,
	}

	spawnNodeCount      int
	spawnUserData       string
	spawnDetach         bool
	spawnShell          bool
	spawnReconnect      bool
	spawnIdle           bool
	spawnRemove         bool
	spawnVerbose        bool
	spawnMachineOptions string
	spawnSetSSHKeys     bool
	spawnSSHKeys        []string
	spawnJSONInfoFd     int
)

func init() {
	cmdSpawn.Flags().IntVarP(&spawnNodeCount, "nodecount", "c", 1, "number of nodes to spawn")
	cmdSpawn.Flags().StringVarP(&spawnUserData, "userdata", "u", "", "file containing userdata to pass to the instances")
	cmdSpawn.Flags().BoolVarP(&spawnDetach, "detach", "t", false, "-kv --shell=false --remove=false")
	cmdSpawn.Flags().BoolVarP(&spawnShell, "shell", "s", true, "spawn a shell in an instance before exiting")
	cmdSpawn.Flags().BoolVarP(&spawnReconnect, "reconnect", "", false, "keep trying to reconnect to machine when disconnected")
	cmdSpawn.Flags().BoolVarP(&spawnIdle, "idle", "", false, "idle after starting machines (implies --shell=false)")
	cmdSpawn.Flags().BoolVarP(&spawnRemove, "remove", "r", true, "remove instances after shell exits")
	cmdSpawn.Flags().BoolVarP(&spawnVerbose, "verbose", "v", false, "output information about spawned instances")
	cmdSpawn.Flags().StringVar(&spawnMachineOptions, "qemu-options", "", "experimental: path to QEMU machine options JSON")
	cmdSpawn.Flags().IntVarP(&spawnJSONInfoFd, "json-info-fd", "", -1, "experimental: write JSON information about spawned machines")
	cmdSpawn.Flags().BoolVarP(&spawnSetSSHKeys, "keys", "k", false, "add SSH keys from --key options")
	cmdSpawn.Flags().StringSliceVar(&spawnSSHKeys, "key", nil, "path to SSH public key (default: SSH agent + ~/.ssh/id_{rsa,dsa,ecdsa,ed25519}.pub)")
	root.AddCommand(cmdSpawn)
}

func runSpawn(cmd *cobra.Command, args []string) error {
	var err error

	if spawnDetach {
		spawnSetSSHKeys = true
		spawnVerbose = true
		spawnShell = false
		spawnRemove = false
	}

	if spawnIdle {
		spawnShell = false
	}

	if spawnNodeCount <= 0 {
		return fmt.Errorf("Cluster Failed: nodecount must be one or more")
	}

	if spawnReconnect && !strings.HasPrefix(kolaPlatform, "qemu") {
		return fmt.Errorf("Cannot use --reconnect on non-qemu platforms %v", kolaPlatform)
	}

	var userdata *conf.UserData
	if spawnUserData != "" {
		userbytes, err := os.ReadFile(spawnUserData)
		if err != nil {
			return errors.Wrapf(err, "Reading userdata failed")
		}
		userdata = conf.Unknown(string(userbytes))
	}
	if spawnSetSSHKeys {
		if userdata == nil {
			userdata = conf.EmptyIgnition()
		}
		// If the user explicitly passed empty userdata, the userdata
		// will be non-nil but Empty, and adding SSH keys will
		// silently fail.
		userdata, err = addSSHKeys(userdata)
		if err != nil {
			return err
		}
	}

	outputDir, err = kola.SetupOutputDir(outputDir, kolaPlatform)
	if err != nil {
		return errors.Wrapf(err, "Setup failed")
	}

	flight, err := kola.NewFlight(kolaPlatform)
	if err != nil {
		return errors.Wrapf(err, "Flight failed")
	}
	if spawnRemove {
		defer flight.Destroy()
	}

	cluster, err := flight.NewCluster(&platform.RuntimeConfig{
		OutputDir:        outputDir,
		AllowFailedUnits: true,
		InternetAccess:   true,
	})
	if err != nil {
		return errors.Wrapf(err, "Cluster failed")
	}

	if spawnRemove {
		defer cluster.Destroy()
	}

	var jsonInfoFile *os.File
	if spawnJSONInfoFd >= 0 {
		jsonInfoFile = os.NewFile(uintptr(spawnJSONInfoFd), "json-info")
		if jsonInfoFile == nil {
			return fmt.Errorf("Failed to create *File from fd %d", spawnJSONInfoFd)
		}
		defer jsonInfoFile.Close()
	}

	var someMach platform.Machine
	// XXX: should spawn in parallel
	for i := 0; i < spawnNodeCount; i++ {
		var mach platform.Machine
		var err error
		if spawnVerbose {
			fmt.Println("Spawning machine...")
		}
		// use qemu-specific interface only if needed
		if strings.HasPrefix(kolaPlatform, "qemu") && (spawnMachineOptions != "" || !spawnRemove) {
			machineOpts := platform.QemuMachineOptions{
				DisablePDeathSig: !spawnRemove,
			}
			if spawnMachineOptions != "" {
				b, err := os.ReadFile(spawnMachineOptions)
				if err != nil {
					return errors.Wrapf(err, "Could not read machine options")
				}

				err = json.Unmarshal(b, &machineOpts)
				if err != nil {
					return errors.Wrapf(err, "Could not unmarshal machine options")
				}
			}

			switch qc := cluster.(type) {
			case *qemu.Cluster:
				mach, err = qc.NewMachineWithQemuOptions(userdata, machineOpts)
			default:
				plog.Fatalf("unreachable: qemu cluster %v unknown type", qc)
			}
		} else {
			mach, err = cluster.NewMachine(userdata)
		}
		if err != nil {
			return errors.Wrapf(err, "Spawning instance failed")
		}

		if spawnVerbose {
			fmt.Printf("Machine %v spawned at %v\n", mach.ID(), mach.IP())
		}
		if jsonInfoFile != nil {
			if err := platform.WriteJSONInfo(mach, jsonInfoFile); err != nil {
				return fmt.Errorf("Failed writing JSON info: %v", err)
			}
		}

		someMach = mach
	}

	if spawnShell {
		if spawnRemove {
			reader := strings.NewReader(`PS1="\[\033[0;31m\][bound]\[\033[0m\] $PS1"` + "\n")
			if err := platform.InstallFile(reader, someMach, "/etc/profile.d/kola-spawn-bound.sh"); err != nil {
				return errors.Wrapf(err, "Setting shell prompt failed")
			}
		}
		for {
			var bootID string
			if spawnReconnect {
				if bootID, err = platform.GetMachineBootId(someMach); err != nil {
					return errors.Wrapf(err, "failed getting boot id")
				}
			}
			err = platform.Manhole(someMach)
			if !spawnReconnect {
				return errors.Wrapf(err, "Manhole failed")
			}
			if _, ok := errors.Cause(err).(*ssh.ExitMissingError); ok {
				fmt.Printf("Reconnecting (press Ctrl-C to abort)... ")
				if err = someMach.WaitForReboot(120*time.Second, bootID); err != nil {
					return errors.Wrapf(err, "failed to reboot")
				}
				fmt.Println()
			}
		}
	} else if spawnIdle {
		select {}
	}
	return nil
}

func addSSHKeys(userdata *conf.UserData) (*conf.UserData, error) {
	// if no keys specified, use keys from agent plus ~/.ssh/id_{rsa,dsa,ecdsa,ed25519}.pub
	if len(spawnSSHKeys) == 0 {
		// add keys directly from the agent
		agentEnv := os.Getenv("SSH_AUTH_SOCK")
		if agentEnv != "" {
			f, err := net.Dial("unix", agentEnv)
			if err != nil {
				return nil, fmt.Errorf("Couldn't connect to unix socket %q: %v", agentEnv, err)
			}
			defer f.Close()

			agent := agent.NewClient(f)
			keys, err := agent.List()
			if err != nil {
				return nil, fmt.Errorf("Couldn't talk to ssh-agent: %v", err)
			}
			for _, key := range keys {
				userdata = userdata.AddKey(*key)
			}
		}

		// populate list of key files
		userInfo, err := user.Current()
		if err != nil {
			return nil, err
		}
		for _, name := range []string{"id_rsa.pub", "id_dsa.pub", "id_ecdsa.pub", "id_ed25519.pub"} {
			path := filepath.Join(userInfo.HomeDir, ".ssh", name)
			if _, err := os.Stat(path); err == nil {
				spawnSSHKeys = append(spawnSSHKeys, path)
			}
		}
	}

	// read key files, failing if any are missing
	for _, path := range spawnSSHKeys {
		keybytes, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		pkey, comment, _, _, err := ssh.ParseAuthorizedKey(keybytes)
		if err != nil {
			return nil, err
		}
		key := agent.Key{
			Format:  pkey.Type(),
			Blob:    pkey.Marshal(),
			Comment: comment,
		}
		userdata = userdata.AddKey(key)
	}
	return userdata, nil
}
