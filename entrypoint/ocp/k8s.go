package ocp

import (
	"fmt"
	"io/ioutil"
	"os"

	log "github.com/sirupsen/logrus"
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

// k8sAPIClient establishes
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

	pname, err := ioutil.ReadFile(clusterNamespaceFile)
	if err != nil {
		return fmt.Errorf("Failed determining the current namespace: %v", err)
	}
	projectNamespace = string(pname)

	log.Infof("Current project/namespace is %s", projectNamespace)
	return nil
}
