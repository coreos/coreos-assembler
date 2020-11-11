package ocp

import "errors"

var (
	// ErrNoSuchCloud is returned when the cloud is unknown
	ErrNoSuchCloud = errors.New("unknown cloud credential type")

	// ErrNoOCPBuildSpec is raised when no OCP envvars are found
	ErrNoOCPBuildSpec = errors.New("no OCP Build specification found")

	// ErrNotInCluster is used to singal that the host is not running in a
	// Kubernetes cluster
	ErrNotInCluster = errors.New("host is not in kubernetes cluster")

	// ErrInvalidOCPMode is used when there is no valid/supported mode the OCP
	// package. Currently this is thrown when neither a build client or kubernetes API
	// client can be initalized.
	ErrInvalidOCPMode = errors.New("program is not running as a buildconfig or with valid kubernetes service account")

	// ErrNoSourceInput is used to signal no source found.
	ErrNoSourceInput = errors.New("no source repo or binary payload defined")

	// ErrNotWorkPod is returned when the pod is not a work pod
	ErrNotWorkPod = errors.New("not a work pod")

	// ErrNoWorkFound is returned when the build client is neither a
	// workPod or BuildConfig.
	ErrNoWorkFound = errors.New("neither a buildconfig or workspec found")
)
