package ocp

import (

	// minio is needed for moving files around in OpenShift.

	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	log "github.com/sirupsen/logrus"
)

/*
	Minio (https://github.com/minio/minio) is an S3-API Compatible
	Object Store. When running in multi-pod mode, we start Minio
	for pulling and pushing artifacts. Object Storage is a little better
	than using PVC's.

	NOTE: This is intentionally private -- we do not want to expose this
		  functionality outside the ocp package.
*/

var (
	// myHostName used for determining the hostname
	myHostName string
)

const (
	// MinioRegion is a "fake" region
	MinioRegion = "darkarts-1"
)

func init() {
	hname, err := ioutil.ReadFile("/proc/sys/kernel/hostname")
	if err == nil {
		myHostName = strings.TrimSpace(string(hname))
	}
}

// minioServer describes a Minio S3 Object stoarge to start.
type minioServer struct {
	AccessKey    string `json:"accesskey"`
	SecretKey    string `json:"secretkey"`
	Host         string `json:"host"`
	Port         int    `json:"port"`
	dir          string
	minioOptions minio.Options
	cmd          *exec.Cmd
}

// newMinioSever defines an ephemeral minio config. To prevent random pods/people
// accessing or relying on the server, we use entirely random keys.
func newMinioServer() *minioServer {
	minioAccessKey, _ := randomString(12)
	minioSecretKey, _ := randomString(12)

	return &minioServer{
		AccessKey: minioAccessKey,
		SecretKey: minioSecretKey,
		Host:      "",
		Port:      9000,
		dir:       cosaSrvDir,
		minioOptions: minio.Options{
			Creds:  credentials.NewStaticV4(minioAccessKey, minioSecretKey, ""),
			Secure: false,
			Region: MinioRegion,
		},
	}
}

// GetClient returns a Minio Client
func (m *minioServer) client() (*minio.Client, error) {
	return minio.New(fmt.Sprintf("%s:%d", m.Host, m.Port),
		&minio.Options{
			Creds:  credentials.NewStaticV4(m.AccessKey, m.SecretKey, ""),
			Secure: false,
			Region: MinioRegion,
		},
	)
}

// start a MinioServer based on the configuration.
func (m *minioServer) start(ctx context.Context) error {
	// COSA_POD_ID should be set via the BuildConfig
	// using a pod reference, i.e:
	// env:
	//	- name: COSA_POD_IP
	//	  valueFrom:
	// 	    fieldRef:
	//     	 fieldPath: status.podIP
	if m.cmd != nil {
		return errors.New("minio is already running")
	}

	if m.Host == "" {
		host, ok := os.LookupEnv("COSA_POD_IP")
		if ok {
			log.Infof("Minio will use envVar defined hostname %s", host)
			m.Host = strings.TrimSpace(host)
		} else {
			log.Infof("Minio will use kernel provided hostname %s", myHostName)
			m.Host = myHostName
		}
	}

	l := log.WithFields(log.Fields{
		"hostname":   m.Host,
		"port":       m.Port,
		"access_key": m.AccessKey,
		"secret_key": m.SecretKey,
		"serv dir":   m.dir,
	})
	l.Infof("Starting Minio")

	mpath, err := exec.LookPath("minio")
	if err != nil {
		l.WithField("err", err).Error("minio binary not found")
		return errors.New("failed to find minio")
	}

	args := []string{mpath, "server", m.dir}
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Foreground: false,           // Background the process
		Pdeathsig:  syscall.SIGTERM, // Let minio finish before killing
		Pgid:       0,               // Use the pid of the minio as the pgroup id
		Setpgid:    true,            // Set the pgroup
	}
	cmd.Env = append(
		os.Environ(),
		fmt.Sprintf("MINIO_ACCESS_KEY=%s", m.AccessKey),
		fmt.Sprintf("MINIO_SECRET_KEY=%s", m.SecretKey),
	)

	err = cmd.Start()
	if err != nil {
		stdoutStderr, _ := cmd.CombinedOutput()
		l.WithFields(log.Fields{
			"err": err,
			"out": stdoutStderr,
		}).Error("Failed to start minio")
	}

	// Ensure the process gets terminated on shutdown
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM, syscall.SIGUSR1, syscall.SIGUSR2)
	go func() {
		<-sigs
		m.kill()
	}()

	m.cmd = cmd
	return err
}

// kill terminates the minio server.
func (m *minioServer) kill() {
	if m.cmd == nil {
		return
	}
	// Note the "-" before the processes PID. A negative pid to
	// syscall.Kill kills the processes Pid group ensuring all forks/execs
	// of minio are killed too.
	_ = syscall.Kill(-m.cmd.Process.Pid, syscall.SIGTERM)

	// Purge the minio files since they are used per-session.
	if err := os.RemoveAll(filepath.Join(m.dir, ".minio.sys")); err != nil {
		log.WithError(err).Error("failed to remove minio files")
	}

}

func randomString(n int) (string, error) {
	const letters = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	bits := make([]byte, n)
	_, err := rand.Read(bits)
	if err != nil {
		return "", err
	}
	for i, b := range bits {
		bits[i] = letters[b%byte(len(letters))]
	}
	return string(bits), nil
}

func (m *minioServer) ensureBucketExists(ctx context.Context, bucket string) error {
	mc, err := m.client()
	if err != nil {
		return err
	}

	be, err := mc.BucketExists(ctx, bucket)
	if err != nil {
		return err
	}
	if be {
		return nil
	}

	err = mc.MakeBucket(ctx, bucket, minio.MakeBucketOptions{Region: MinioRegion})
	if err != nil {
		return fmt.Errorf("failed call to create bucket: %w", err)
	}
	return nil
}

// fetcher retrieves an object from a Minio server
func (m *minioServer) fetcher(ctx context.Context, bucket, object string, dest io.Writer) error {
	if m.Host == "" {
		return errors.New("host is undefined")
	}
	log.Infof("Requesting remote http://%s/%s/%s", m.Host, bucket, object)
	mc, err := m.client()
	if err != nil {
		return err
	}

	src, err := mc.GetObject(ctx, bucket, object, minio.GetObjectOptions{})
	if err != nil {
		return err
	}
	defer src.Close()
	n, err := io.Copy(dest, src)
	log.WithFields(log.Fields{
		"bucket": bucket,
		"err":    err,
		"host":   m.Host,
		"object": object,
		"read":   n,
	}).Info("processed")
	return err
}

// putter uploads the contents of an io.Reader to a remote MinioServer
func (m *minioServer) putter(ctx context.Context, bucket, object, fpath string, overwrite bool) error {
	if err := m.ensureBucketExists(ctx, bucket); err != nil {
		return fmt.Errorf("unable to validate %s bucket exists: %w", bucket, err)
	}
	fi, err := os.Stat(fpath)
	if err != nil {
		return err
	}
	stamp := fmt.Sprintf("%d", fi.ModTime().UnixNano())

	l := log.WithFields(log.Fields{
		"bucket":    bucket,
		"from":      fpath,
		"func":      "putter",
		"object":    object,
		"overwrite": overwrite,
		"size":      fmt.Sprintf("%d", fi.Size()),
		"stamp":     stamp,
	})
	l.Info("starting upload")

	mC, err := m.client()
	if err != nil {
		return err
	}

	s, err := mC.StatObject(ctx, bucket, object, minio.GetObjectOptions{})
	if err != nil {
		for k, v := range s.UserMetadata {
			if k == "stamp" && stamp == v {
				l.Info("already uploaded size matches, skipping")
				return nil
			}
			if v != myHostName && !overwrite {
				l.Error("already uploaded by another host, skipping")
				return fmt.Errorf("%s has already created %s/%s", v, bucket, object)
			}
		}
	}

	i, err := mC.FPutObject(ctx, bucket, object, fpath,
		minio.PutObjectOptions{
			UserMetadata: map[string]string{
				"creator": myHostName,
				"stamp":   stamp,
			},
		},
	)
	if err != nil {
		return fmt.Errorf("failed to upload to %s/%s: %w", bucket, object, err)
	}
	l.WithFields(log.Fields{
		"etag":        i.ETag,
		"remote size": i.Size,
	}).Info("uploaded")

	return nil
}
