package spawn

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"text/template"
	"time"

	"github.com/coreos-inc/pluton"

	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/tests/etcd"
	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/util"
	"github.com/coreos/pkg/capnslog"
)

var plog = capnslog.NewPackageLogger("github.com/coreos-inc/pluton", "spawn")

type BootkubeManager struct {
	cluster.TestCluster

	firstNode       platform.Machine
	kubeletImageTag string
}

func (m *BootkubeManager) AddMasters(n int) ([]platform.Machine, error) {
	return m.provisionNodes(n, true)
}

func (m *BootkubeManager) AddWorkers(n int) ([]platform.Machine, error) {
	return m.provisionNodes(n, false)
}

// MakeSimpleCluster brings up a multi node bootkube cluster with static etcd
// and checks that all nodes are registered before returning. NOTE: If startup
// times become too long there are a few sections of this setup that could be
// run in parallel.
func MakeBootkubeCluster(c cluster.TestCluster, workerNodes int, selfHostEtcd bool) (*pluton.Cluster, error) {
	// options from flags set by main package
	var (
		imageRepo       = c.Options["BootkubeRepo"]
		imageTag        = c.Options["BootkubeTag"]
		kubeletImageTag = c.Options["HostKubeletTag"]
	)

	// provision master node running etcd
	masterConfig, err := renderCloudConfig(kubeletImageTag, true, !selfHostEtcd)
	if err != nil {
		return nil, err
	}
	master, err := c.NewMachine(masterConfig)
	if err != nil {
		return nil, err
	}
	if !selfHostEtcd {
		if err := etcd.GetClusterHealth(master, 1); err != nil {
			return nil, err
		}
	}
	plog.Infof("Master VM (%s) started. It's IP is %s.", master.ID(), master.IP())

	// start bootkube on master
	if err := bootstrapMaster(master, imageRepo, imageTag, selfHostEtcd); err != nil {
		return nil, err
	}

	// install kubectl on master
	if err := installKubectl(master, kubeletImageTag); err != nil {
		return nil, err
	}

	manager := &BootkubeManager{
		TestCluster:     c,
		kubeletImageTag: kubeletImageTag,
		firstNode:       master,
	}

	// provision workers
	workers, err := manager.provisionNodes(workerNodes, false)
	if err != nil {
		return nil, err
	}

	cluster := pluton.NewCluster(manager, []platform.Machine{master}, workers)

	// check that all nodes appear in kubectl
	if err := cluster.NodeCheck(12); err != nil {
		return nil, err
	}

	return cluster, nil
}

func renderCloudConfig(kubeletImageTag string, isMaster, startEtcd bool) (string, error) {
	config := struct {
		Master         bool
		KubeletVersion string
		StartEtcd      bool
	}{
		isMaster,
		kubeletImageTag,
		startEtcd,
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

func bootstrapMaster(m platform.Machine, imageRepo, imageTag string, selfHostEtcd bool) error {
	var etcdRenderAdditions, etcdStartAdditions string
	if selfHostEtcd {
		etcdRenderAdditions = "--etcd-servers=http://10.3.0.15:2379  --experimental-self-hosted-etcd"
		etcdStartAdditions = fmt.Sprintf("--etcd-server=http://%s:12379 --experimental-self-hosted-etcd", m.PrivateIP())
	}

	var cmds = []string{
		// disable selinux or rkt run commands fail in odd ways
		"sudo setenforce 0",

		// render assets
		fmt.Sprintf(`sudo /usr/bin/rkt run \
		--volume home,kind=host,source=/home/core \
		--mount volume=home,target=/core \
		--trust-keys-from-https --net=host %s:%s --exec \
		/bootkube -- render --asset-dir=/core/assets --api-servers=https://%s:443,https://%s:443 %s`,
			imageRepo, imageTag, m.IP(), m.PrivateIP(), etcdRenderAdditions),

		// move the local kubeconfig into expected location and add admin config for running tests
		"sudo chown -R core:core /home/core/assets",
		"sudo mkdir -p /etc/kubernetes",
		"sudo cp /home/core/assets/auth/bootstrap-kubeconfig /etc/kubernetes/",
		"sudo cp /home/core/assets/auth/admin-kubeconfig /etc/kubernetes/",

		// start kubelet
		"sudo systemctl enable --now kubelet",

		// start bootkube (rkt fly makes stderr/stdout seperation work)
		fmt.Sprintf(`sudo /usr/bin/rkt run \
                --stage1-name=coreos.com/rkt/stage1-fly:1.19.0 \
        	--volume home,kind=host,source=/home/core \
        	--mount volume=home,target=/core \
        	--volume manifests,kind=host,source=/etc/kubernetes/manifests \
        	--mount volume=manifests,target=/etc/kubernetes/manifests \
                --trust-keys-from-https \
		%s:%s --exec \
		/bootkube -- start --asset-dir=/core/assets %s`,
			imageRepo, imageTag, etcdStartAdditions),
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

func (m *BootkubeManager) provisionNodes(n int, tagMaster bool) ([]platform.Machine, error) {
	if n == 0 {
		return []platform.Machine{}, nil
	} else if n < 0 {
		return nil, fmt.Errorf("can't provision negative number of nodes")
	}

	config, err := renderCloudConfig(m.kubeletImageTag, tagMaster, false)
	if err != nil {
		return nil, err
	}

	configs := make([]string, n)
	for i := range configs {
		configs[i] = config
	}

	nodes, err := platform.NewMachines(m, configs)
	if err != nil {
		return nil, err
	}

	// start kubelet
	for _, node := range nodes {
		// transfer bootstrap config from existing node
		err := platform.TransferFile(m.firstNode, "/etc/kubernetes/bootstrap-kubeconfig", node, "/etc/kubernetes/bootstrap-kubeconfig")
		if err != nil {
			return nil, err
		}

		if err := installKubectl(node, m.kubeletImageTag); err != nil {
			return nil, err
		}

		// disable selinux
		_, err = node.SSH("sudo setenforce 0")
		if err != nil {
			return nil, err
		}

		// start kubelet
		_, err = node.SSH("sudo systemctl enable --now kubelet.service")
		if err != nil {
			return nil, err
		}
	}

	// assumes bootkube died before worker node creation and weren't auto-approved
	if err := approvePendingCSRS(m.firstNode, n); err != nil {
		return nil, fmt.Errorf("Approving pending CSRs: %s", err)
	}
	return nodes, nil

}

// Approve exactly n pending CSRs. Block until n pending CSRs are seen and
// eventually timeout. TODO(pb): This is kinda hacky and fragile. We should
// have a way to allow worker nodes to be brought up while bootkube is still
// running. Also use kubelet to approve CSRs when 1.6 is out.
func approvePendingCSRS(node platform.Machine, n int) error {
	var pendingCSRNames []string
	nPending := func() error {
		// return names of non-pending CSRs and subtract that from
		// entire set to get pending set because I don't understand
		// jsonpath right now
		jsonpath := `'{.items[?(@.status.conditions)].metadata.name}'`
		out, err := node.SSH(fmt.Sprintf("./kubectl get csr -o jsonpath=%v", jsonpath))
		if err != nil {
			return err
		}

		nonPendingNames := strings.Split(string(out), " ")

		jsonpath = `'{.items[*].metadata.name}'`
		out, err = node.SSH(fmt.Sprintf("./kubectl get csr -o jsonpath=%v", jsonpath))
		if err != nil {
			return err
		}

		allCSRs := strings.Split(string(out), " ")
		// ones we want to approve
		for _, name := range allCSRs {
			var isPending = true
			for _, n := range nonPendingNames {
				if n == name {
					isPending = false
					break
				}
			}
			if isPending {
				pendingCSRNames = append(pendingCSRNames, name)
			}
		}

		if len(pendingCSRNames) != n {
			pendingCSRNames = pendingCSRNames[:0]
			return fmt.Errorf("Unepected number of CSR requests expected: %v got: %v", n, pendingCSRNames)
		}
		return nil
	}

	if err := util.Retry(10, 10*time.Second, nPending); err != nil {
		return err
	}

	for _, name := range pendingCSRNames {
		// approve all pending
		_, err := node.SSH(fmt.Sprintf(`./kubectl get csr %v -o json |  jq -cr ".status.conditions = [{"type":\"Approved\"}]" > out.json`, name))
		if err != nil {
			return err
		}
		out, err := node.SSH(fmt.Sprintf(`cat out.json | curl -X PUT -H "Content-Type: application/json" --data @- 127.0.0.1:8080/apis/certificates.k8s.io/v1alpha1/certificatesigningrequests/%v/approval`, name))
		if err != nil {
			return fmt.Errorf("curling approved csr: %s", out)
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
