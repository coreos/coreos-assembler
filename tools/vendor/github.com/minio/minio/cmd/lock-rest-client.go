/*
 * MinIO Cloud Storage, (C) 2019 MinIO, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package cmd

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net/url"
	"strconv"

	"github.com/minio/minio/cmd/http"
	xhttp "github.com/minio/minio/cmd/http"
	"github.com/minio/minio/cmd/rest"
	"github.com/minio/minio/pkg/dsync"
)

// lockRESTClient is authenticable lock REST client
type lockRESTClient struct {
	restClient *rest.Client
	endpoint   Endpoint
}

func toLockError(err error) error {
	if err == nil {
		return nil
	}

	switch err.Error() {
	case errLockConflict.Error():
		return errLockConflict
	case errLockNotExpired.Error():
		return errLockNotExpired
	}
	return err
}

// String stringer *dsync.NetLocker* interface compatible method.
func (client *lockRESTClient) String() string {
	return client.endpoint.String()
}

// Wrapper to restClient.Call to handle network errors, in case of network error the connection is marked disconnected
// permanently. The only way to restore the connection is at the xl-sets layer by xlsets.monitorAndConnectEndpoints()
// after verifying format.json
func (client *lockRESTClient) callWithContext(ctx context.Context, method string, values url.Values, body io.Reader, length int64) (respBody io.ReadCloser, err error) {
	if values == nil {
		values = make(url.Values)
	}

	respBody, err = client.restClient.Call(ctx, method, values, body, length)
	if err == nil {
		return respBody, nil
	}

	return nil, toLockError(err)
}

// IsOnline - returns whether REST client failed to connect or not.
func (client *lockRESTClient) IsOnline() bool {
	return client.restClient.IsOnline()
}

// Not a local locker
func (client *lockRESTClient) IsLocal() bool {
	return false
}

// Close - marks the client as closed.
func (client *lockRESTClient) Close() error {
	client.restClient.Close()
	return nil
}

// restCall makes a call to the lock REST server.
func (client *lockRESTClient) restCall(ctx context.Context, call string, args dsync.LockArgs) (reply bool, err error) {
	values := url.Values{}
	values.Set(lockRESTUID, args.UID)
	values.Set(lockRESTOwner, args.Owner)
	values.Set(lockRESTSource, args.Source)
	values.Set(lockRESTQuorum, strconv.Itoa(args.Quorum))
	var buffer bytes.Buffer
	for _, resource := range args.Resources {
		buffer.WriteString(resource)
		buffer.WriteString("\n")
	}
	respBody, err := client.callWithContext(ctx, call, values, &buffer, -1)
	defer http.DrainBody(respBody)
	switch err {
	case nil:
		return true, nil
	case errLockConflict, errLockNotExpired:
		return false, nil
	default:
		return false, err
	}
}

// RLock calls read lock REST API.
func (client *lockRESTClient) RLock(ctx context.Context, args dsync.LockArgs) (reply bool, err error) {
	return client.restCall(ctx, lockRESTMethodRLock, args)
}

// Lock calls lock REST API.
func (client *lockRESTClient) Lock(ctx context.Context, args dsync.LockArgs) (reply bool, err error) {
	return client.restCall(ctx, lockRESTMethodLock, args)
}

// RUnlock calls read unlock REST API.
func (client *lockRESTClient) RUnlock(args dsync.LockArgs) (reply bool, err error) {
	return client.restCall(context.Background(), lockRESTMethodRUnlock, args)
}

// Unlock calls write unlock RPC.
func (client *lockRESTClient) Unlock(args dsync.LockArgs) (reply bool, err error) {
	return client.restCall(context.Background(), lockRESTMethodUnlock, args)
}

// Expired calls expired handler to check if lock args have expired.
func (client *lockRESTClient) Expired(ctx context.Context, args dsync.LockArgs) (expired bool, err error) {
	return client.restCall(ctx, lockRESTMethodExpired, args)
}

func newLockAPI(endpoint Endpoint) dsync.NetLocker {
	if endpoint.IsLocal {
		return globalLockServers[endpoint]
	}
	return newlockRESTClient(endpoint)
}

// Returns a lock rest client.
func newlockRESTClient(endpoint Endpoint) *lockRESTClient {
	serverURL := &url.URL{
		Scheme: endpoint.Scheme,
		Host:   endpoint.Host,
		Path:   pathJoin(lockRESTPrefix, endpoint.Path, lockRESTVersion),
	}

	var tlsConfig *tls.Config
	if globalIsSSL {
		tlsConfig = &tls.Config{
			ServerName: endpoint.Hostname(),
			RootCAs:    globalRootCAs,
		}
	}

	trFn := newInternodeHTTPTransport(tlsConfig, rest.DefaultTimeout)
	restClient := rest.NewClient(serverURL, trFn, newAuthToken)
	restClient.HealthCheckFn = func() bool {
		ctx, cancel := context.WithTimeout(GlobalContext, restClient.HealthCheckTimeout)
		// Instantiate a new rest client for healthcheck
		// to avoid recursive healthCheckFn()
		respBody, err := rest.NewClient(serverURL, trFn, newAuthToken).Call(ctx, lockRESTMethodHealth, nil, nil, -1)
		xhttp.DrainBody(respBody)
		cancel()
		var ne *rest.NetworkError
		return !errors.Is(err, context.DeadlineExceeded) && !errors.As(err, &ne)
	}

	return &lockRESTClient{endpoint: endpoint, restClient: restClient}
}
