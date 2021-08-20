package ocp

import (

	// minio is needed for moving files around in OpenShift.

	"bufio"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/minio/minio-go/v7/pkg/tags"
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
	ExternalServer bool   `json:"external_server"` //indicates that a server should not be started
	Host           string `json:"host"`
	Port           int    `json:"port"`
	Region         string `json:"region"`
	SecretKey      string `json:"secretkey"`
	Secure         bool   `json:"secure"` // indicates use of TLS

	// overSSH describes how to forward the Minio Port over SSH
	// This option is only useful with envVar CONTAINER_HOST running
	// in podman mode.
	overSSH *SSHForwardPort
	// sshStopCh is used to shutdown the SSH port forwarding.
	sshStopCh chan bool
	// sshErrCh is
	sshErrCh chan error

	dir          string
	minioOptions minio.Options
	cmd          *exec.Cmd
}

// StartStanaloneMinioServer starts a standalone minio server.
func StartStandaloneMinioServer(ctx context.Context, srvDir, cfgFile string, overSSH *SSHForwardPort) (*minioServer, error) {
	m := newMinioServer("")
	m.overSSH = overSSH
	m.dir = srvDir

	// Start the minio server. If we're forwarding over SSH we'll call
	// startMinioAndForwardOverSSH to start the minio server. because
	// the port we use will be dynamically chosen based on the SSH
	// connection.
	if m.overSSH == nil {
		if err := m.start(ctx); err != nil {
			return nil, err
		}
	} else {
		m.sshStopCh = make(chan bool, 1)
		m.sshErrCh = make(chan error, 256)
		if err := m.startMinioAndForwardOverSSH(ctx, m.sshStopCh, m.sshErrCh); err != nil {
			return nil, err
		}
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
		var ctx ClusterContext = context.Background()
		ip, err := getPodIP(ctx, ac, ns, host)
		if err == nil {
			host = ip
		}
	}

	log.Info("Defining a new minio server")
	minioAccessKey, _ := randomString(12)
	minioSecretKey, _ := randomString(12)

	m := &minioServer{
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

	if m.overSSH != nil {
		m.Host = "127.0.0.1"
	}

	return m
}

// GetClient returns a Minio Client
func (m *minioServer) client() (*minio.Client, error) {
	region := m.Region
	var secure bool
	if m.ExternalServer {
		if strings.Contains(m.Host, "s3.amazonaws.com") {
			secure = true
			if m.Region == "" {
				region = "us-east-1"
			}
		}
	}
	return minio.New(fmt.Sprintf("%s:%d", m.Host, m.Port),
		&minio.Options{
			Transport: &http.Transport{
				MaxIdleConns:       10,
				IdleConnTimeout:    0,
				DisableCompression: false, // force compression
			},
			Creds:  credentials.NewStaticV4(m.AccessKey, m.SecretKey, ""),
			Secure: secure,
			Region: region,
		},
	)
}

// start executes the minio server and returns an error if not ready.
func (m *minioServer) start(ctx context.Context) error {
	if m.ExternalServer {
		return <-m.waitReadyChan(90 * time.Second)
	}

	if err := retry(5, 1*time.Second, func() error { return m.exec(ctx) }); err != nil {
		log.WithError(err).Warn("failed to start minio")
		return err
	}

	return nil
}

// exec runs the minio command
func (m *minioServer) exec(ctx context.Context) error {
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

	absPath, err := filepath.Abs(m.dir)
	if err != nil {
		return err
	}

	cport := getPortOrNext(4747)

	addr := fmt.Sprintf(":%d", m.Port)
	args := []string{
		mpath, "server",
		"--quiet", "--anonymous",
		"--console-address", fmt.Sprintf(":%d", cport),
		"--address", addr,
		absPath,
	}
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Foreground: false,           // Background the process
		Pdeathsig:  syscall.SIGTERM, // Let minio finish before killing
		Pgid:       0,               // Use the pid of the minio as the pgroup id
		Setpgid:    true,            // Set the pgroup
	}
	cmd.Dir = absPath
	cmd.Env = append(
		os.Environ(),
		fmt.Sprintf("MINIO_ACCESS_KEY=%s", m.AccessKey),
		fmt.Sprintf("MINIO_SECRET_KEY=%s", m.SecretKey),
	)

	outPipe, _ := cmd.StdoutPipe()
	errPipe, sErr := cmd.StderrPipe()
	if sErr != nil {
		return fmt.Errorf("failed to start get output pipe: %v", sErr)
	}

	if err := cmd.Start(); err != nil {
		l.WithFields(log.Fields{
			"err": err,
		}).Error("Failed to start minio")
		return err
	}

	minioMsgChan := make(chan string, 1)
	go func() {
		s := bufio.NewScanner(io.MultiReader(outPipe, errPipe))
		for s.Scan() {
			minioMsgChan <- s.Text()
		}
	}()

	startChan := make(chan error, 1)
	go func() {
		for {
			if cmd == nil || (cmd.ProcessState != nil && cmd.ProcessState.Exited()) {
				startChan <- fmt.Errorf("minio started but exited")
				return

			}
			if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
				startChan <- fmt.Errorf("failed to start minio")
				return
			}
		}
	}()

	// Ensure the process gets terminated on shutdown
	sigs := make(chan os.Signal, 64)
	go func() {
		signal.Notify(sigs, os.Interrupt, syscall.SIGTERM, syscall.SIGUSR1, syscall.SIGUSR2)
		<-sigs
		m.Kill()
	}()

	m.cmd = cmd
	for {
		select {
		case err := <-startChan:
			if cmd != nil {
				stdoutStderr, _ := cmd.CombinedOutput()
				l.WithFields(log.Fields{
					"err": err,
					"out": stdoutStderr,
				}).Errorf("minio start failure")
			}
			return err
		case msg := <-minioMsgChan:
			fmt.Printf("MINIO: %s\n", msg)
		case <-sigs:
			return fmt.Errorf("minio startup was interrupted")
		case err := <-m.waitReadyChan(90 * time.Second):
			return err
		}
	}
}

// Wait blocks until Minio is finished.
func (m *minioServer) Wait() {
	if m.cmd != nil {
		_ = m.cmd.Wait()
	}
}

// Kill terminates the minio server.
func (m *minioServer) Kill() {
	if m.cmd == nil {
		return
	}

	// Kill any forward SSH connections.
	if m.overSSH != nil && m.sshStopCh != nil {
		m.sshStopCh <- true
	}

	// Note the "-" before the processes PID. A negative pid to
	// syscall.Kill kills the processes Pid group ensuring all forks/execs
	// of minio are killed too.
	_ = syscall.Kill(-m.cmd.Process.Pid, syscall.SIGTERM)

	// Wait for the command to end.
	m.Wait()

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

	if be, err := mc.BucketExists(ctx, bucket); err != nil {
		return err
	} else if be {
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
	return retry(
		3,
		(3 * time.Second),
		func() error {
			if m.Host == "" {
				return errors.New("host is undefined")
			}
			// Set the attributes
			f, isFile := dest.(*os.File)
			l := log.WithFields(log.Fields{
				"bucket": bucket,
				"host":   m.Host,
				"object": object,
			})

			l.Info("Requesting remote object")

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
			if err != nil {
				l.WithError(err).Error("failed to write the file")
				return err
			}
			l.WithField("bytes", n).Info("Wrote file")

			if isFile {
				info, err := src.Stat()
				if err != nil {
					return err
				}
				if err := os.Chtimes(f.Name(), info.LastModified, info.LastModified); err != nil {
					return err
				}
			}
			return nil
		})
}

// putter uploads the contents of an io.Reader to a remote MinioServer
func (m *minioServer) putter(ctx context.Context, bucket, object, fpath string) error {
	return retry(
		3,
		(3 * time.Second),
		func() error {
			if err := m.ensureBucketExists(ctx, bucket); err != nil {
				return fmt.Errorf("unable to validate %s bucket exists: %w", bucket, err)
			}
			fi, err := os.Stat(fpath)
			if err != nil {
				return err
			}
			l := log.WithFields(log.Fields{
				"bucket": bucket,
				"from":   fpath,
				"func":   "putter",
				"object": object,
				"size":   fmt.Sprintf("%d", fi.Size()),
			})

			mC, err := m.client()
			if err != nil {
				return err
			}

			i, err := mC.FPutObject(ctx, bucket, object, fpath, minio.PutObjectOptions{})
			if err != nil {
				return fmt.Errorf("failed to upload to %s/%s: %w", bucket, object, err)
			}
			if err := m.stampFile(bucket, object); err != nil {
				return fmt.Errorf("failed to stamp uploaded file %s/%s: %w", bucket, object, err)
			}
			stamp, _ := m.getStamp(bucket, object)
			l.WithFields(log.Fields{
				fileStampName: stamp,
				"etag":        i.ETag,
				"remote size": i.Size,
			}).Info("Uploaded")

			return nil
		})
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

// Exists check if bucket/object exists.
func (m *minioServer) Exists(bucket, object string) bool {
	mc, err := m.client()
	if err != nil {
		return false
	}
	if _, err := mc.StatObject(context.Background(), bucket, object, minio.GetObjectOptions{}); err != nil {
		return false
	}
	return true
}

const fileStampName = "gangplank.coreos.com/cosa/stamp"

// newFileStamp returns the Unix nanoseconds of the file as a string
// We use Unix nanoseconds for precision.
func newFileStamp() string {
	return fmt.Sprintf("%d", time.Now().UTC().UnixNano())
}

// stampFile add the unique stamp
func (m *minioServer) stampFile(bucket, object string) error {
	mc, err := m.client()
	if err != nil {
		return err
	}

	tagMap := map[string]string{
		fileStampName: newFileStamp(),
	}

	t, err := tags.NewTags(tagMap, true)
	if err != nil {
		return err
	}

	return mc.PutObjectTagging(context.Background(), bucket, object, t, minio.PutObjectTaggingOptions{})
}

// isLocalNewer checks if the file is newer than the remote file, if any. If the file
// does not exist remotely, then it is considered newer.
func (m *minioServer) isLocalNewer(bucket, object string, path string) (bool, error) {
	curStamp, err := m.getStamp(bucket, object)
	if err != nil {
		return true, err
	}
	modTime, err := getLocalFileStamp(path)
	if err != nil {
		return false, err
	}
	if modTime > curStamp {
		return true, nil
	}
	return false, nil
}

// getLocalFileStamp returns the local file mod time in UTC Unix epic nanoseconds.
func getLocalFileStamp(path string) (int64, error) {
	f, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	modTime := f.ModTime().UTC().UnixNano()
	return modTime, nil
}

// getStamp returns the stamp. If the file does not exist remotely the stamp of
// zero is returned. If the file exists but has not been stamped, then UTC
// Unix epic in nanoseconds of the modification time is used (the stamps are lost
// when the minio instance is reaped). The obvious flaw is that this does require
// all hosts to have coordinate time; this should be the case for Kubernetes cluster
// and podman based builds will always use the same time source.
func (m *minioServer) getStamp(bucket, object string) (int64, error) {
	mc, err := m.client()
	if err != nil {
		return 0, err
	}

	if !m.Exists(bucket, object) {
		return 0, nil
	}

	tags, err := mc.GetObjectTagging(context.Background(), bucket, object, minio.GetObjectTaggingOptions{})
	if err != nil {
		return 0, err
	}
	if tags == nil {
		return 0, nil
	}

	for k, v := range tags.ToMap() {
		if k == fileStampName {
			curStamp, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return 0, fmt.Errorf("failed to convert stamp %s to int64", v)
			}
			return curStamp, nil
		}
	}

	// fallback to modtime
	info, err := mc.StatObject(context.Background(), bucket, object, minio.GetObjectOptions{})
	if err == nil {
		return info.LastModified.UTC().UnixNano(), nil
	}

	return 0, err
}

// retry tries the func upto n "tries" with a sleep between.
func retry(tries int, sleep time.Duration, fn func() error) error {
	var err error
	for i := 0; i <= tries; i++ {
		newErr := fn()
		if newErr == nil {
			return nil
		}
		err = fmt.Errorf("error %d: %w", i, newErr)
		if sleep < 0 {
			time.Sleep(sleep)
		}
	}
	return err
}

// waitReadyChan returns a chan that emits true when the endpoint responds
func (m *minioServer) waitReadyChan(timeout time.Duration) <-chan error {
	readyChan := make(chan error)
	go func() {
		startTime := time.Now()
		for {
			// set a timeout
			if time.Since(startTime) > timeout {
				readyChan <- errors.New("timeout waiting for minio to be ready")
				return
			}

			mc, err := m.client()
			if err != nil {
				readyChan <- err
			}

			// Test if the remote bucket exists and that the error code does not
			// match a magic string.
			if _, err := mc.BucketExists(context.Background(), "testBucket"); err != nil {
				if strings.Contains(err.Error(), "Server not initialized, please try again.") ||
					strings.Contains(err.Error(), "connection refused") {
					time.Sleep(1 * time.Second)
					continue
				}
				readyChan <- err
			}
			readyChan <- nil

		}
	}()
	return readyChan
}
