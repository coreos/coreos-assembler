package ocp

import (
	"fmt"
	"path/filepath"

	log "github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

/*
	mountReferance describes secrets or configMaps that are mounted
	as volumes. In general, these volumes contain data that is used by
	systems level tooling use as Kerberos, CA certs, etc.
	The label coreos-assembler.coreos.com/mount-ref is needed in this case
*/

// mountReferance is mapping of secrets or a configmap
type mountReferance struct {
	volumes         []v1.Volume
	volumeMounts    []v1.VolumeMount
	requireData     []string
	addInitCommands []string
}

// secretMountRefLabel is used for mounting of secrets
const mountRefLabel = "coreos-assembler.coreos.com/mount-ref"

var (
	volMaps = map[string]mountReferance{
		// internal-ca should be a fully extracted pem file
		"internal-ca": {
			volumes: []v1.Volume{
				{
					Name: "pki",
					VolumeSource: v1.VolumeSource{
						Secret: &v1.SecretVolumeSource{
							DefaultMode: ptrInt32(444),
							SecretName:  "<UNSET>",
						},
					},
				},
			},
			volumeMounts: []v1.VolumeMount{
				{
					Name:      "pki",
					MountPath: "/etc/pki/ca-trust/source/anchors2/",
				},
			},
		},

		// Push/Pull secrets
		"docker.json": {
			volumes: []v1.Volume{
				{
					Name: "docker-json",
					VolumeSource: v1.VolumeSource{
						Secret: &v1.SecretVolumeSource{
							DefaultMode: ptrInt32(444),
							SecretName:  "<UNSET>",
						},
					},
				},
			},
			volumeMounts: []v1.VolumeMount{
				{
					Name:      "docker-json",
					MountPath: filepath.Join(cosaSrvDir, "secrets", "auths"),
				},
			},
		},
		// Koji ConfigMap
		"koji-ca": {
			volumes: []v1.Volume{
				{
					Name: "koji-ca",
					VolumeSource: v1.VolumeSource{
						ConfigMap: &v1.ConfigMapVolumeSource{
							LocalObjectReference: v1.LocalObjectReference{
								Name: "<UNSET>",
							},
						},
					},
				},
			},
			volumeMounts: []v1.VolumeMount{
				{
					Name:      "koji-ca",
					MountPath: "/etc/pki/brew",
				},
			},
		},

		// Koji Configuration ConfigMap
		"koji-config": {
			volumes: []v1.Volume{
				{
					Name: "koji-config",
					VolumeSource: v1.VolumeSource{
						ConfigMap: &v1.ConfigMapVolumeSource{
							LocalObjectReference: v1.LocalObjectReference{
								Name: "<UNSET>",
							},
						},
					},
				},
			},
			volumeMounts: []v1.VolumeMount{
				{
					Name:      "koji-config",
					MountPath: "/etc/koji.conf.d",
				},
			},
		},

		// Kerberos Configuration ConfigMap: usually used by the brew code.
		"krb5.conf": {
			volumes: []v1.Volume{
				{
					Name: "koji-kerberos",
					VolumeSource: v1.VolumeSource{
						ConfigMap: &v1.ConfigMapVolumeSource{
							LocalObjectReference: v1.LocalObjectReference{
								Name: "<UNSET>",
							},
						},
					},
				},
			},
			volumeMounts: []v1.VolumeMount{
				{
					Name:      "koji-kerberos",
					MountPath: "/etc/krb5.conf.d",
				},
			},
		},
	}
)

// ptrInt32 converts an int32 to a ptr of the int32
func ptrInt32(i int32) *int32 { return &i }

// byteField represents a configMap's data fields
type byteFields map[string][]byte

// stringFields represent a secret's data fields
type stringFields map[string]string

// toStringFields is used to convert from a byteFields to a stringFields
func toStringFields(bf byteFields) stringFields {
	d := make(stringFields)
	for k, v := range bf {
		d[k] = string(v)
	}
	return d
}

// addVolumesFromConfigMapLabels discovers configMaps with matching labels and if known,
// adds the defined volume mount from volMaps.
func (cp *cosaPod) addVolumesFromConfigMapLabels() error {
	ac, ns, err := GetClient(cp.clusterCtx)
	if err != nil {
		return err
	}
	lo := metav1.ListOptions{
		LabelSelector: mountRefLabel,
		Limit:         100,
	}

	cfgMaps, err := ac.CoreV1().ConfigMaps(ns).List(cp.clusterCtx, lo)
	if err != nil {
		return err
	}
	log.Infof("Found %d configMaps to consider for mounting", len(cfgMaps.Items))

	for _, cfgMap := range cfgMaps.Items {
		if err := cp.addVolumeFromObjectLabel(cfgMap.GetObjectMeta(), cfgMap.Data); err != nil {
			return err
		}
		log.WithField("secret", cfgMap.Name).Info("mounts defined for secret")
	}

	return nil
}

// addVolumesFromSecretLabels discovers secrets with matching labels and if known,
// adds the defined volume mount from volMaps.
func (cp *cosaPod) addVolumesFromSecretLabels() error {
	ac, ns, err := GetClient(cp.clusterCtx)
	if err != nil {
		return err
	}
	lo := metav1.ListOptions{
		LabelSelector: mountRefLabel,
		Limit:         100,
	}

	secrets, err := ac.CoreV1().Secrets(ns).List(cp.clusterCtx, lo)
	if err != nil {
		return err
	}
	log.Infof("Found secret %d to consider for mounting", len(secrets.Items))

	for _, secret := range secrets.Items {
		if err := cp.addVolumeFromObjectLabel(secret.GetObjectMeta(), toStringFields(secret.Data)); err != nil {
			return err
		}
		log.WithField("secret", secret.Name).Info("mounts defined for secret")
	}
	return nil
}

// addVolumeFromObjectLabel is a helper that recieves an object and data and looks up
// the object's name from volMaps. If a mapping is found, then the object is added to
// cosaPod's definition.
func (cp *cosaPod) addVolumeFromObjectLabel(obj metav1.Object, fields stringFields) error {
	oName := obj.GetName()
	labels := obj.GetLabels()
	for k, v := range labels {
		if k != mountRefLabel {
			continue
		}
		elem, ok := volMaps[v]
		if !ok {
			continue
		}

		// Check for required elements to be in the secret
		missing := make([]string, len(elem.requireData))
		for _, r := range elem.requireData {
			_, found := fields[r]
			if !found {
				missing = append(missing, r)
			}
		}
		if len(missing) > 0 {
			return fmt.Errorf("object %s is missing required elements %v", oName, missing)
		}

		// Set the object reference
		for i := range elem.volumes {
			log.WithField("volume", elem.volumes[i].Name).Info("Added mount definition")
			if elem.volumes[i].VolumeSource.Secret != nil {
				elem.volumes[i].VolumeSource.Secret.SecretName = oName
				log.WithField("secret", oName).Info("Added secretRef volume mount")
			}
			if elem.volumes[i].VolumeSource.ConfigMap != nil {
				elem.volumes[i].VolumeSource.ConfigMap.LocalObjectReference.Name = oName
				log.WithField("configMaps", oName).Info("Added configmap volume mount")
			}
		}

		// Set the volumes in the defualt pod spec
		cp.volumes = append(cp.volumes, elem.volumes...)
		cp.volumeMounts = append(cp.volumeMounts, elem.volumeMounts...)
		cp.ocpInitCommand = append(cp.ocpInitCommand, elem.addInitCommands...)
	}
	return nil
}
