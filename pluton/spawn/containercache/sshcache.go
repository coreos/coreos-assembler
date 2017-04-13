package containercache

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/util"
)

const (
	cacheDir          = "/home/core/containercache"
	downloadRetries   = 3
	downloadRetryWait = 15 * time.Second
	downloadTimeout   = 2 * time.Minute
)

var once sync.Once

type ImageName struct {
	Name   string
	Engine string // docker or rkt
}

// StartBastionOnce will populate a bastion machine with the given containers
// for rkt and docker. Since its best to run this only once per test suite, the
// bastion machine should be in a separate platform.Cluster then the one used
// in the test so it can be available for all tests in a given run. This
// function is guarded with a sync.Once, so only the first within a test suite
// it will have it run.
func StartBastionOnce(bastion platform.Machine, names []ImageName) error {
	var err error

	f := func() {
		err = startBastion(bastion, names)
	}
	once.Do(f)

	return err
}

// Load all containers cached by the bastion node into the docker and rkt store
// of the given machines.
func Load(bastion platform.Machine, machines []platform.Machine) error {
	if len(machines) < 0 {
		return nil
	}

	// error if bastion is not setup
	if err := StartBastionOnce(nil, nil); err != nil {
		return fmt.Errorf("Must call StartBastionOnce before Load")
	}

	if err := copyPublicKeys(bastion, machines); err != nil {
		return fmt.Errorf("copying public keys: %v", err)
	}

	var wg sync.WaitGroup
	var errors []error

	wg.Add(len(machines))

	for _, m := range machines {
		go func(m platform.Machine) {
			defer wg.Done()

			if err := transferContainers(bastion, m); err != nil {
				errors = append(errors, err)
			}
		}(m)
	}
	wg.Wait()

	if len(errors) != 0 {
		return fmt.Errorf("%s", errors)
	}

	return nil
}

func startBastion(bastion platform.Machine, names []ImageName) error {
	if bastion == nil {
		return fmt.Errorf("error starting containercache: bastion is nil")
	}

	// generate key-pair
	out, err := bastion.SSH(`ssh-keygen -t ed25519 -N "" -f ./.ssh/bastion.key`)
	if err != nil {
		return fmt.Errorf("%v: %s", err, out)
	}

	start := time.Now()

	//fetch rkt and docker images and retry on failures
	//TODO: log failures via harness logs along with stderr
	for _, name := range names {
		switch name.Engine {
		case "rkt":
			rktFetch := func() error {
				err := sshWithTimeout(bastion, fmt.Sprintf("sudo rkt fetch %s --trust-keys-from-https", name.Name), downloadTimeout)
				if err != nil {
					fmt.Printf("failure or timeout fetching %v, retrying...\n", name.Name)
					return err
				}
				return nil
			}

			start := time.Now()

			if err := util.Retry(downloadRetries, downloadRetryWait, rktFetch); err != nil {
				return err
			}

			elasped := time.Since(start)
			fmt.Printf("Extracting and fetching %v took %v\n", name.Name, elasped)

		case "docker":
			dockerFetch := func() error {
				err := sshWithTimeout(bastion, fmt.Sprintf("docker pull %s", name.Name), downloadTimeout)
				if err != nil {
					fmt.Printf("failure or timeout pulling %v, retrying...\n", name.Name)
					return err
				}
				return nil
			}

			start := time.Now()

			if err := util.Retry(downloadRetries, downloadRetryWait, dockerFetch); err != nil {
				return err
			}

			elasped := time.Since(start)
			fmt.Printf("Extracting and pulling %v took %v\n", name.Name, elasped)

		default:
			return fmt.Errorf("invalid container name Engine must either be 'rkt' or 'docker' got %v", name.Engine)
		}
	}

	elasped := time.Since(start)
	fmt.Printf("Total prefetch time took %v\n", elasped)

	start = time.Now()

	// extract containers to known location for Load to pick up later
	err = extract(bastion, names)
	if err != nil {
		return fmt.Errorf("error extracting containers: %v", err)
	}

	elasped = time.Since(start)
	fmt.Printf("Total time extracting prefetched containers to disk: %v\n", elasped)

	return nil
}

// Will add a timeout to the SSH command
func sshWithTimeout(m platform.Machine, cmd string, timeout time.Duration) error {
	client, err := m.SSHClient()
	if err != nil {
		return err
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()

	var outBuf = bytes.NewBuffer(nil)
	var errBuf = bytes.NewBuffer(nil)
	session.Stdout = outBuf
	session.Stderr = errBuf

	err = session.Start(cmd)
	if err != nil {
		return err
	}

	errc := make(chan error)
	go func() { errc <- session.Wait() }()

	select {
	case err := <-errc:
		if err != nil {
			return fmt.Errorf("error prefetching cmd: %v -- %s", cmd, append(outBuf.Bytes(), errBuf.Bytes()...))
		}
	case <-time.After(timeout):
		return fmt.Errorf("timeout prefetching: %v -- %s", cmd, append(outBuf.Bytes(), errBuf.Bytes()...))
	}

	return nil // unreachable
}

// Extract existing containers to fs
func extract(bastion platform.Machine, names []ImageName) error {
	out, err := bastion.SSH(fmt.Sprintf("mkdir %v", cacheDir))
	if err != nil {
		return fmt.Errorf("creating cache dir: %v -- %s", err, out)
	}

	for _, name := range names {
		switch name.Engine {
		case "rkt":
			out, err = bastion.SSH(fmt.Sprintf("sudo rkt image export %v %v/%v.aci", name.Name, cacheDir, strings.Replace(name.Name, "/", ".", -1)))
			if err != nil {
				return fmt.Errorf("rkt image export: %v -- %s", err, out)
			}
		case "docker":
			out, err = bastion.SSH(fmt.Sprintf("docker save %v -o %v/%v.tar", name.Name, cacheDir, strings.Replace(name.Name, "/", ".", -1)))
			if err != nil {
				return fmt.Errorf("docker save: %v -- %s", err, out)
			}
		default:
			return fmt.Errorf("invalid container name Engine must either be 'rkt' or 'docker' got %v", name.Engine)
		}
	}

	out, err = bastion.SSH("sudo chown -R core " + cacheDir)
	if err != nil {
		return fmt.Errorf("%v: %s", err, out)
	}

	return nil
}

func copyPublicKeys(bastion platform.Machine, machines []platform.Machine) error {
	for _, m := range machines {
		err := platform.TransferFile(bastion, "/home/core/.ssh/bastion.key.pub", m, "/home/core/bastion.key.pub")
		if err != nil {
			return err
		}

		out, err := m.SSH("update-ssh-keys -a bastion /home/core/bastion.key.pub")
		if err != nil {
			return fmt.Errorf("%v: %s", err, out)
		}
	}

	return nil
}

func transferContainers(bastion, dst platform.Machine) error {
	scpCmd := fmt.Sprintf("sudo scp -rqB -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -i /home/core/.ssh/bastion.key %v core@%v:%v", cacheDir, dst.PrivateIP(), cacheDir)
	out, err := bastion.SSH(scpCmd)
	if err != nil {
		return fmt.Errorf("scp'ing container : %v -- %s", err, out)
	}

	// get file list to import
	out, err = dst.SSH(fmt.Sprintf("ls -m %v", cacheDir))
	if err != nil {
		return fmt.Errorf("ls -m : %v -- %s", err, out)
	}
	var files []string
	for _, s := range strings.Split(string(out), ",") {
		files = append(files, filepath.Join(cacheDir, strings.TrimSpace(s)))
	}

	// fetch/import files
	for _, f := range files {
		if strings.HasSuffix(f, "aci") {
			dst.SSH(fmt.Sprintf("sudo rkt fetch file://%v --insecure-options=image", f))
		} else if strings.HasSuffix(f, "tar") {
			dst.SSH(fmt.Sprintf("docker load -i %v", f))
		} else {
			return fmt.Errorf("can't import file: %v", f)
		}
	}
	return nil

}
