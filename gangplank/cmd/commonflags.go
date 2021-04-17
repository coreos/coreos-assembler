package main

import (
	flag "github.com/spf13/pflag"
)

// Flags has the configuration flags.
var specCommonFlags = flag.NewFlagSet("", flag.ContinueOnError)

func init() {
	specCommonFlags.StringSliceVar(&generateCommands, "singleCmd", []string{}, "commands to run in stage")
	specCommonFlags.StringSliceVar(&generateSingleRequires, "singleReq", []string{}, "artifacts to require")
	specCommonFlags.StringVarP(&cosaSrvDir, "srvDir", "S", "", "directory for /srv; in pod mount this will be bind mounted")
}
