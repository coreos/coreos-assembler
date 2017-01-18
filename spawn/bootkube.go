package spawn

import (
	"bytes"
	"fmt"
	"regexp"
	"text/template"
	"time"

	"github.com/coreos-inc/pluton"

	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/tests/etcd"
	"github.com/coreos/mantle/platform"
	"github.com/coreos/pkg/capnslog"
)

var plog = capnslog.NewPackageLogger("github.com/coreos-inc/pluton", "spawn")

// MakeSimpleCluster brings up a multi node bootkube cluster with static etcd
// and checks that all nodes are registered before returning. NOTE: If startup
// times become too long there are a few sections of this setup that could be
// run in parallel.
func MakeBootkubeCluster(c cluster.TestCluster, workerNodes int) (*pluton.Cluster, error) {
	// options from flags set by main package
	var (
		imageRepo       = c.Options["BootkubeRepo"]
		imageTag        = c.Options["BootkubeTag"]
		kubeletImageTag = c.Options["HostKubeletTag"]
	)

	// provision master node running etcd
	masterConfig, err := renderCloudConfig(kubeletImageTag, true)
	if err != nil {
		return nil, err
	}
	master, err := c.NewMachine(masterConfig)
	if err != nil {
		return nil, err
	}
	if err := etcd.GetClusterHealth(master, 1); err != nil {
		return nil, err
	}
	plog.Infof("Master VM (%s) started. It's IP is %s.", master.ID(), master.IP())

	// start bootkube on master
	if err := startMaster(master, imageRepo, imageTag); err != nil {
		return nil, err
	}

	// provision workers
	workerConfig, err := renderCloudConfig(kubeletImageTag, false)
	if err != nil {
		return nil, err
	}

	workerConfigs := make([]string, workerNodes)
	for i := range workerConfigs {
		workerConfigs[i] = workerConfig
	}

	workers, err := platform.NewMachines(c, workerConfigs)
	if err != nil {
		return nil, err
	}

	// start bootkube on workers
	if err := startWorkers(workers, master, kubeletImageTag); err != nil {
		return nil, err
	}

	// install kubectl on master
	if err := installKubectl(master, kubeletImageTag); err != nil {
		return nil, err
	}

	bootkubeCluster := &pluton.Cluster{
		Masters: []platform.Machine{master},
		Workers: workers,
	}

	// check that all nodes appear in kubectl
	if err := bootkubeCluster.NodeCheck(10); err != nil {
		return nil, err
	}

	return bootkubeCluster, nil
}

func renderCloudConfig(kubeletImageTag string, isMaster bool) (string, error) {
	config := struct {
		Master         bool
		KubeletVersion string
	}{
		isMaster,
		kubeletImageTag,
	}

	buf := new(bytes.Buffer)

	tmpl, err := template.New("nodeConfig").Parse(cloudConfigTmpl)
	if err != nil {
		return "", err
	}
	if err := tmpl.Execute(buf, &config); err != nil {
		return "", err
	}

	return buf.String(), nil
}

func startMaster(m platform.Machine, imageRepo, imageTag string) error {
	var cmds = []string{
		// disable selinux or rkt run commands fail in odd ways
		"sudo setenforce 0",

		// render assets
		fmt.Sprintf(`sudo /usr/bin/rkt run \
		--volume home,kind=host,source=/home/core \
		--mount volume=home,target=/core \
		--trust-keys-from-https --net=host %s:%s --exec \
		/bootkube -- render --asset-dir=/core/assets --api-servers=https://%s:443,https://%s:443`,
			imageRepo, imageTag, m.IP(), m.PrivateIP()),

		// move the local kubeconfig into expected location
		"sudo chown -R core:core /home/core/assets",
		"sudo mkdir -p /etc/kubernetes",
		"sudo cp /home/core/assets/auth/kubeconfig /etc/kubernetes/",

		// start kubelet
		"sudo systemctl enable --now kubelet",

		// start bootkube (rkt fly makes stderr/stdout seperation work)
		fmt.Sprintf(`sudo /usr/bin/rkt run \
                --stage1-name=coreos.com/rkt/stage1-fly:1.19.0 \
        	--volume home,kind=host,source=/home/core \
        	--mount volume=home,target=/core \
                --trust-keys-from-https \
		%s:%s --exec \
        	/bootkube -- start --asset-dir=/core/assets`,
			imageRepo, imageTag),
	}

	// use ssh client to collect stderr and stdout separetly
	// TODO: make the SSH method on a platform.Machine return two slices
	// for stdout/stderr in upstream kola code.
	client, err := m.SSHClient()
	if err != nil {
		return err
	}
	defer client.Close()
	for _, cmd := range cmds {
		session, err := client.NewSession()
		if err != nil {
			return err
		}

		var stdout = bytes.NewBuffer(nil)
		var stderr = bytes.NewBuffer(nil)
		session.Stderr = stderr
		session.Stdout = stdout

		err = session.Start(cmd)
		if err != nil {
			session.Close()
			return err
		}

		// add timeout for each command (mostly used to shorten the bootkube timeout which helps with debugging bootkube start)
		errc := make(chan error)
		go func() { errc <- session.Wait() }()
		select {
		case err := <-errc:
			if err != nil {
				session.Close()
				return fmt.Errorf("SSH session returned error for cmd %s: %s\nSTDOUT:\n%s\nSTDERR:\n%s\n--\n", cmd, err, stdout, stderr)
			}
		case <-time.After(time.Minute * 8):
			session.Close()
			return fmt.Errorf("Timed out waiting for cmd %s: %s\nSTDOUT:\n%s\nSTDERR:\n%s\n--\n", cmd, err, stdout, stderr)
		}
		plog.Infof("Success for cmd %s: %s\nSTDOUT:\n%s\nSTDERR:\n%s\n--\n", cmd, err, stdout, stderr)
		session.Close()
	}

	return nil
}

func startWorkers(workers []platform.Machine, master platform.Machine, kubeletImageTag string) error {
	for _, worker := range workers {
		// transfer kubeconfig from master to worker
		err := platform.TransferFile(master, "/etc/kubernetes/kubeconfig", worker, "/etc/kubernetes/kubeconfig")
		if err != nil {
			return err
		}

		if err := installKubectl(worker, kubeletImageTag); err != nil {
			return err
		}

		// disabled on master so might as well here too
		_, err = worker.SSH("sudo setenforce 0")
		if err != nil {
			return err
		}

		// start kubelet
		_, err = worker.SSH("sudo systemctl enable --now kubelet.service")
		if err != nil {
			return err
		}
	}
	return nil

}

func installKubectl(m platform.Machine, version string) error {
	version, err := stripSemverSuffix(version)
	if err != nil {
		return err
	}

	kubeURL := fmt.Sprintf("https://storage.googleapis.com/kubernetes-release/release/%v/bin/linux/amd64/kubectl", version)
	if _, err := m.SSH("wget -q " + kubeURL); err != nil {
		return err
	}
	if _, err := m.SSH("chmod +x ./kubectl"); err != nil {
		return err
	}

	return nil
}

func stripSemverSuffix(v string) (string, error) {
	semverPrefix := regexp.MustCompile(`^v[\d]+\.[\d]+\.[\d]+`)
	v = semverPrefix.FindString(v)
	if v == "" {
		return "", fmt.Errorf("error stripping semver suffix")
	}

	return v, nil
}
