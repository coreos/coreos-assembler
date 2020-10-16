package ocp

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

/*
	To support automagically presenting secrets, this provides support
	secret discovery.

	For this to work properly a service account with access to get secrets
	must be bound to the pod, i.e "serviceNameAccount: cosa-builder"

	Consider the following secret:
		apiVersion: v1
		data:
		  aws_default_region: dXMtZWFzdC0xCg==
		  config:...
		kind: Secret
		metadata:
		  annotations:
		  labels:
			coreos-assembler.coreos.com/secret: aws
		  name: my-super-secret-AWS-keys
		type: Opaque

	When the secretMapper(contextDir) is called, it will look
	for secrets with the label 'coreos-assembler.coreos.com/secret'
	and then look for a matching configuration.

	If the secret is defined, then entrypoint will map in the envVars to the common
	CLI envars, otherwise the secret will be written to a file by its name
	to the path /srv/secrets/<NAME>/<< data.key >>

	In the above example, it would:
		- set the envVar "AWS_DEFAULT_REGION" to "us-east-1"
		- write config to /srv/secrets/my-super-secret-AWS-keys/config
		  and set AWS_CONFIG_FILE to that location.

*/

type varMap map[string]string

type secretMap struct {
	label      string
	envVarMap  varMap
	fileVarMap varMap
}

// SecretMapper maps a secretMap
type SecretMapper interface {
	Setup() error
}

var (
	// create the secret mappings for the supported Clouds
	secretMaps = []*secretMap{
		// Definition for AWS
		&secretMap{
			label: "aws",
			envVarMap: varMap{
				"aws_access_key_id":     "AWS_ACCESS_KEY_ID",
				"aws_secret_access_key": "AWS_SECRET_ACCESS_KEY",
				"aws_default_region":    "AWS_DEFAULT_REGION",
				"aws_ca_bundle":         "AWS_CA_BUNDLE",
			},
			fileVarMap: varMap{
				"config": "AWS_CONFIG_FILE",
			},
		},
		// Definition for GCP
		&secretMap{
			label: "gcp",
			fileVarMap: varMap{
				// gce is the legacy name for GCP
				"gce.json": "GCP_IMAGE_UPLOAD_CONFIG",
				"gcp.json": "GCP_IMAGE_UPLOAD_CONFIG",
			},
		},
	}
)

// Get SecretMapping returns the secretMap and true if found.
func getSecretMapping(s string) (*secretMap, bool) {
	for _, v := range secretMaps {
		if v.label == s {
			return v, true
		}
	}
	return nil, false
}

func (sm *secretMap) writeSecretEnvVars(d map[string][]byte, ret *[]string) error {
	for k, v := range d {
		envKey, ok := sm.envVarMap[k]
		if !ok {
			continue
		}
		log.Debugf("Set envVar %s from secret", envKey)
		*ret = append(*ret, fmt.Sprintf("%s=%s", envKey, strings.TrimSuffix(string(v), "\n")))
	}
	return nil
}

func (sm *secretMap) writeSecretFiles(toDir, name string, d map[string][]byte, ret *[]string) error {
	sDir := filepath.Join(toDir, name)
	if err := os.MkdirAll(sDir, 0755); err != nil {
		return err
	}
	for k, v := range d {
		eKey, ok := sm.fileVarMap[k]
		if !ok {
			continue
		}
		f := filepath.Join(sDir, k)
		if err := ioutil.WriteFile(k, v, 0555); err != nil {
			return err
		}
		*ret = append(*ret, fmt.Sprintf("%s=%s", eKey, f))
	}
	return nil
}

// kubernetesSecretSetup looks for matching secrets in the environment matching
// 'coreos-assembler.coreos.com/secret=k' and then maps the secret
// automatically in. "k" must be in the "known" secrets type to be mapped
// automatically.
func kubernetesSecretsSetup(toDir string) ([]string, error) {
	lo := metav1.ListOptions{
		LabelSelector: secretLabelName,
		Limit:         100,
	}

	var ret []string

	secrets, err := apiClient.Secrets(projectNamespace).List(lo)
	if err != nil {
		return ret, err
	}
	log.Infof("Found %d secrets to consider", len(secrets.Items))

	for _, secret := range secrets.Items {
		sName := secret.GetObjectMeta().GetName()
		labels := secret.GetObjectMeta().GetLabels()
		for k, v := range labels {
			if k != secretLabelName {
				continue
			}
			m, ok := getSecretMapping(v)
			if !ok {
				log.Errorf("Unknown secret type for %s found at %s", v, sName)
				continue
			}
			log.Infof("Known secret type for %s found, mapping automatically", v)

			if err := m.writeSecretEnvVars(secret.Data, &ret); err != nil {
				log.Errorf("Failed to set envVars for %s: %s", sName, err)
			}

			if err := m.writeSecretFiles(toDir, sName, secret.Data, &ret); err != nil {
				log.Errorf("Failed to set files envVars for %s: %s", sName, err)
			}
		}
	}
	return ret, nil
}
