package cosa

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

/*
	cosa_reader.go provides an interface for interacting with
	files through an "ioBackender." Any struct that inmplements ioBackender
	can read meta-data.

*/

// default ioBackend is file backend
var ioBackend ioBackender = new(ioBackendFile)

// ioBackendMinio is an ioBackender.
var _ ioBackender = &ioBackendMinio{}

// newBackend returns a new backend
func newBackend() ioBackender {
	var newBackender ioBackender = ioBackend
	return newBackender
}

// Open calls the backend's open function.
func Open(p string) (io.ReadCloser, error) {
	nb := newBackend()
	return nb.Open(p)
}

// ioBackender is the basic interface.
type ioBackender interface {
	Open(string) (io.ReadCloser, error)
}

// ioBackendFile is a file based backend
type ioBackendFile struct {
	*os.File
	path string
}

// Open implements ioBackender Open interface.
func (i *ioBackendFile) Open(p string) (io.ReadCloser, error) {
	f, err := os.Open(p)
	i.File = f
	i.path = p
	return f, err
}

func (i *ioBackendFile) Name() string {
	return i.path
}

// ioBackendMinio is a minio based backend
type ioBackendMinio struct {
	ctx  context.Context
	m    *minio.Client
	obj  *minio.Object
	name string

	bucket string
	prefix string
}

var ErrNoMinioClient = errors.New("minio client is not defined")

// getBucketAndPath returns the relative bucket and path.
func (im *ioBackendMinio) getBucketAndPath(p string) (string, string) {
	parts := strings.Split(p, "/")
	path := strings.Join(parts[1:], "/")

	bucket := parts[0]
	if im.bucket != "" {
		bucket = im.bucket
		path = p
	}
	if im.prefix != "" {
		path = filepath.Join(im.prefix, path)
	}
	return bucket, path
}

// Open implements ioBackender's and os.File's Open interface.
func (im *ioBackendMinio) Open(p string) (io.ReadCloser, error) {
	if im.m == nil {
		return nil, ErrNoMinioClient
	}

	bucket, path := im.getBucketAndPath(p)
	obj, err := im.m.GetObject(im.ctx, bucket, path, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	im.obj = obj
	im.name = p

	return obj, nil
}

// objectInfo holds basic information about either a file object
// or a remote minio object.
type objectInfo struct {
	info minio.ObjectInfo
	name string
}

// TODO: drop with GoLang 1.16. This is a backport of the interface from 1.16.
// var _ os.FileInfo = &objectInfo{}
type fileMode uint32
type fileInfo interface {
	Name() string       // base name of the file
	Size() int64        // length in bytes for regular files; system-dependent for others
	Mode() fileMode     // file mode bits
	ModTime() time.Time // modification time
	IsDir() bool        // abbreviation for Mode().IsDir()
	Sys() interface{}   // underlying data source (can return nil)
}

// objectInfo implements the os.FileInfo interface.
// This allows for abstracting any file or object to be compared as if they were
// local files regardless of location.
var _ fileInfo = &objectInfo{}

// IsDir implements the os.FileInfo IsDir func. For minio objects,
// the answer is always false.
func (ao *objectInfo) IsDir() bool {
	return false
}

// ModTime implements the os.FileInfo ModTime func. The returned value
// is remote aodification time.
func (ao *objectInfo) ModTime() time.Time {
	return ao.info.LastModified
}

// Mode implements the os.FileInfo Mode func. Since there is not simple
// way to convert an ACL into Unix permisions, it blindly returns 0644.
func (ao *objectInfo) Mode() fileMode {
	return 0644
}

// Name implements the os.FileInfo interface Name func.
func (ao *objectInfo) Name() string {
	return filepath.Base(ao.name)
}

// Size implements the os.FileInfo size func.
func (ao *objectInfo) Size() int64 {
	return ao.info.Size
}

// Sys implements the os.FileInfo interface Sys func. The interface spec allows
// for returning a nil.
func (ao *objectInfo) Sys() interface{} {
	return nil
}

// SetIOBackendMinio sets the backend to minio. The client must be provided
// by the caller, including authorization.
func SetIOBackendMinio(ctx context.Context, m *minio.Client, bucket, prefix string) error {
	if m == nil {
		return errors.New("minio client must not be nil")
	}

	log.WithFields(log.Fields{
		"bucket": bucket,
		"prefix": prefix,
	}).Info("minio bucket and prefix defined")

	backend := &ioBackendMinio{
		m:      m,
		ctx:    ctx,
		bucket: bucket,
		prefix: prefix,
	}
	ioBackend = backend
	walkFn = createMinioWalkFunc(m, bucket, prefix)
	return nil
}

// SetIOBackendFile sets the backend to the default file backend.
func SetIOBackendFile() {
	ioBackend = new(ioBackendFile)
}

// walkerFn is a function that implements the walk func
type walkerFn func(string) <-chan fileInfo

// walkFn is used to walk paths
var walkFn walkerFn = defaultWalkFunc

// defaultWalkFunc walks over a directory and returns a channel of os.FileInfo
func defaultWalkFunc(p string) <-chan fileInfo {
	ret := make(chan fileInfo)
	go func() {
		defer close(ret) //nolint
		_ = filepath.Walk(p, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			ret <- &objectInfo{
				name: filepath.Join(p, info.Name()),
				info: minio.ObjectInfo{
					Key:          info.Name(),
					Size:         info.Size(),
					LastModified: info.ModTime(),
				},
			}
			return nil
		})
	}()
	return ret
}

// createMinioWalkFunc creates a new func a minio client. The returned function
// will list the remote objects and return os.FileInfo compliant interfaces.
func createMinioWalkFunc(m *minio.Client, bucket, prefix string) walkerFn {
	return func(p string) <-chan fileInfo {
		ret := make(chan fileInfo)
		go func() {
			defer close(ret) //nolint
			ao := minio.ListObjectsOptions{
				Recursive: true,
			}
			if prefix != "" {
				ao.Prefix = prefix
			}
			info := m.ListObjects(context.Background(), bucket, ao)
			for {
				val, ok := <-info
				if !ok {
					return
				}
				ret <- &objectInfo{
					info: val,
					name: filepath.Join(bucket, val.Key),
				}
			}
		}()
		return ret
	}
}
