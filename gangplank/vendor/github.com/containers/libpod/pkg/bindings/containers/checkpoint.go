package containers

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	"github.com/containers/libpod/pkg/bindings"
	"github.com/containers/libpod/pkg/domain/entities"
)

// Checkpoint checkpoints the given container (identified by nameOrId).  All additional
// options are options and allow for more fine grained control of the checkpoint process.
func Checkpoint(ctx context.Context, nameOrId string, keep, leaveRunning, tcpEstablished, ignoreRootFS *bool, export *string) (*entities.CheckpointReport, error) {
	var report entities.CheckpointReport
	conn, err := bindings.GetClient(ctx)
	if err != nil {
		return nil, err
	}
	params := url.Values{}
	if keep != nil {
		params.Set("keep", strconv.FormatBool(*keep))
	}
	if leaveRunning != nil {
		params.Set("leaveRunning", strconv.FormatBool(*leaveRunning))
	}
	if tcpEstablished != nil {
		params.Set("TCPestablished", strconv.FormatBool(*tcpEstablished))
	}
	if ignoreRootFS != nil {
		params.Set("ignoreRootFS", strconv.FormatBool(*ignoreRootFS))
	}
	if export != nil {
		params.Set("export", *export)
	}
	response, err := conn.DoRequest(nil, http.MethodPost, "/containers/%s/checkpoint", params, nameOrId)
	if err != nil {
		return nil, err
	}
	return &report, response.Process(&report)
}

// Restore restores a checkpointed container to running. The container is identified by the nameOrId option. All
// additional options are optional and allow finer control of the restore processs.
func Restore(ctx context.Context, nameOrId string, keep, tcpEstablished, ignoreRootFS, ignoreStaticIP, ignoreStaticMAC *bool, name, importArchive *string) (*entities.RestoreReport, error) {
	var report entities.RestoreReport
	conn, err := bindings.GetClient(ctx)
	if err != nil {
		return nil, err
	}
	params := url.Values{}
	if keep != nil {
		params.Set("keep", strconv.FormatBool(*keep))
	}
	if tcpEstablished != nil {
		params.Set("TCPestablished", strconv.FormatBool(*tcpEstablished))
	}
	if ignoreRootFS != nil {
		params.Set("ignoreRootFS", strconv.FormatBool(*ignoreRootFS))
	}
	if ignoreStaticIP != nil {
		params.Set("ignoreStaticIP", strconv.FormatBool(*ignoreStaticIP))
	}
	if ignoreStaticMAC != nil {
		params.Set("ignoreStaticMAC", strconv.FormatBool(*ignoreStaticMAC))
	}
	if name != nil {
		params.Set("name", *name)
	}
	if importArchive != nil {
		params.Set("import", *importArchive)
	}
	response, err := conn.DoRequest(nil, http.MethodPost, "/containers/%s/restore", params, nameOrId)
	if err != nil {
		return nil, err
	}
	return &report, response.Process(&report)
}
