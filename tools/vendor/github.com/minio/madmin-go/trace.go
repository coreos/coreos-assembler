//
// MinIO Object Storage (c) 2021 MinIO, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//

package madmin

import (
	"net/http"
	"time"
)

// TraceType indicates the type of the tracing Info
type TraceType int

const (
	// TraceHTTP tracing (MinIO S3 & Internode)
	TraceHTTP TraceType = iota
	// TraceOS tracing (Golang os package calls)
	TraceOS
	// TraceStorage tracing (MinIO Storage Layer)
	TraceStorage
)

// TraceInfo - represents a trace record, additionally
// also reports errors if any while listening on trace.
type TraceInfo struct {
	TraceType TraceType `json:"type"`

	NodeName string    `json:"nodename"`
	FuncName string    `json:"funcname"`
	Time     time.Time `json:"time"`

	ReqInfo   TraceRequestInfo  `json:"request"`
	RespInfo  TraceResponseInfo `json:"response"`
	CallStats TraceCallStats    `json:"stats"`

	StorageStats TraceStorageStats `json:"storageStats"`
	OSStats      TraceOSStats      `json:"osStats"`
}

// TraceStorageStats statistics on MinIO Storage layer calls
type TraceStorageStats struct {
	Path     string        `json:"path"`
	Duration time.Duration `json:"duration"`
}

// TraceOSStats statistics on operating system specific calls.
type TraceOSStats struct {
	Path     string        `json:"path"`
	Duration time.Duration `json:"duration"`
}

// TraceCallStats records request stats
type TraceCallStats struct {
	InputBytes      int           `json:"inputbytes"`
	OutputBytes     int           `json:"outputbytes"`
	Latency         time.Duration `json:"latency"`
	TimeToFirstByte time.Duration `json:"timetofirstbyte"`
}

// TraceRequestInfo represents trace of http request
type TraceRequestInfo struct {
	Time     time.Time   `json:"time"`
	Proto    string      `json:"proto"`
	Method   string      `json:"method"`
	Path     string      `json:"path,omitempty"`
	RawQuery string      `json:"rawquery,omitempty"`
	Headers  http.Header `json:"headers,omitempty"`
	Body     []byte      `json:"body,omitempty"`
	Client   string      `json:"client"`
}

// TraceResponseInfo represents trace of http request
type TraceResponseInfo struct {
	Time       time.Time   `json:"time"`
	Headers    http.Header `json:"headers,omitempty"`
	Body       []byte      `json:"body,omitempty"`
	StatusCode int         `json:"statuscode,omitempty"`
}
