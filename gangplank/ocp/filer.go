package ocp

import (

	// minio is needed for moving files around in OpenShift.

	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

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

// minioServer describes a Minio S3 Object stoarge to start.
type minioServer struct {
	AccessKey      string `json:"accesskey"`
	SecretKey      string `json:"secretkey"`
	Host           string `json:"host"`
	Port           int    `json:"port"`
	ExternalServer bool   `json:"external_server"` //indicates that a server should not be started
	Region         string `json:"region"`

	dir          string
	minioOptions minio.Options
	cmd          *exec.Cmd
}

// StartStanaloneMinioServer starts a standalone minio server.
func StartStandaloneMinioServer(ctx context.Context, srvDir, cfgFile string) (*minioServer, error) {
	m := newMinioServer("")
	m.dir = srvDir

	if err := m.start(ctx); err != nil {
		return nil, err
	}

	m.ExternalServer = true
	if err := m.WriteToFile(cfgFile); err != nil {
		return nil, fmt.Errorf("failed to write configuration for minio: %v", err)
	}

	return m, nil
}

// newMinioSever defines an ephemeral minioServer from a config or creates a new one.
// To prevent random pods/people accessing or relying on the server, we use entirely random keys.
func newMinioServer(cfgFile string) *minioServer {
	if cfgFile != "" {
		m, err := minioCfgFromFile(cfgFile)
		if err != nil {
			log.WithError(err).Fatalf("failed read minio cfg from %s", cfgFile)
		}
		log.Infof("Minio configuration defined from %s", cfgFile)
		return &m
	}

	// If Gangplank is running in cluster, then get the IP address. Using
	// hostnames can be problematic.
	host := getHostname()
	ac, ns, err := k8sInClusterClient()
	if err == nil && ac != nil {
		ip, err := getPodIP(ac, ns, host)
		if err == nil {
			host = ip
		}
	}

	log.Info("Defining a new minio server")
	minioAccessKey, _ := randomString(12)
	minioSecretKey, _ := randomString(12)

	return &minioServer{
		AccessKey:      minioAccessKey,
		SecretKey:      minioSecretKey,
		Host:           host,
		dir:            cosaSrvDir,
		ExternalServer: false,
		minioOptions: minio.Options{
			Creds:  credentials.NewStaticV4(minioAccessKey, minioSecretKey, ""),
			Secure: false,
			Region: fmt.Sprintf("cosaHost-%s", getHostname()),
		},
	}

}

// GetClient returns a Minio Client
func (m *minioServer) client() (*minio.Client, error) {
	return minio.New(fmt.Sprintf("%s:%d", m.Host, m.Port),
		&minio.Options{
			Creds:  credentials.NewStaticV4(m.AccessKey, m.SecretKey, ""),
			Secure: false,
			Region: m.Region,
		},
	)
}

// start a MinioServer based on the configuration. If the minioServer is external,
// then is this noop.
func (m *minioServer) start(ctx context.Context) error {

	// If the server is external, we test it before proceeding.
	if m.ExternalServer {
		if err := m.ensureBucketExists(ctx, "builds"); err != nil {
			return fmt.Errorf("failed to query Minio/S3 server: %v", err)
		}
		log.Info("Minio Server is listening and ready")
		return nil
	}

	if m.Host == "" {
		m.Host = getHostname()
	}

	if m.Port == 0 {
		m.Port = getPortOrNext(9000)
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

	addr := fmt.Sprintf(":%d", m.Port)
	args := []string{mpath, "server", "--address", addr, m.dir}
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

	time.Sleep(1 * time.Second)
	if cmd == nil || (cmd.ProcessState != nil && cmd.ProcessState.Exited()) {
		return fmt.Errorf("minio started but exited")
	}

	if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
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
		m.Kill()
	}()

	m.cmd = cmd
	return err
}

// Kill terminates the minio server.
func (m *minioServer) Kill() {
	if m.cmd == nil {
		return
	}
	// Note the "-" before the processes PID. A negative pid to
	// syscall.Kill kills the processes Pid group ensuring all forks/execs
	// of minio are killed too.
	_ = syscall.Kill(-m.cmd.Process.Pid, syscall.SIGTERM)

	// Wait for the command to end.
	if m.cmd != nil {
		_ = m.cmd.Wait()
	}

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

	err = mc.MakeBucket(ctx, bucket, minio.MakeBucketOptions{Region: m.Region})
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
	}).Info("written")

	// Set the attributes
	f, ok := dest.(*os.File)
	if ok {
		info, err := src.Stat()
		if err != nil {
			return err
		}
		if err := os.Chtimes(f.Name(), info.LastModified, info.LastModified); err != nil {
			return err
		}
	}

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
	l := log.WithFields(log.Fields{
		"bucket":    bucket,
		"from":      fpath,
		"func":      "putter",
		"object":    object,
		"overwrite": overwrite,
		"size":      fmt.Sprintf("%d", fi.Size()),
	})

	fo, err := os.Open(fpath)
	if err != nil {
		return err
	}

	l.Info("Calculating SHA256 for upload consideration")
	sha256, err := calcSha256(fo)
	if err != nil {
		return err
	}
	l.WithField("sha256", sha256).Infof("Calculated SHA256")

	mC, err := m.client()
	if err != nil {
		return err
	}

	s, err := mC.StatObject(ctx, bucket, object, minio.GetObjectOptions{})
	if err == nil {
		rSHA, ok := s.UserMetadata["X-Amz-Meta-Sha256"]
		if ok {
			if rSHA == sha256 && fi.Size() == s.Size {
				l.Infof("Exact version has already been uploaded, skipping")
				return nil
			}
			l.WithFields(log.Fields{
				"remote sha":  rSHA,
				"remote size": s.Size,
			}).Info("Replacing remove version")
		}

		if !overwrite {
			l.Warning("Skipping overwrite of file; overwrite set to false for the this file")
		}
	}

	l.WithField("sha256", sha256).Info("Starting Upload")
	i, err := mC.FPutObject(ctx, bucket, object, fpath,
		minio.PutObjectOptions{
			// When uploaded, user-metadata will be X-Amz-Meta-<key>
			UserMetadata: map[string]string{
				"sha256": sha256,
			},
		},
	)
	if err != nil {
		return fmt.Errorf("failed to upload to %s/%s: %w", bucket, object, err)
	}
	l.WithFields(log.Fields{
		"etag":        i.ETag,
		"remote size": i.Size,
	}).Info("Uploaded")

	return nil
}

// calcSha256 caluclates a SHA256 hash for the file.
// io.Copy is buffered to 32Kb, so this is large-file safe.
func calcSha256(f io.Reader) (string, error) {
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", nil
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

// checkPort checks if a port is open
func checkPort(port int) error {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return err
	}
	defer ln.Close() //nolint
	return nil
}

// getNextPort iterates and finds the next available port
func getPortOrNext(port int) int {
	for {
		if err := checkPort(port); err == nil {
			return port
		}
		port++
	}
}

// minioCfgFromFile returns a minio configuration from a file
func minioCfgFromFile(f string) (mk minioServer, err error) {
	in, err := os.Open(f)
	if err != nil {
		return mk, err
	}
	defer in.Close()
	b := bufio.NewReader(in)
	return minioCfgReader(b)
}

// WriteJSON returns the jobspec
func (m *minioServer) WriteJSON(w io.Writer) error {
	encode := json.NewEncoder(w)
	encode.SetIndent("", "  ")
	return encode.Encode(*m)
}

// minioKeysFromFile writes the minio keys to a file
func (m *minioServer) WriteToFile(f string) error {
	out, err := os.OpenFile(f, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0644)
	if err != nil {
		return err
	}
	defer out.Close()
	return m.WriteJSON(out)
}

// minioKeysReader takes an io.Reader and returns a minio cfg
func minioCfgReader(in io.Reader) (m minioServer, err error) {
	d, err := ioutil.ReadAll(in)
	if err != nil {
		return m, err
	}

	err = json.Unmarshal(d, &m)
	if err != nil {
		return m, err
	}
	return m, err
}

// getHostname gets the current hostname
func getHostname() string {
	data, _ := ioutil.ReadFile("/proc/sys/kernel/hostname")
	return strings.TrimSpace(string(data))
}
