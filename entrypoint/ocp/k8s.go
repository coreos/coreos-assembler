package ocp

import (
	"fmt"
	"io/ioutil"
	"os"

	log "github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
)

const clusterNamespaceFile = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"

var (
	// apiClient is v1 Client Interface for interacting Kubernetes
	apiClient corev1.CoreV1Interface

	// projectNamespace is the current namespace
	projectNamespace string

	// forceNotInCluster is used for testing. This is set to
	// true for when testing is run with `-tag ci`
	forceNotInCluster = false
)

// k8sAPIClient opens an in-cluster Kubernetes API client.
// The running pod must have a service account defined in the PodSpec.
func k8sAPIClient() error {
	_, kport := os.LookupEnv("KUBERNETES_SERVICE_PORT")
	_, khost := os.LookupEnv("KUBERNETES_SERVICE_HOST")
	if !khost || !kport || forceNotInCluster {
		return ErrNotInCluster
	}

	// creates the in-cluster config
	cc, err := rest.InClusterConfig()
	if err != nil {
		return err
	}

	// creates the clientset
	nc, err := kubernetes.NewForConfig(cc)
	if err != nil {
		return err
	}
	apiClient = nc.CoreV1()
	//appClient = nc.AppsV1()

	pname, err := ioutil.ReadFile(clusterNamespaceFile)
	if err != nil {
		return fmt.Errorf("Failed determining the current namespace: %v", err)
	}
	projectNamespace = string(pname)

	log.Infof("Current project/namespace is %s", projectNamespace)
	return nil
}

// getPodiP returns the IP of a pod. getPodIP blocks pending until the podIP
// is recieved.
func getPodIP(podName string) (string, error) {
	w, err := apiClient.Pods(projectNamespace).Watch(
		metav1.ListOptions{
			Watch:         true,
			FieldSelector: fields.Set{"metadata.name": podName}.AsSelector().String(),
			LabelSelector: labels.Everything().String(),
		},
	)
	defer w.Stop()

	if err != nil {
		return "", err
	}

	for {
		events, ok := <-w.ResultChan()
		if !ok {
			return "", fmt.Errorf("Failed query for pod IP on pod/%s", podName)
		}
		resp := events.Object.(*v1.Pod)
		if resp.Status.PodIP != "" {
			return resp.Status.PodIP, nil
		}
	}
}
