package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/coreos-inc/pluton/spawn"

	"github.com/coreos/mantle/kola"
	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/platform/machine/aws"
	"github.com/coreos/mantle/platform/machine/gcloud"
	"github.com/spf13/cobra"
)

var (
	root = &cobra.Command{
		Use:   "bagger288 [command]",
		Short: "Krupp Excavator Model 288",
	}

	cmdUp = &cobra.Command{
		Use: "up",
		Run: runUp,
	}

	cmdDown = &cobra.Command{
		Use: "down",
		Run: runDown,
	}

	// image uri
	bootkubeImage string
	// image tag
	bootkubeTag string
	// image tag
	hyperkubeTag string

	clusterStateFile string
	workers          int
	targetPlatform   string
	outputKubeConfig string
)

func init() {
	root.PersistentFlags().StringVar(&clusterStateFile, "cluster-state", "clusterstate.json", "opaque cluster state file")

	cmdUp.Flags().IntVar(&workers, "workers", 2, "worker node count")

	sv := cmdUp.Flags().StringVar

	sv(&bootkubeImage, "bootkube-image", "quay.io/coreos/bootkube", "bootkube image")
	sv(&bootkubeTag, "bootkube-tag", "", "bootkube image tag")
	sv(&hyperkubeTag, "hyperkube-tag", "v1.5.1_coreos.0", "hyperkube image tag")

	sv(&targetPlatform, "platform", "aws", "platform - aws or gce")
	sv(&outputKubeConfig, "kubeconfig", "kubeconfig", "output kube-config file")

	// gce-specific options
	sv(&kola.GCEOptions.Image, "gce-image", "latest", "GCE image")
	sv(&kola.GCEOptions.Project, "gce-project", "coreos-gce-testing", "GCE project name")
	sv(&kola.GCEOptions.Zone, "gce-zone", "us-central1-a", "GCE zone name")
	sv(&kola.GCEOptions.MachineType, "gce-machinetype", "n1-standard-1", "GCE machine type")
	sv(&kola.GCEOptions.DiskType, "gce-disktype", "pd-ssd", "GCE disk type")
	sv(&kola.GCEOptions.Network, "gce-network", "default", "GCE network")
	bv := cmdUp.Flags().BoolVar
	bv(&kola.GCEOptions.ServiceAuth, "gce-service-auth", false, "for non-interactive auth when running within GCE")

	// aws-specific options
	// CoreOS-alpha-1262.0.0 on us-west-1
	sv(&kola.AWSOptions.AMI, "aws-ami", "ami-d08ddbb0", "AWS AMI ID")
	sv(&kola.AWSOptions.InstanceType, "aws-type", "m1.small", "AWS instance type")
	sv(&kola.AWSOptions.SecurityGroup, "aws-sg", "pluton", "AWS security group name")

	root.AddCommand(cmdUp)
	root.AddCommand(cmdDown)
}

func main() {
	if err := root.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

type CluserState struct {
	Platform  string   `json:"platform"`
	Instances []string `json:"instances"`
}

func runUp(cmd *cobra.Command, args []string) {
	if workers <= 0 {
		die("Workers must be one or more")
	}

	if bootkubeTag == "" {
		die("Must specify bootkube image (%q) tag", bootkubeImage)
	}

	var (
		err error
		cls platform.Cluster
	)

	switch targetPlatform {
	case "gce":
		cls, err = gcloud.NewCluster(&kola.GCEOptions)
	case "aws":
		cls, err = aws.NewCluster(&kola.AWSOptions)
	default:
		err = fmt.Errorf("invalid platform %q", targetPlatform)
	}

	if err != nil {
		die("Cluster failed: %v", err)
	}

	tc := cluster.TestCluster{
		Name:    "pluton",
		Cluster: cls,
		Options: map[string]string{
			"BootkubeImageRepo": bootkubeImage,
			"BootkubeImageTag":  bootkubeTag,
			"KubeletImageTag":   hyperkubeTag,
		},
	}

	pc, err := spawn.MakeBootkubeCluster(tc, workers)
	if err != nil {
		die("Failed to create k8s cluster: %v", err)
	}

	kc, err := os.Create(outputKubeConfig)
	if err != nil {
		die("Failed creating local kubeconfig: %v", err)
	}

	defer kc.Close()

	rc, err := platform.ReadFile(pc.Masters[0], "/etc/kubernetes/kubeconfig")
	if err != nil {
		die("Failed snarfing kubeconfig: %v", err)
	}

	defer rc.Close()

	if _, err := io.Copy(kc, rc); err != nil {
		die("Failed copying kubeconfig: %v", err)
	}

	cs := &CluserState{
		Platform: targetPlatform,
	}

	for _, m := range pc.Masters {
		cs.Instances = append(cs.Instances, m.ID())
	}
	for _, m := range pc.Workers {
		cs.Instances = append(cs.Instances, m.ID())
	}

	csf, err := os.Create(clusterStateFile)
	if err != nil {
		die("Failed to create cluster state file: %v", err)
	}

	if err := json.NewEncoder(csf).Encode(cs); err != nil {
		die("Failed encoding cluster state: %v", err)
	}
}

func runDown(cmd *cobra.Command, args []string) {
	die("The `down` subcommand is not implemented yet.")
}

func die(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
