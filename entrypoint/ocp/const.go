package ocp

const (
	// ocpStructTag is the struct tag used to read in
	// OCPBuilder from envvars
	ocpStructTag = "envVar"

	// defaultContextdir is the default path to use for a build
	defaultContextDir = "/srv"

	// secretLabelName is the label to search for secrets to automatically use
	secretLabelName = "coreos-assembler.coreos.com/secret"
)
