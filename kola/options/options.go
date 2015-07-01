package options

//TODO(pb): get rid of this hack and look into implementing subcommand
//flags per kola test

// Misc options that kola tests can access without a cyclic
// dependency. Set in kola main cmd.
type TestOptions struct {
	EtcdRollingVersion     string
	EtcdRollingVersion2    string
	EtcdRollingBin         string
	EtcdRollingBin2        string
	EtcdRollingSkipVersion bool
}

var Opts TestOptions
