package ocp

const (
	// ocpStructTag is the struct tag used to read in
	// OCPBuilder from envvars
	ocpStructTag = "envVar"

	// defaultContextdir is the default path to use for a build
	defaultContextDir = "/srv"

	// secretLabelName is the label to search for secrets to automatically use
	secretLabelName = "coreos-assembler.coreos.com/secret"

	// ocpSecretDir is where OpenShift mounts the secrets locally
	ocpSecretDir = "/var/run/secrets/openshift.io/build"

	// SECRET_MAP_FILE_ prefix is used to map a secret name
	// to set $ENVAR = $FILE
	cosaSecretMapFile = "SECRET_MAP_FILE_"

	strictModeBashTemplate = `#!/bin/bash
cat <<EOM
===========================================
Executing Commands:
%s
===========================================
EOM

set -euo pipefail
%s
`
)
