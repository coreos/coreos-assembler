package ocp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"time"

	"github.com/coreos/gangplank/spec"
	buildapiv1 "github.com/openshift/api/build/v1"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"
)

/*
	A CIBuilder is a ci interface for running Gangplank as part
	of a CI system (i.e. Jenkins) while benefiting from the BuildConfig
	niceties.

	While it does not require running as a BuildConfig, it does require that the
	running pod exposes a service account with:
	- secret access
	- the ability to create pods
*/

type ci struct {
	jobSpecFile string
	bc          *buildConfig
	ciBuild     string
}

// CIBuilder is the manual/unbounded Build interface.
// A CIBuilder uses a build.openshift.io/v1 Build interface
// to use the exact same code path between the two.
type CIBuilder interface {
	Exec(ctx context.Context) error
}

var (
	// cli is a Builder (and a poor one at that too...)
	// While a CIBuilder is a Builder, we treat it seperately.
	_ = CIBuilder(&ci{})
)

const (
	ciLabel      = "gangplank.coreos-assembler.coreos.com"
	ciAnnotation = ciLabel + "%s"
	ciRunnerTag  = "cosa-ci-runner"
)

func (c *ci) Exec(ctx context.Context) error {
	log.Info("Executing unbounded builder")
	return c.bc.Exec(ctx)
}

// NewCIBuilder returns a CIBuilder ready for execution.
func NewCIBuilder(ctx context.Context, image, serviceAccount, jsF string) (CIBuilder, error) {
	// Require a Kubernetes Service Account Client
	if err := k8sAPIClient(); err != nil {
		return nil, fmt.Errorf("failed create a kubernetes client: %w", err)
	}

	// Directly inject the jobspec
	js, err := spec.JobSpecFromFile(jsF)
	if err != nil {
		return nil, err
	}
	if js.Recipe.GitURL == "" {
		return nil, errors.New("JobSpec does inclue a Git Recipe")
	}
	os.Setenv("SOURCE_REF", js.Recipe.GitRef)
	os.Setenv("SOURCE_URI", js.Recipe.GitURL)

	log.WithFields(log.Fields{
		"override image": image,
		"source ref":     js.Recipe.GitRef,
		"source uri":     js.Recipe.GitURL,
	}).Info("jobspec defined source")

	// Get the ci API
	ciBuild, err := ciAPIBuild(&js, image, serviceAccount)
	if err != nil {
		return nil, fmt.Errorf("failed to create a ci API build object")
	}
	os.Setenv("BUILD", ciBuild)

	// Create the buildConfig object
	bc, err := newBC()
	if err != nil {
		return nil, err
	}
	bc.JobSpec = js
	bc.JobSpecFile = jsF

	c := &ci{
		jobSpecFile: jsF,
		bc:          bc,
		ciBuild:     ciBuild,
	}
	return c, nil
}

func ciAPIBuild(js *spec.JobSpec, image, serviceAccount string) (string, error) {
	// Dig deep and query find out what Kubernetes thinks this pod
	// Discover where this running
	hostname, ok := os.LookupEnv("HOSTNAME")
	if !ok {
		return "", errors.New("Unable to find hostname")
	}

	myIP, err := getPodIP(hostname)
	if err != nil {
		return "", fmt.Errorf("failed to query my hostname: %w", err)
	}

	// Discover where this running
	myPod, err := apiClient.Pods(projectNamespace).Get(hostname, metav1.GetOptions{})
	if err != nil {
		return "", err
	}

	// find the running pod this is running on.
	var myContainer *v1.Container = nil
	for _, k := range myPod.Spec.Containers {
		lk := strings.ToLower(k.Image)
		for _, x := range []string{"cosa", "coreos-assembler"} {
			if strings.Contains(lk, x) {
				myContainer = &k
				break
			}
		}
	}

	// allow both the service account and the image to be overriden
	if serviceAccount == "" {
		serviceAccount = myPod.Spec.ServiceAccountName
	}
	if image == "" {
		image = myContainer.Image
	}
	if serviceAccount == "" || image == "" {
		return "", errors.New("serviceAccount and image must be defined by running pod or via overrides")
	}

	l := log.WithFields(log.Fields{
		"ip":             myIP,
		"image":          image,
		"serviceAccount": serviceAccount,
		"host":           myContainer.Name,
	})
	l.Info("identified pod")

	// Create just _enough_ of the OpenShift BuildConfig spec
	// Create a "ci" build.openshift.io/v1 specification.
	ciBuildNumber := time.Now().Format("20060102150405")
	a := buildapiv1.Build{}

	// Create annotations
	a.Annotations = map[string]string{
		// ciRunnerTag is tested for to determine if this is
		// a buildconfig or a faked one
		ciRunnerTag:                     "true",
		fmt.Sprintf(ciAnnotation, "IP"): myIP,
		// Required Labels
		buildapiv1.BuildConfigAnnotation: "ci-cosa-bc",
		buildapiv1.BuildNumberAnnotation: ciBuildNumber,
	}

	// Create basic labels
	a.Labels = map[string]string{
		ciLabel: ciBuildNumber,
	}

	// Populate the Spec
	a.Spec = buildapiv1.BuildSpec{}
	a.Spec.ServiceAccount = myPod.Spec.ServiceAccountName
	a.Spec.Strategy = buildapiv1.BuildStrategy{}
	a.Spec.Strategy.CustomStrategy = new(buildapiv1.CustomBuildStrategy)
	a.Spec.Strategy.CustomStrategy.From = corev1.ObjectReference{
		Name: image,
	}
	a.Spec.Source = buildapiv1.BuildSource{
		ContextDir: cosaSrvDir,
		Git: &buildapiv1.GitBuildSource{
			Ref: js.Recipe.GitRef,
			URI: js.Recipe.GitURL,
		},
	}

	// Render the ci buildapiv1 object to a JSON object.
	// JSON is the messaginging interface for Kubernetes.
	aW := bytes.NewBuffer([]byte{})
	s := json.NewYAMLSerializer(json.DefaultMetaFactory, buildScheme, buildScheme)
	if err := s.Encode(&a, aW); err != nil {
		return "", err
	}
	d, err := ioutil.ReadAll(aW)
	if err != nil {
		return "", err
	}

	return string(d), nil

}
