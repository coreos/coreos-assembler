package main

import (
	"fmt"
	"github.com/spf13/cobra"
	"io"
	"os"
	"os/exec"
	"strings"
)

type RemoteSessionOptions struct {
	CreateImage      string
	CreateExpiration string
	CreateWorkdir    string
	SyncQuiet        bool
}

var (
	remoteSessionOpts RemoteSessionOptions

	cmdRemoteSession = &cobra.Command{
		Use:   "remote-session",
		Short: "cosa remote-session [command]",
		Long:  "Initiate and use remote sessions for COSA execution.",
	}

	cmdRemoteSessionCreate = &cobra.Command{
		Use:   "create",
		Short: "Create a remote session",
		Long: "Create a remote session. This command will print an ID to " +
			"STDOUT that should be set in COREOS_ASSEMBLER_REMOTE_SESSION " +
			"environment variable for later commands to use.",
		Args:    cobra.ExactArgs(0),
		PreRunE: preRunCheckEnv,
		RunE:    runCreate,
	}

	cmdRemoteSessionDestroy = &cobra.Command{
		Use:   "destroy",
		Short: "Destroy a remote session",
		Long: "Destroy a remote session. After running this command the " +
			"COREOS_ASSEMBLER_REMOTE_SESSION should be unset.",
		Args:    cobra.ExactArgs(0),
		PreRunE: preRunCheckEnv,
		RunE:    runDestroy,
	}

	cmdRemoteSessionExec = &cobra.Command{
		Use:     "exec",
		Short:   "Execute a cosa command in the remote session",
		Long:    "Execute a cosa command in the remote session.",
		Args:    cobra.MinimumNArgs(1),
		PreRunE: preRunCheckEnv,
		RunE:    runExec,
	}

	cmdRemoteSessionPS = &cobra.Command{
		Use:     "ps",
		Short:   "Check if the remote session is running",
		Long:    "Check if the remote session is running.",
		Args:    cobra.ExactArgs(0),
		PreRunE: preRunCheckEnv,
		RunE:    runPS,
	}

	cmdRemoteSessionSync = &cobra.Command{
		Use:   "sync",
		Short: "sync files/directories to/from the remote",
		Long: "sync files/directories to/from the remote. The symantics here " +
			"are similar to rsync or scp. Provide `:from to` or `from :to`. " +
			"The argument with the leading ':' will represent the remote.",
		Args:    cobra.MinimumNArgs(2),
		PreRunE: preRunCheckEnv,
		RunE:    runSync,
	}
)

// Function to determine if stdin is a terminal or not.
func isatty() bool {
	cmd := exec.Command("tty")
	cmd.Stdin = os.Stdin
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	err := cmd.Run()
	return err == nil
}

// Function to check if a given environment variable exists
// and is non-empty.
func envVarIsSet(v string) bool {
	val, ok := os.LookupEnv(v)
	if !ok || val == "" {
		return false
	} else {
		return true
	}
}

// Function to return an error object with appropriate text for an
// environment variable error based on the given inputs.
func envVarError(v string, required bool) error {
	if required {
		return fmt.Errorf("The env var %s must be defined and non-empty", v)
	} else {
		return fmt.Errorf("The env var %s must not be defined", v)
	}
}

// Function to check requisite environment variables. This is run
// before each subcommand to perform the checks.
func preRunCheckEnv(c *cobra.Command, args []string) error {
	// We need to make sure that the CONTAINER_HOST env var
	// is set for all commands. This is used for `podman --remote`.
	// We could also check `CONTAINER_SSHKEY` key here but it's not
	// strictly required (user could be using ssh-agent).
	if !envVarIsSet("CONTAINER_HOST") {
		return envVarError("CONTAINER_HOST", true)
	}
	// We need to check COREOS_ASSEMBLER_REMOTE_SESSION. For create
	// we need to make sure it's not set. For all other commands we
	// need to make sure it is set.
	remoteSessionVarIsSet := envVarIsSet("COREOS_ASSEMBLER_REMOTE_SESSION")
	if c.Use == "create" && remoteSessionVarIsSet {
		return envVarError("COREOS_ASSEMBLER_REMOTE_SESSION", false)
	} else if c.Use != "create" && !remoteSessionVarIsSet {
		return envVarError("COREOS_ASSEMBLER_REMOTE_SESSION", true)
	}
	return nil
}

// Creates a "remote session" on the remote. This just creates a
// container on the remote and prints to STDOUT the container ID.
// The user is then expected to store this ID in the
// COREOS_ASSEMBLER_REMOTE_SESSION environment variable.
func runCreate(c *cobra.Command, args []string) error {
	podmanargs := []string{"--remote", "run", "--rm", "-d",
		"--pull=always", "--net=host", "--privileged", "--security-opt=label=disable",
		"--volume", remoteSessionOpts.CreateWorkdir,
		"--workdir", remoteSessionOpts.CreateWorkdir,
		// Mount required volume for buildextend-secex, it will be empty on
		// non-s390x builders.
		// See: https://github.com/coreos/coreos-assembler/blob/main/docs/cosa/buildextend-secex.md
		"--volume=secex-data:/data.secex:ro",
		"--uidmap=1000:0:1", "--uidmap=0:1:1000", "--uidmap=1001:1001:64536",
		"--device=/dev/kvm", "--device=/dev/fuse", "--tmpfs=/tmp",
		"--init", "--entrypoint=/usr/bin/sleep",
		remoteSessionOpts.CreateImage,
		remoteSessionOpts.CreateExpiration}
	cmd := exec.Command("podman", podmanargs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Destroys the "remote session". In reality it just deletes
// the container referenced by $COREOS_ASSEMBLER_REMOTE_SESSION.
func runDestroy(c *cobra.Command, args []string) error {
	session := os.Getenv("COREOS_ASSEMBLER_REMOTE_SESSION")
	podmanargs := []string{"--remote", "rm", "-f", session}
	cmd := exec.Command("podman", podmanargs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Executes a command in the "remote session". Mostly just a
// `podman --remote exec`.
func runExec(c *cobra.Command, args []string) error {
	podmanargs := []string{"--remote", "exec", "-i"}
	if isatty() {
		podmanargs = append(podmanargs, "-t")
	}
	session := os.Getenv("COREOS_ASSEMBLER_REMOTE_SESSION")
	podmanargs = append(podmanargs, session, "cosa")
	podmanargs = append(podmanargs, args...)
	cmd := exec.Command("podman", podmanargs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			// If the command failed let's exit with the same exitcode
			// as the remotely executed process.
			os.Exit(exitError.ExitCode())
		} else {
			return err
		}
	}
	return nil
}

// Executes a `podman --remote ps -a --filter id=<container>`
// to show the status of the remote running cosa container.
func runPS(c *cobra.Command, args []string) error {
	session := os.Getenv("COREOS_ASSEMBLER_REMOTE_SESSION")
	podmanargs := []string{"--remote", "ps", "-a",
		fmt.Sprintf("--filter=id=%s", session)}
	cmd := exec.Command("podman", podmanargs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// runSync provides an rsync-like interface that allows
// files to be copied to/from the remote. It uses
// `podman --remote exec` as the transport for rsync (see [1])
//
// One of the arguments here must be prepended with a `:`. This
// argument will represent the path on the remote. This function
// will substitute `:` with `$COREOS_ASSEMBLER_REMOTE_SESSION:`.
// That environment var just contains the container ID on the remote.
//
// [1] https://github.com/moby/moby/issues/13660
func runSync(c *cobra.Command, args []string) error {
	// check arguments. Need one with pre-pended ':'
	found := 0
	for index, arg := range args {
		if strings.HasPrefix(arg, ":") {
			args[index] = fmt.Sprintf("%s%s",
				os.Getenv("COREOS_ASSEMBLER_REMOTE_SESSION"), arg)
			found++
		}
	}
	if found != 1 {
		return fmt.Errorf("Must pass in a single arg with `:` prepended")
	}
	// build command and execute
	rsyncargs := []string{"-ah", "--no-owner", "--no-group", "--mkpath", "--blocking-io",
		"--compress", "--rsh", "podman --remote exec -i"}
	if !remoteSessionOpts.SyncQuiet {
		rsyncargs = append(rsyncargs, "-v")
	}
	rsyncargs = append(rsyncargs, args...)
	cmd := exec.Command("rsync", rsyncargs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func init() {
	cmdRemoteSession.AddCommand(cmdRemoteSessionCreate)
	cmdRemoteSession.AddCommand(cmdRemoteSessionDestroy)
	cmdRemoteSession.AddCommand(cmdRemoteSessionExec)
	cmdRemoteSession.AddCommand(cmdRemoteSessionPS)
	cmdRemoteSession.AddCommand(cmdRemoteSessionSync)

	// cmdRemoteSessionCreate options
	cmdRemoteSessionCreate.Flags().StringVarP(
		&remoteSessionOpts.CreateImage, "image", "",
		"quay.io/coreos-assembler/coreos-assembler:main",
		"The COSA container image to use on the remote")
	cmdRemoteSessionCreate.Flags().StringVarP(
		&remoteSessionOpts.CreateExpiration, "expiration", "", "infinity",
		"The amount of time before the remote-session auto-exits")
	cmdRemoteSessionCreate.Flags().StringVarP(
		&remoteSessionOpts.CreateWorkdir, "workdir", "", "/srv",
		"The COSA working directory to use inside the container")

	// cmdRemoteSessionSync options
	cmdRemoteSessionSync.Flags().BoolVarP(
		&remoteSessionOpts.SyncQuiet, "quiet", "", false,
		"Make the sync output less verbose")
}

// execute the cmdRemoteSession cobra command
func runRemoteSession(argv []string) error {
	cmdRemoteSession.SetArgs(argv)
	return cmdRemoteSession.Execute()
}
