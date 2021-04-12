package ocp

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"

	log "github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const clusterNamespaceFile = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"

// Cluster describes a Kubnetenes Cluster.
type Cluster struct {
	cs         *kubernetes.Clientset
	nameSpace  string
	configFile string

	// inCluster indicates the client should use the Kubernetes in-cluster client
	inCluster bool

	// podman indicates that the container should be built using Podman
	podman bool

	// podmanSrvDir is the scratch workdir for podman and is bind-mounted
	// in as /srv.
	podmanSrvDir string

	stdIn  *os.File
	stdOut *os.File
	stdErr *os.File
}

// KubernetesCluster is the Gangplank interface to using a cluster.
type KubernetesCluster interface {
	SetStdIO(stdIn, stdOut, stdErr *os.File)
	GetStdIO() (*os.File, *os.File, *os.File)
	SetPodman(string)
}

// Cluster implements a KubernetesCluster
var _ KubernetesCluster = &Cluster{}

// NewCluster returns a Kubernetes cluster
func NewCluster(inCluster bool, namespace, configFile string) KubernetesCluster {
	return &Cluster{
		nameSpace:  namespace,
		inCluster:  inCluster,
		configFile: configFile,
	}
}

// SetPodman forces out-of-cluster execution via Podman.
func (c *Cluster) SetPodman(srvDir string) {
	c.inCluster = false
	c.podman = true
	c.podmanSrvDir = srvDir
}

// SetStdIO sets the IO options
// TODO: Implement for `cosa remote`
func (c *Cluster) SetStdIO(stdIn, stdOut, stdErr *os.File) {
	c.stdIn = stdIn
	c.stdOut = stdOut
	c.stdErr = stdErr
}

// GetStdIO returns the stdIO options
func (c *Cluster) GetStdIO() (*os.File, *os.File, *os.File) {
	return c.stdIn, c.stdOut, c.stdOut
}

// toKubernetesCluster casts the cluster to the interface
func (c *Cluster) toKubernetesCluster() *KubernetesCluster {
	var kc KubernetesCluster = c
	return &kc
}

// ClusterContext is a context
type ClusterContext context.Context
type clusterCtxKey int

const clusterObj clusterCtxKey = 0

// NewClusterContext context with cluster options.
func NewClusterContext(ctx context.Context, kc KubernetesCluster) ClusterContext {
	return context.WithValue(ctx, clusterObj, kc)
}

// GetCluster fetches the Cluster options from the Context
func GetCluster(ctx ClusterContext) (*Cluster, error) { //nolint
	c, ok := ctx.Value(clusterObj).(*Cluster)
	if ok {
		return c, nil
	}
	return nil, fmt.Errorf("invalid or undefined cluster object in context")
}

// GetClient fetches the Kubernetes Client from a ClusterContext.
func GetClient(ctx ClusterContext) (*kubernetes.Clientset, string, error) {
	c, err := GetCluster(ctx)
	if err != nil {
		return nil, "", err
	}
	if c.cs != nil {
		return c.cs, c.nameSpace, nil
	}

	if c.inCluster {
		var ns string
		c.cs, ns, err = k8sInClusterClient()
		if c.nameSpace == "" {
			c.nameSpace = ns
		} else {
			log.WithFields(log.Fields{
				"target":  c.nameSpace,
				"current": ns,
			}).Info("work will be done in another namespace")
		}
	}

	return c.cs, c.nameSpace, err
}

// k8sInClusterClient opens an in-cluster Kubernetes API client.
// The running pod must have a service account defined in the PodSpec.
func k8sInClusterClient() (*kubernetes.Clientset, string, error) {
	_, kport := os.LookupEnv("KUBERNETES_SERVICE_PORT")
	_, khost := os.LookupEnv("KUBERNETES_SERVICE_HOST")
	if !khost || !kport {
		return nil, "", ErrNotInCluster
	}

	// creates the in-cluster config
	cc, err := rest.InClusterConfig()
	if err != nil {
		return nil, "", err
	}

	// creates the clientset
	nc, err := kubernetes.NewForConfig(cc)
	if err != nil {
		return nil, "", err
	}

	pname, err := ioutil.ReadFile(clusterNamespaceFile)
	if err != nil {
		return nil, "", fmt.Errorf("failed determining the current namespace: %v", err)
	}
	pn := string(pname)

	log.Infof("Current project/namespace is %s", pn)
	return nc, pn, nil
}

// getPodIP returns the IP of a pod. getPodIP blocks pending until the podIP is recieved.
func getPodIP(cs *kubernetes.Clientset, podNamespace, podName string) (string, error) {
	w, err := cs.CoreV1().Pods(podNamespace).Watch(
		metav1.ListOptions{
			Watch:         true,
			FieldSelector: fields.Set{"metadata.name": podName}.AsSelector().String(),
			LabelSelector: labels.Everything().String(),
		},
	)

	if err != nil {
		return "", err
	}
	defer w.Stop()

	for {
		events, ok := <-w.ResultChan()
		if !ok {
			return "", fmt.Errorf("failed query for pod IP on pod/%s", podName)
		}
		resp := events.Object.(*v1.Pod)
		if resp.Status.PodIP != "" {
			return resp.Status.PodIP, nil
		}
	}
}
