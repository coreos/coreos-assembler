package main

import (
	"os/user"

	jobspec "github.com/coreos/gangplank/internal/spec"
	flag "github.com/spf13/pflag"
)

// Flags has the configuration flags.
var specCommonFlags = flag.NewFlagSet("", flag.ContinueOnError)

// sshFlags are specific to minio and remote podman
var (
	sshFlags = flag.NewFlagSet("", flag.ContinueOnError)

	// minioSshRemoteHost is the SSH remote host to forward the local
	// minio instance over.
	minioSshRemoteHost string

	// minioSshRemoteUser is the name of the SSH user to use with minioSshRemoteHost
	minioSshRemoteUser string

	// minioSshRemoteKey is the SSH key to use with minioSshRemoteHost
	minioSshRemoteKey string

	// minioSshRemotePort is the SSH port to use with minioSshRemotePort
	minioSshRemotePort int
)

// cosaKolaTests are used to generate automatic Kola stages.
var cosaKolaTests []string

func init() {
	specCommonFlags.StringSliceVar(&generateCommands, "singleCmd", []string{}, "commands to run in stage")
	specCommonFlags.StringSliceVar(&generateSingleRequires, "singleReq", []string{}, "artifacts to require")
	specCommonFlags.StringVarP(&cosaSrvDir, "srvDir", "S", "", "directory for /srv; in pod mount this will be bind mounted")
	specCommonFlags.StringSliceVar(&generateReturnFiles, "returnFiles", []string{}, "Extra files to upload to the minio server")
	jobspec.AddKolaTestFlags(&cosaKolaTests, specCommonFlags)

	username := ""
	user, err := user.Current()
	if err != nil && user != nil {
		username = user.Username
	}

	sshFlags.StringVar(&minioSshRemoteHost, "forwardMinioSSH", containerHost(), "forward and use minio to ssh host")
	sshFlags.StringVar(&minioSshRemoteUser, "sshUser", username, "name of SSH; used with forwardMinioSSH")
	sshFlags.StringVar(&minioSshRemoteKey, "sshKey", "", "path to SSH key; used with forwardMinioSSH")
}
