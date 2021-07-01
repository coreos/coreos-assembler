// Copyright (c) 2015-2021 MinIO, Inc.
//
// This file is part of MinIO Object Storage stack
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package cmd

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"runtime"
	"runtime/pprof"
	"runtime/trace"
	"strings"
	"sync"
	"time"

	humanize "github.com/dustin/go-humanize"
	"github.com/gorilla/mux"
	"github.com/minio/madmin-go"
	miniogopolicy "github.com/minio/minio-go/v7/pkg/policy"
	"github.com/minio/minio/internal/handlers"
	xhttp "github.com/minio/minio/internal/http"
	"github.com/minio/minio/internal/logger"
	"github.com/minio/minio/internal/logger/message/audit"
	"github.com/minio/minio/internal/rest"
	"github.com/minio/pkg/certs"
)

const (
	slashSeparator = "/"
)

// BucketAccessPolicy - Collection of canned bucket policy at a given prefix.
type BucketAccessPolicy struct {
	Bucket string                     `json:"bucket"`
	Prefix string                     `json:"prefix"`
	Policy miniogopolicy.BucketPolicy `json:"policy"`
}

// IsErrIgnored returns whether given error is ignored or not.
func IsErrIgnored(err error, ignoredErrs ...error) bool {
	return IsErr(err, ignoredErrs...)
}

// IsErr returns whether given error is exact error.
func IsErr(err error, errs ...error) bool {
	for _, exactErr := range errs {
		if errors.Is(err, exactErr) {
			return true
		}
	}
	return false
}

func request2BucketObjectName(r *http.Request) (bucketName, objectName string) {
	path, err := getResource(r.URL.Path, r.Host, globalDomainNames)
	if err != nil {
		logger.CriticalIf(GlobalContext, err)
	}

	return path2BucketObject(path)
}

// path2BucketObjectWithBasePath returns bucket and prefix, if any,
// of a 'path'. basePath is trimmed from the front of the 'path'.
func path2BucketObjectWithBasePath(basePath, path string) (bucket, prefix string) {
	path = strings.TrimPrefix(path, basePath)
	path = strings.TrimPrefix(path, SlashSeparator)
	m := strings.Index(path, SlashSeparator)
	if m < 0 {
		return path, ""
	}
	return path[:m], path[m+len(SlashSeparator):]
}

func path2BucketObject(s string) (bucket, prefix string) {
	return path2BucketObjectWithBasePath("", s)
}

func getReadQuorum(drive int) int {
	return drive - getDefaultParityBlocks(drive)
}

func getWriteQuorum(drive int) int {
	parity := getDefaultParityBlocks(drive)
	quorum := drive - parity
	if quorum == parity {
		quorum++
	}
	return quorum
}

// cloneMSS will clone a map[string]string.
// If input is nil an empty map is returned, not nil.
func cloneMSS(v map[string]string) map[string]string {
	r := make(map[string]string, len(v))
	for k, v := range v {
		r[k] = v
	}
	return r
}

// URI scheme constants.
const (
	httpScheme  = "http"
	httpsScheme = "https"
)

// nopCharsetConverter is a dummy charset convert which just copies input to output,
// it is used to ignore custom encoding charset in S3 XML body.
func nopCharsetConverter(label string, input io.Reader) (io.Reader, error) {
	return input, nil
}

// xmlDecoder provide decoded value in xml.
func xmlDecoder(body io.Reader, v interface{}, size int64) error {
	var lbody io.Reader
	if size > 0 {
		lbody = io.LimitReader(body, size)
	} else {
		lbody = body
	}
	d := xml.NewDecoder(lbody)
	// Ignore any encoding set in the XML body
	d.CharsetReader = nopCharsetConverter
	return d.Decode(v)
}

// hasContentMD5 returns true if Content-MD5 header is set.
func hasContentMD5(h http.Header) bool {
	_, ok := h[xhttp.ContentMD5]
	return ok
}

/// http://docs.aws.amazon.com/AmazonS3/latest/dev/UploadingObjects.html
const (
	// Maximum object size per PUT request is 5TB.
	// This is a divergence from S3 limit on purpose to support
	// use cases where users are going to upload large files
	// using 'curl' and presigned URL.
	globalMaxObjectSize = 5 * humanize.TiByte

	// Minimum Part size for multipart upload is 5MiB
	globalMinPartSize = 5 * humanize.MiByte

	// Maximum Part size for multipart upload is 5GiB
	globalMaxPartSize = 5 * humanize.GiByte

	// Maximum Part ID for multipart upload is 10000
	// (Acceptable values range from 1 to 10000 inclusive)
	globalMaxPartID = 10000

	// Default values used while communicating for gateway communication
	defaultDialTimeout = 5 * time.Second
)

// isMaxObjectSize - verify if max object size
func isMaxObjectSize(size int64) bool {
	return size > globalMaxObjectSize
}

// // Check if part size is more than maximum allowed size.
func isMaxAllowedPartSize(size int64) bool {
	return size > globalMaxPartSize
}

// Check if part size is more than or equal to minimum allowed size.
func isMinAllowedPartSize(size int64) bool {
	return size >= globalMinPartSize
}

// isMaxPartNumber - Check if part ID is greater than the maximum allowed ID.
func isMaxPartID(partID int) bool {
	return partID > globalMaxPartID
}

func contains(slice interface{}, elem interface{}) bool {
	v := reflect.ValueOf(slice)
	if v.Kind() == reflect.Slice {
		for i := 0; i < v.Len(); i++ {
			if v.Index(i).Interface() == elem {
				return true
			}
		}
	}
	return false
}

// profilerWrapper is created becauses pkg/profiler doesn't
// provide any API to calculate the profiler file path in the
// disk since the name of this latter is randomly generated.
type profilerWrapper struct {
	// Profile recorded at start of benchmark.
	base   []byte
	stopFn func() ([]byte, error)
	ext    string
}

// recordBase will record the profile and store it as the base.
func (p *profilerWrapper) recordBase(name string, debug int) {
	var buf bytes.Buffer
	p.base = nil
	err := pprof.Lookup(name).WriteTo(&buf, debug)
	if err != nil {
		return
	}
	p.base = buf.Bytes()
}

// Base returns the recorded base if any.
func (p profilerWrapper) Base() []byte {
	return p.base
}

// Stop the currently running benchmark.
func (p profilerWrapper) Stop() ([]byte, error) {
	return p.stopFn()
}

// Extension returns the extension without dot prefix.
func (p profilerWrapper) Extension() string {
	return p.ext
}

// Returns current profile data, returns error if there is no active
// profiling in progress. Stops an active profile.
func getProfileData() (map[string][]byte, error) {
	globalProfilerMu.Lock()
	defer globalProfilerMu.Unlock()

	if len(globalProfiler) == 0 {
		return nil, errors.New("profiler not enabled")
	}

	dst := make(map[string][]byte, len(globalProfiler))
	for typ, prof := range globalProfiler {
		// Stop the profiler
		var err error
		buf, err := prof.Stop()
		delete(globalProfiler, typ)
		if err == nil {
			dst[typ+"."+prof.Extension()] = buf
		}
		buf = prof.Base()
		if len(buf) > 0 {
			dst[typ+"-before"+"."+prof.Extension()] = buf
		}
	}
	return dst, nil
}

func setDefaultProfilerRates() {
	runtime.MemProfileRate = 4096      // 512K -> 4K - Must be constant throughout application lifetime.
	runtime.SetMutexProfileFraction(0) // Disable until needed
	runtime.SetBlockProfileRate(0)     // Disable until needed
}

// Starts a profiler returns nil if profiler is not enabled, caller needs to handle this.
func startProfiler(profilerType string) (minioProfiler, error) {
	var prof profilerWrapper
	prof.ext = "pprof"
	// Enable profiler and set the name of the file that pkg/pprof
	// library creates to store profiling data.
	switch madmin.ProfilerType(profilerType) {
	case madmin.ProfilerCPU:
		dirPath, err := ioutil.TempDir("", "profile")
		if err != nil {
			return nil, err
		}
		fn := filepath.Join(dirPath, "cpu.out")
		f, err := os.Create(fn)
		if err != nil {
			return nil, err
		}
		err = pprof.StartCPUProfile(f)
		if err != nil {
			return nil, err
		}
		prof.stopFn = func() ([]byte, error) {
			pprof.StopCPUProfile()
			err := f.Close()
			if err != nil {
				return nil, err
			}
			defer os.RemoveAll(dirPath)
			return ioutil.ReadFile(fn)
		}
	case madmin.ProfilerMEM:
		runtime.GC()
		prof.recordBase("heap", 0)
		prof.stopFn = func() ([]byte, error) {
			runtime.GC()
			var buf bytes.Buffer
			err := pprof.Lookup("heap").WriteTo(&buf, 0)
			return buf.Bytes(), err
		}
	case madmin.ProfilerBlock:
		runtime.SetBlockProfileRate(100)
		prof.stopFn = func() ([]byte, error) {
			var buf bytes.Buffer
			err := pprof.Lookup("block").WriteTo(&buf, 0)
			runtime.SetBlockProfileRate(0)
			return buf.Bytes(), err
		}
	case madmin.ProfilerMutex:
		prof.recordBase("mutex", 0)
		runtime.SetMutexProfileFraction(1)
		prof.stopFn = func() ([]byte, error) {
			var buf bytes.Buffer
			err := pprof.Lookup("mutex").WriteTo(&buf, 0)
			runtime.SetMutexProfileFraction(0)
			return buf.Bytes(), err
		}
	case madmin.ProfilerThreads:
		prof.recordBase("threadcreate", 0)
		prof.stopFn = func() ([]byte, error) {
			var buf bytes.Buffer
			err := pprof.Lookup("threadcreate").WriteTo(&buf, 0)
			return buf.Bytes(), err
		}
	case madmin.ProfilerGoroutines:
		prof.ext = "txt"
		prof.recordBase("goroutine", 1)
		prof.stopFn = func() ([]byte, error) {
			var buf bytes.Buffer
			err := pprof.Lookup("goroutine").WriteTo(&buf, 1)
			return buf.Bytes(), err
		}
	case madmin.ProfilerTrace:
		dirPath, err := ioutil.TempDir("", "profile")
		if err != nil {
			return nil, err
		}
		fn := filepath.Join(dirPath, "trace.out")
		f, err := os.Create(fn)
		if err != nil {
			return nil, err
		}
		err = trace.Start(f)
		if err != nil {
			return nil, err
		}
		prof.ext = "trace"
		prof.stopFn = func() ([]byte, error) {
			trace.Stop()
			err := f.Close()
			if err != nil {
				return nil, err
			}
			defer os.RemoveAll(dirPath)
			return ioutil.ReadFile(fn)
		}
	default:
		return nil, errors.New("profiler type unknown")
	}

	return prof, nil
}

// minioProfiler - minio profiler interface.
type minioProfiler interface {
	// Return base profile. 'nil' if none.
	Base() []byte
	// Stop the profiler
	Stop() ([]byte, error)
	// Return extension of profile
	Extension() string
}

// Global profiler to be used by service go-routine.
var globalProfiler map[string]minioProfiler
var globalProfilerMu sync.Mutex

// dump the request into a string in JSON format.
func dumpRequest(r *http.Request) string {
	header := r.Header.Clone()
	header.Set("Host", r.Host)
	// Replace all '%' to '%%' so that printer format parser
	// to ignore URL encoded values.
	rawURI := strings.Replace(r.RequestURI, "%", "%%", -1)
	req := struct {
		Method     string      `json:"method"`
		RequestURI string      `json:"reqURI"`
		Header     http.Header `json:"header"`
	}{r.Method, rawURI, header}

	var buffer bytes.Buffer
	enc := json.NewEncoder(&buffer)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(&req); err != nil {
		// Upon error just return Go-syntax representation of the value
		return fmt.Sprintf("%#v", req)
	}

	// Formatted string.
	return strings.TrimSpace(buffer.String())
}

// isFile - returns whether given path is a file or not.
func isFile(path string) bool {
	if fi, err := os.Stat(path); err == nil {
		return fi.Mode().IsRegular()
	}

	return false
}

// UTCNow - returns current UTC time.
func UTCNow() time.Time {
	return time.Now().UTC()
}

// GenETag - generate UUID based ETag
func GenETag() string {
	return ToS3ETag(getMD5Hash([]byte(mustGetUUID())))
}

// ToS3ETag - return checksum to ETag
func ToS3ETag(etag string) string {
	etag = canonicalizeETag(etag)

	if !strings.HasSuffix(etag, "-1") {
		// Tools like s3cmd uses ETag as checksum of data to validate.
		// Append "-1" to indicate ETag is not a checksum.
		etag += "-1"
	}

	return etag
}

func newInternodeHTTPTransport(tlsConfig *tls.Config, dialTimeout time.Duration) func() http.RoundTripper {
	// For more details about various values used here refer
	// https://golang.org/pkg/net/http/#Transport documentation
	tr := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           xhttp.DialContextWithDNSCache(globalDNSCache, xhttp.NewInternodeDialContext(dialTimeout)),
		MaxIdleConnsPerHost:   1024,
		WriteBufferSize:       32 << 10, // 32KiB moving up from 4KiB default
		ReadBufferSize:        32 << 10, // 32KiB moving up from 4KiB default
		IdleConnTimeout:       15 * time.Second,
		ResponseHeaderTimeout: 15 * time.Minute, // Set conservative timeouts for MinIO internode.
		TLSHandshakeTimeout:   15 * time.Second,
		ExpectContinueTimeout: 15 * time.Second,
		TLSClientConfig:       tlsConfig,
		// Go net/http automatically unzip if content-type is
		// gzip disable this feature, as we are always interested
		// in raw stream.
		DisableCompression: true,
	}

	// https://github.com/golang/go/issues/23559
	// https://github.com/golang/go/issues/42534
	// https://github.com/golang/go/issues/43989
	// https://github.com/golang/go/issues/33425
	// https://github.com/golang/go/issues/29246
	// if tlsConfig != nil {
	// 	trhttp2, _ := http2.ConfigureTransports(tr)
	// 	if trhttp2 != nil {
	// 		// ReadIdleTimeout is the timeout after which a health check using ping
	// 		// frame will be carried out if no frame is received on the
	// 		// connection. 5 minutes is sufficient time for any idle connection.
	// 		trhttp2.ReadIdleTimeout = 5 * time.Minute
	// 		// PingTimeout is the timeout after which the connection will be closed
	// 		// if a response to Ping is not received.
	// 		trhttp2.PingTimeout = dialTimeout
	// 		// DisableCompression, if true, prevents the Transport from
	// 		// requesting compression with an "Accept-Encoding: gzip"
	// 		trhttp2.DisableCompression = true
	// 	}
	// }

	return func() http.RoundTripper {
		return tr
	}
}

// Used by only proxied requests, specifically only supports HTTP/1.1
func newCustomHTTPProxyTransport(tlsConfig *tls.Config, dialTimeout time.Duration) func() *http.Transport {
	// For more details about various values used here refer
	// https://golang.org/pkg/net/http/#Transport documentation
	tr := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           xhttp.DialContextWithDNSCache(globalDNSCache, xhttp.NewInternodeDialContext(dialTimeout)),
		MaxIdleConnsPerHost:   1024,
		WriteBufferSize:       16 << 10, // 16KiB moving up from 4KiB default
		ReadBufferSize:        16 << 10, // 16KiB moving up from 4KiB default
		IdleConnTimeout:       15 * time.Second,
		ResponseHeaderTimeout: 30 * time.Minute, // Set larger timeouts for proxied requests.
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 10 * time.Second,
		TLSClientConfig:       tlsConfig,
		// Go net/http automatically unzip if content-type is
		// gzip disable this feature, as we are always interested
		// in raw stream.
		DisableCompression: true,
	}

	return func() *http.Transport {
		return tr
	}
}

func newCustomHTTPTransport(tlsConfig *tls.Config, dialTimeout time.Duration) func() *http.Transport {
	// For more details about various values used here refer
	// https://golang.org/pkg/net/http/#Transport documentation
	tr := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           xhttp.DialContextWithDNSCache(globalDNSCache, xhttp.NewInternodeDialContext(dialTimeout)),
		MaxIdleConnsPerHost:   1024,
		WriteBufferSize:       16 << 10, // 16KiB moving up from 4KiB default
		ReadBufferSize:        16 << 10, // 16KiB moving up from 4KiB default
		IdleConnTimeout:       15 * time.Second,
		ResponseHeaderTimeout: 3 * time.Minute, // Set conservative timeouts for MinIO internode.
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 10 * time.Second,
		TLSClientConfig:       tlsConfig,
		// Go net/http automatically unzip if content-type is
		// gzip disable this feature, as we are always interested
		// in raw stream.
		DisableCompression: true,
	}

	// https://github.com/golang/go/issues/23559
	// https://github.com/golang/go/issues/42534
	// https://github.com/golang/go/issues/43989
	// https://github.com/golang/go/issues/33425
	// https://github.com/golang/go/issues/29246
	// if tlsConfig != nil {
	// 	trhttp2, _ := http2.ConfigureTransports(tr)
	// 	if trhttp2 != nil {
	// 		// ReadIdleTimeout is the timeout after which a health check using ping
	// 		// frame will be carried out if no frame is received on the
	// 		// connection. 5 minutes is sufficient time for any idle connection.
	// 		trhttp2.ReadIdleTimeout = 5 * time.Minute
	// 		// PingTimeout is the timeout after which the connection will be closed
	// 		// if a response to Ping is not received.
	// 		trhttp2.PingTimeout = dialTimeout
	// 		// DisableCompression, if true, prevents the Transport from
	// 		// requesting compression with an "Accept-Encoding: gzip"
	// 		trhttp2.DisableCompression = true
	// 	}
	// }

	return func() *http.Transport {
		return tr
	}
}

// NewGatewayHTTPTransportWithClientCerts returns a new http configuration
// used while communicating with the cloud backends.
func NewGatewayHTTPTransportWithClientCerts(clientCert, clientKey string) *http.Transport {
	transport := newGatewayHTTPTransport(1 * time.Minute)
	if clientCert != "" && clientKey != "" {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		c, err := certs.NewManager(ctx, clientCert, clientKey, tls.LoadX509KeyPair)
		if err != nil {
			logger.LogIf(ctx, fmt.Errorf("failed to load client key and cert, please check your endpoint configuration: %s",
				err.Error()))
		}
		if c != nil {
			transport.TLSClientConfig.GetClientCertificate = c.GetClientCertificate
		}
	}
	return transport
}

// NewGatewayHTTPTransport returns a new http configuration
// used while communicating with the cloud backends.
func NewGatewayHTTPTransport() *http.Transport {
	return newGatewayHTTPTransport(1 * time.Minute)
}

func newGatewayHTTPTransport(timeout time.Duration) *http.Transport {
	tr := newCustomHTTPTransport(&tls.Config{
		RootCAs: globalRootCAs,
	}, defaultDialTimeout)()

	// Customize response header timeout for gateway transport.
	tr.ResponseHeaderTimeout = timeout
	return tr
}

// NewRemoteTargetHTTPTransport returns a new http configuration
// used while communicating with the remote replication targets.
func NewRemoteTargetHTTPTransport() *http.Transport {
	// For more details about various values used here refer
	// https://golang.org/pkg/net/http/#Transport documentation
	tr := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   15 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConnsPerHost:   1024,
		WriteBufferSize:       16 << 10, // 16KiB moving up from 4KiB default
		ReadBufferSize:        16 << 10, // 16KiB moving up from 4KiB default
		IdleConnTimeout:       15 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ExpectContinueTimeout: 5 * time.Second,
		TLSClientConfig: &tls.Config{
			RootCAs: globalRootCAs,
		},
		// Go net/http automatically unzip if content-type is
		// gzip disable this feature, as we are always interested
		// in raw stream.
		DisableCompression: true,
	}
	return tr
}

// Load the json (typically from disk file).
func jsonLoad(r io.ReadSeeker, data interface{}) error {
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return err
	}
	return json.NewDecoder(r).Decode(data)
}

// Save to disk file in json format.
func jsonSave(f interface {
	io.WriteSeeker
	Truncate(int64) error
}, data interface{}) error {
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	if err = f.Truncate(0); err != nil {
		return err
	}
	if _, err = f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	_, err = f.Write(b)
	if err != nil {
		return err
	}
	return nil
}

// ceilFrac takes a numerator and denominator representing a fraction
// and returns its ceiling. If denominator is 0, it returns 0 instead
// of crashing.
func ceilFrac(numerator, denominator int64) (ceil int64) {
	if denominator == 0 {
		// do nothing on invalid input
		return
	}
	// Make denominator positive
	if denominator < 0 {
		numerator = -numerator
		denominator = -denominator
	}
	ceil = numerator / denominator
	if numerator > 0 && numerator%denominator != 0 {
		ceil++
	}
	return
}

// pathClean is like path.Clean but does not return "." for
// empty inputs, instead returns "empty" as is.
func pathClean(p string) string {
	cp := path.Clean(p)
	if cp == "." {
		return ""
	}
	return cp
}

func trimLeadingSlash(ep string) string {
	if len(ep) > 0 && ep[0] == '/' {
		// Path ends with '/' preserve it
		if ep[len(ep)-1] == '/' && len(ep) > 1 {
			ep = path.Clean(ep)
			ep += slashSeparator
		} else {
			ep = path.Clean(ep)
		}
		ep = ep[1:]
	}
	return ep
}

// unescapeGeneric is similar to url.PathUnescape or url.QueryUnescape
// depending on input, additionally also handles situations such as
// `//` are normalized as `/`, also removes any `/` prefix before
// returning.
func unescapeGeneric(p string, escapeFn func(string) (string, error)) (string, error) {
	ep, err := escapeFn(p)
	if err != nil {
		return "", err
	}
	return trimLeadingSlash(ep), nil
}

// unescapePath is similar to unescapeGeneric but for specifically
// path unescaping.
func unescapePath(p string) (string, error) {
	return unescapeGeneric(p, url.PathUnescape)
}

// similar to unescapeGeneric but never returns any error if the unescaping
// fails, returns the input as is in such occasion, not meant to be
// used where strict validation is expected.
func likelyUnescapeGeneric(p string, escapeFn func(string) (string, error)) string {
	ep, err := unescapeGeneric(p, escapeFn)
	if err != nil {
		return p
	}
	return ep
}

// Returns context with ReqInfo details set in the context.
func newContext(r *http.Request, w http.ResponseWriter, api string) context.Context {
	vars := mux.Vars(r)
	bucket := vars["bucket"]
	object := likelyUnescapeGeneric(vars["object"], url.PathUnescape)
	prefix := likelyUnescapeGeneric(vars["prefix"], url.QueryUnescape)
	if prefix != "" {
		object = prefix
	}
	reqInfo := &logger.ReqInfo{
		DeploymentID: globalDeploymentID,
		RequestID:    w.Header().Get(xhttp.AmzRequestID),
		RemoteHost:   handlers.GetSourceIP(r),
		Host:         getHostName(r),
		UserAgent:    r.UserAgent(),
		API:          api,
		BucketName:   bucket,
		ObjectName:   object,
	}
	return logger.SetReqInfo(r.Context(), reqInfo)
}

// Used for registering with rest handlers (have a look at registerStorageRESTHandlers for usage example)
// If it is passed ["aaaa", "bbbb"], it returns ["aaaa", "{aaaa:.*}", "bbbb", "{bbbb:.*}"]
func restQueries(keys ...string) []string {
	var accumulator []string
	for _, key := range keys {
		accumulator = append(accumulator, key, "{"+key+":.*}")
	}
	return accumulator
}

// Suffix returns the longest common suffix of the provided strings
func lcpSuffix(strs []string) string {
	return lcp(strs, false)
}

func lcp(strs []string, pre bool) string {
	// short-circuit empty list
	if len(strs) == 0 {
		return ""
	}
	xfix := strs[0]
	// short-circuit single-element list
	if len(strs) == 1 {
		return xfix
	}
	// compare first to rest
	for _, str := range strs[1:] {
		xfixl := len(xfix)
		strl := len(str)
		// short-circuit empty strings
		if xfixl == 0 || strl == 0 {
			return ""
		}
		// maximum possible length
		maxl := xfixl
		if strl < maxl {
			maxl = strl
		}
		// compare letters
		if pre {
			// prefix, iterate left to right
			for i := 0; i < maxl; i++ {
				if xfix[i] != str[i] {
					xfix = xfix[:i]
					break
				}
			}
		} else {
			// suffix, iterate right to left
			for i := 0; i < maxl; i++ {
				xi := xfixl - i - 1
				si := strl - i - 1
				if xfix[xi] != str[si] {
					xfix = xfix[xi+1:]
					break
				}
			}
		}
	}
	return xfix
}

// Returns the mode in which MinIO is running
func getMinioMode() string {
	mode := globalMinioModeFS
	if globalIsDistErasure {
		mode = globalMinioModeDistErasure
	} else if globalIsErasure {
		mode = globalMinioModeErasure
	} else if globalIsGateway {
		mode = globalMinioModeGatewayPrefix + globalGatewayName
	}
	return mode
}

func iamPolicyClaimNameOpenID() string {
	return globalOpenIDConfig.ClaimPrefix + globalOpenIDConfig.ClaimName
}

func iamPolicyClaimNameSA() string {
	return "sa-policy"
}

// timedValue contains a synchronized value that is considered valid
// for a specific amount of time.
// An Update function must be set to provide an updated value when needed.
type timedValue struct {
	// Update must return an updated value.
	// If an error is returned the cached value is not set.
	// Only one caller will call this function at any time, others will be blocking.
	// The returned value can no longer be modified once returned.
	// Should be set before calling Get().
	Update func() (interface{}, error)

	// TTL for a cached value.
	// If not set 1 second TTL is assumed.
	// Should be set before calling Get().
	TTL time.Duration

	// Once can be used to initialize values for lazy initialization.
	// Should be set before calling Get().
	Once sync.Once

	// Managed values.
	value      interface{}
	lastUpdate time.Time
	mu         sync.RWMutex
}

// Get will return a cached value or fetch a new one.
// If the Update function returns an error the value is forwarded as is and not cached.
func (t *timedValue) Get() (interface{}, error) {
	v := t.get()
	if v != nil {
		return v, nil
	}

	v, err := t.Update()
	if err != nil {
		return v, err
	}

	t.update(v)
	return v, nil
}

func (t *timedValue) get() (v interface{}) {
	ttl := t.TTL
	if ttl <= 0 {
		ttl = time.Second
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	v = t.value
	if time.Since(t.lastUpdate) < ttl {
		return v
	}
	return nil
}

func (t *timedValue) update(v interface{}) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.value = v
	t.lastUpdate = time.Now()
}

// On MinIO a directory object is stored as a regular object with "__XLDIR__" suffix.
// For ex. "prefix/" is stored as "prefix__XLDIR__"
func encodeDirObject(object string) string {
	if HasSuffix(object, slashSeparator) {
		return strings.TrimSuffix(object, slashSeparator) + globalDirSuffix
	}
	return object
}

// Reverse process of encodeDirObject()
func decodeDirObject(object string) string {
	if HasSuffix(object, globalDirSuffix) {
		return strings.TrimSuffix(object, globalDirSuffix) + slashSeparator
	}
	return object
}

// This is used by metrics to show the number of failed RPC calls
// between internodes
func loadAndResetRPCNetworkErrsCounter() uint64 {
	defer rest.ResetNetworkErrsCounter()
	return rest.GetNetworkErrsCounter()
}

// Helper method to return total number of nodes in cluster
func totalNodeCount() uint64 {
	peers, _ := globalEndpoints.peers()
	totalNodesCount := uint64(len(peers))
	if totalNodesCount == 0 {
		totalNodesCount = 1 // For standalone erasure coding
	}
	return totalNodesCount
}

// AuditLogOptions takes options for audit logging subsystem activity
type AuditLogOptions struct {
	Trigger   string
	APIName   string
	Status    string
	VersionID string
}

// sends audit logs for internal subsystem activity
func auditLogInternal(ctx context.Context, bucket, object string, opts AuditLogOptions) {
	entry := audit.NewEntry(globalDeploymentID)
	entry.Trigger = opts.Trigger
	entry.API.Name = opts.APIName
	entry.API.Bucket = bucket
	entry.API.Object = object
	if opts.VersionID != "" {
		entry.ReqQuery = make(map[string]string)
		entry.ReqQuery[xhttp.VersionID] = opts.VersionID
	}
	entry.API.Status = opts.Status
	ctx = logger.SetAuditEntry(ctx, &entry)
	logger.AuditLog(ctx, nil, nil, nil)
}
