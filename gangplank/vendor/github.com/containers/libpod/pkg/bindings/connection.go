package bindings

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/containers/libpod/pkg/api/handlers"
	jsoniter "github.com/json-iterator/go"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"golang.org/x/crypto/ssh"
	"k8s.io/client-go/util/homedir"
)

var (
	basePath = &url.URL{
		Scheme: "http",
		Host:   "d",
		Path:   "/v" + handlers.MinimalApiVersion + "/libpod",
	}
)

type APIResponse struct {
	*http.Response
	Request *http.Request
}

type Connection struct {
	_url   *url.URL
	client *http.Client
}

type valueKey string

const (
	clientKey = valueKey("client")
)

// GetClient from context build by NewConnection()
func GetClient(ctx context.Context) (*Connection, error) {
	c, ok := ctx.Value(clientKey).(*Connection)
	if !ok {
		return nil, errors.Errorf("ClientKey not set in context")
	}
	return c, nil
}

// JoinURL elements with '/'
func JoinURL(elements ...string) string {
	return strings.Join(elements, "/")
}

// NewConnection takes a URI as a string and returns a context with the
// Connection embedded as a value.  This context needs to be passed to each
// endpoint to work correctly.
//
// A valid URI connection should be scheme://
// For example tcp://localhost:<port>
// or unix:///run/podman/podman.sock
// or ssh://<user>@<host>[:port]/run/podman/podman.sock?secure=True
func NewConnection(ctx context.Context, uri string, identity ...string) (context.Context, error) {
	var (
		err    error
		secure bool
	)
	if v, found := os.LookupEnv("PODMAN_HOST"); found {
		uri = v
	}

	if v, found := os.LookupEnv("PODMAN_SSHKEY"); found {
		identity = []string{v}
	}

	_url, err := url.Parse(uri)
	if err != nil {
		return nil, errors.Wrapf(err, "Value of PODMAN_HOST is not a valid url: %s", uri)
	}

	// Now we setup the http client to use the connection above
	var client *http.Client
	switch _url.Scheme {
	case "ssh":
		secure, err = strconv.ParseBool(_url.Query().Get("secure"))
		if err != nil {
			secure = false
		}
		client, err = sshClient(_url, identity[0], secure)
	case "unix":
		if !strings.HasPrefix(uri, "unix:///") {
			// autofix unix://path_element vs unix:///path_element
			_url.Path = JoinURL(_url.Host, _url.Path)
			_url.Host = ""
		}
		client, err = unixClient(_url)
	case "tcp":
		if !strings.HasPrefix(uri, "tcp://") {
			return nil, errors.New("tcp URIs should begin with tcp://")
		}
		client, err = tcpClient(_url)
	default:
		return nil, errors.Errorf("'%s' is not a supported schema", _url.Scheme)
	}
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to create %sClient", _url.Scheme)
	}

	ctx = context.WithValue(ctx, clientKey, &Connection{_url, client})
	if err := pingNewConnection(ctx); err != nil {
		return nil, err
	}
	return ctx, nil
}

func tcpClient(_url *url.URL) (*http.Client, error) {
	return &http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("tcp", _url.Path)
			},
			DisableCompression: true,
		},
	}, nil
}

// pingNewConnection pings to make sure the RESTFUL service is up
// and running. it should only be used where initializing a connection
func pingNewConnection(ctx context.Context) error {
	client, err := GetClient(ctx)
	if err != nil {
		return err
	}
	// the ping endpoint sits at / in this case
	response, err := client.DoRequest(nil, http.MethodGet, "../../../_ping", nil)
	if err != nil {
		return err
	}
	if response.StatusCode == http.StatusOK {
		return nil
	}
	return errors.Errorf("ping response was %q", response.StatusCode)
}

func sshClient(_url *url.URL, identity string, secure bool) (*http.Client, error) {
	auth, err := publicKey(identity)
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to parse identity %s: %v\n", _url.String(), identity)
	}

	callback := ssh.InsecureIgnoreHostKey()
	if secure {
		key := hostKey(_url.Hostname())
		if key != nil {
			callback = ssh.FixedHostKey(key)
		}
	}

	port := _url.Port()
	if port == "" {
		port = "22"
	}

	bastion, err := ssh.Dial("tcp",
		net.JoinHostPort(_url.Hostname(), port),
		&ssh.ClientConfig{
			User:            _url.User.Username(),
			Auth:            []ssh.AuthMethod{auth},
			HostKeyCallback: callback,
			HostKeyAlgorithms: []string{
				ssh.KeyAlgoRSA,
				ssh.KeyAlgoDSA,
				ssh.KeyAlgoECDSA256,
				ssh.KeyAlgoECDSA384,
				ssh.KeyAlgoECDSA521,
				ssh.KeyAlgoED25519,
			},
			Timeout: 5 * time.Second,
		},
	)
	if err != nil {
		return nil, errors.Wrapf(err, "Connection to bastion host (%s) failed.", _url.String())
	}
	return &http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return bastion.Dial("unix", _url.Path)
			},
		}}, nil
}

func unixClient(_url *url.URL) (*http.Client, error) {
	return &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				d := net.Dialer{}
				return d.DialContext(ctx, "unix", _url.Path)
			},
			DisableCompression: true,
		},
	}, nil
}

// DoRequest assembles the http request and returns the response
func (c *Connection) DoRequest(httpBody io.Reader, httpMethod, endpoint string, queryParams url.Values, pathValues ...string) (*APIResponse, error) {
	var (
		err      error
		response *http.Response
	)
	safePathValues := make([]interface{}, len(pathValues))
	// Make sure path values are http url safe
	for i, pv := range pathValues {
		safePathValues[i] = url.PathEscape(pv)
	}
	// Lets eventually use URL for this which might lead to safer
	// usage
	safeEndpoint := fmt.Sprintf(endpoint, safePathValues...)
	e := basePath.String() + safeEndpoint
	req, err := http.NewRequest(httpMethod, e, httpBody)
	if err != nil {
		return nil, err
	}
	if len(queryParams) > 0 {
		req.URL.RawQuery = queryParams.Encode()
	}
	// Give the Do three chances in the case of a comm/service hiccup
	for i := 0; i < 3; i++ {
		response, err = c.client.Do(req) // nolint
		if err == nil {
			break
		}
		time.Sleep(time.Duration(i*100) * time.Millisecond)
	}
	return &APIResponse{response, req}, err
}

// FiltersToString converts our typical filter format of a
// map[string][]string to a query/html safe string.
func FiltersToString(filters map[string][]string) (string, error) {
	lowerCaseKeys := make(map[string][]string)
	for k, v := range filters {
		lowerCaseKeys[strings.ToLower(k)] = v
	}
	return jsoniter.MarshalToString(lowerCaseKeys)
}

// IsInformation returns true if the response code is 1xx
func (h *APIResponse) IsInformational() bool {
	return h.Response.StatusCode/100 == 1
}

// IsSuccess returns true if the response code is 2xx
func (h *APIResponse) IsSuccess() bool {
	return h.Response.StatusCode/100 == 2
}

// IsRedirection returns true if the response code is 3xx
func (h *APIResponse) IsRedirection() bool {
	return h.Response.StatusCode/100 == 3
}

// IsClientError returns true if the response code is 4xx
func (h *APIResponse) IsClientError() bool {
	return h.Response.StatusCode/100 == 4
}

// IsServerError returns true if the response code is 5xx
func (h *APIResponse) IsServerError() bool {
	return h.Response.StatusCode/100 == 5
}

func publicKey(path string) (ssh.AuthMethod, error) {
	key, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return nil, err
	}

	return ssh.PublicKeys(signer), nil
}

func hostKey(host string) ssh.PublicKey {
	// parse OpenSSH known_hosts file
	// ssh or use ssh-keyscan to get initial key
	known_hosts := filepath.Join(homedir.HomeDir(), ".ssh", "known_hosts")
	fd, err := os.Open(known_hosts)
	if err != nil {
		logrus.Error(err)
		return nil
	}

	scanner := bufio.NewScanner(fd)
	for scanner.Scan() {
		_, hosts, key, _, _, err := ssh.ParseKnownHosts(scanner.Bytes())
		if err != nil {
			logrus.Errorf("Failed to parse known_hosts: %s", scanner.Text())
			continue
		}

		for _, h := range hosts {
			if h == host {
				return key
			}
		}
	}

	return nil
}
