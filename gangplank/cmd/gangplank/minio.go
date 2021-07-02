package main

import (
	"os"

	"github.com/coreos/gangplank/internal/ocp"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

/*
	In enviroments where something else handles the visiualization of stages (such as Jenkins)
	using the inherent stages in Gangplank can produced jumbled garbage.

	Each Gangpplank worker requires a Minio Server running on the coordinating host/pod.
	The `gangplank minio` command allows for starting a Minio instance and then saving
	the configuration to allow for a shared `minio` instance.

	When using a shared instance, you must use `cosa build --delay-meta-merge`.

	Example start
		nohup gangplank minio -m /tmp/minio.cfg -d /srv &

	Example worker accessing minio:
		gangplank pod -m /tmp/minio.cfg --spec test.spec

	When running in minio mode, Gangplank will continue to run until it is sig{kill,term}ed.

*/

var cmdMinio = &cobra.Command{
	Use:   "minio",
	Short: "Start running a minio server",
	Run:   runMinio,
}

var (
	// minioCfgFile is an ocp.minioServerCfg file
	minioCfgFile string

	// minioServeDir is the directory that Gangplank runs minio from
	minioServeDir string
)

func init() {
	cmdRoot.AddCommand(cmdMinio)
	cmdRoot.PersistentFlags().StringVarP(&minioCfgFile, "minioCfgFile", "m", "", "location of where to create of external minio config file")
	cmdMinio.Flags().StringVarP(&minioServeDir, "minioServeDir", "d", "", "location to service minio from")
	cmdMinio.Flags().AddFlagSet(sshFlags)
}

func runMinio(c *cobra.Command, args []string) {
	defer cancel()
	defer ctx.Done()

	if minioCfgFile == "" {
		log.Fatal("must define --minioCfgfile to run in Minio mode")
	}
	if minioServeDir == "" {
		log.Fatal("must define the workdir to serve minio from")
	}
	if _, err := os.Stat(minioCfgFile); err == nil {
		log.Fatalf("existing minio configuration exists, refusing to overwrite")
	}

	var minioSSH *ocp.SSHForwardPort
	if minioSshRemoteHost != "" {
		minioSSH = &ocp.SSHForwardPort{
			Host: minioSshRemoteHost,
			User: minioSshRemoteUser,
		}
	}
	m, err := ocp.StartStandaloneMinioServer(ctx, minioServeDir, minioCfgFile, minioSSH)
	if err != nil {
		log.WithError(err).Fatalf("failed to start minio server")
	}
	defer m.Kill()

	log.Info("Waiting for kill signal for minio")

	m.Wait()
}
