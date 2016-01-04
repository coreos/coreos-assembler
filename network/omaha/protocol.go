// Copyright 2013-2015 CoreOS, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Google's Omaha application update protocol, version 3.
//
// Omaha is a poll based protocol using XML. Requests are made by clients to
// check for updates or report events of an update process. Responses are given
// by the server to provide update information, if any, or to simply
// acknowledge the receipt of event status.
//
// https://github.com/google/omaha/blob/wiki/ServerProtocol.md
package omaha

import (
	"encoding/xml"

	"github.com/coreos/mantle/version"
)

// Request sent by the Omaha client
type Request struct {
	XMLName       xml.Name      `xml:"request" json:"-"`
	OS            *OS           `xml:"os"`
	Apps          []*AppRequest `xml:"app"`
	Protocol      string        `xml:"protocol,attr"`
	Version       string        `xml:"version,attr,omitempty"`
	IsMachine     string        `xml:"ismachine,attr,omitempty"`
	RequestId     string        `xml:"requestid,attr,omitempty"`
	SessionId     string        `xml:"sessionid,attr,omitempty"`
	UserId        string        `xml:"userid,attr,omitempty"`
	InstallSource string        `xml:"installsource,attr,omitempty"`
	TestSource    string        `xml:"testsource,attr,omitempty"`

	// update engine extension, duplicates the version attribute.
	UpdaterVersion string `xml:"updaterversion,attr,omitempty"`
}

func NewRequest() *Request {
	return &Request{
		Protocol: "3.0",
		Version:  version.Version,
		OS: &OS{
			Platform: LocalPlatform(),
			Arch:     LocalArch(),
			// TODO(marineam): Version and ServicePack
		},
	}
}

func (r *Request) AddApp(id, version string) *AppRequest {
	a := &AppRequest{Id: id, Version: version}
	r.Apps = append(r.Apps, a)
	return a
}

type AppRequest struct {
	Ping        *PingRequest    `xml:"ping"`
	UpdateCheck *UpdateRequest  `xml:"updatecheck"`
	Events      []*EventRequest `xml:"event" json:",omitempty"`
	Id          string          `xml:"appid,attr,omitempty"`
	Version     string          `xml:"version,attr,omitempty"`
	NextVersion string          `xml:"nextversion,attr,omitempty"`
	Lang        string          `xml:"lang,attr,omitempty"`
	Client      string          `xml:"client,attr,omitempty"`
	InstallAge  string          `xml:"installage,attr,omitempty"`

	// update engine extensions
	Track     string `xml:"track,attr,omitempty"`
	FromTrack string `xml:"from_track,attr,omitempty"`
	Board     string `xml:"board,attr,omitempty"`
	DeltaOK   bool   `xml:"delta_okay,attr,omitempty"`

	// coreos update engine extensions
	BootId       string `xml:"bootid,attr,omitempty"`
	MachineID    string `xml:"machineid,attr,omitempty"`
	OEM          string `xml:"oem,attr,omitempty"`
	OEMVersion   string `xml:"oemversion,attr,omitempty"`
	AlephVersion string `xml:"alephversion,attr,omitempty"`
}

func (a *AppRequest) AddUpdateCheck() *UpdateRequest {
	a.UpdateCheck = new(UpdateRequest)
	return a.UpdateCheck
}

func (a *AppRequest) AddPing() *PingRequest {
	a.Ping = &PingRequest{Active: 1}
	return a.Ping
}

func (a *AppRequest) AddEvent() *EventRequest {
	event := new(EventRequest)
	a.Events = append(a.Events, event)
	return event
}

type UpdateRequest struct {
	TargetVersionPrefix string `xml:"targetversionprefix,attr,omitempty"`
}

type PingRequest struct {
	Active               int `xml:"active,attr,omitempty"`
	LastActiveReportDays int `xml:"a,attr,omitempty"`
	LastReportDays       int `xml:"r,attr,omitempty"`
}

type EventRequest struct {
	Type            EventType   `xml:"eventtype,attr"`
	Result          EventResult `xml:"eventresult,attr"`
	NextVersion     string      `xml:"nextversion,attr,omitempty"`
	PreviousVersion string      `xml:"previousversion,attr,omitempty"`
	ErrorCode       string      `xml:"errorcode,attr,omitempty"`
}

// Response sent by the Omaha server
type Response struct {
	XMLName  xml.Name       `xml:"response" json:"-"`
	DayStart DayStart       `xml:"daystart"`
	Apps     []*AppResponse `xml:"app"`
	Protocol string         `xml:"protocol,attr"`
	Server   string         `xml:"server,attr"`
}

func NewResponse() *Response {
	return &Response{
		Protocol: "3.0",
		Server:   "mantle",
		DayStart: DayStart{ElapsedSeconds: "0"},
	}
}

type DayStart struct {
	ElapsedSeconds string `xml:"elapsed_seconds,attr"`
}

func (r *Response) AddApp(id string, status AppStatus) *AppResponse {
	a := &AppResponse{Id: id, Status: status}
	r.Apps = append(r.Apps, a)
	return a
}

type AppResponse struct {
	Ping        *PingResponse    `xml:"ping"`
	UpdateCheck *UpdateResponse  `xml:"updatecheck"`
	Events      []*EventResponse `xml:"event" json:",omitempty"`
	Id          string           `xml:"appid,attr,omitempty"`
	Status      AppStatus        `xml:"status,attr,omitempty"`
}

func (a *AppResponse) AddUpdateCheck(status UpdateStatus) *UpdateResponse {
	a.UpdateCheck = &UpdateResponse{Status: status}
	return a.UpdateCheck
}

func (a *AppResponse) AddPing() *PingResponse {
	a.Ping = &PingResponse{"ok"}
	return a.Ping
}

func (a *AppResponse) AddEvent() *EventResponse {
	event := &EventResponse{"ok"}
	a.Events = append(a.Events, event)
	return event
}

type UpdateResponse struct {
	URLs     []*URL       `xml:"urls>url" json:",omitempty"`
	Manifest *Manifest    `xml:"manifest"`
	Status   UpdateStatus `xml:"status,attr,omitempty"`
}

func (u *UpdateResponse) AddURL(codebase string) *URL {
	url := &URL{CodeBase: codebase}
	u.URLs = append(u.URLs, url)
	return url
}

func (u *UpdateResponse) AddManifest(version string) *Manifest {
	u.Manifest = &Manifest{Version: version}
	return u.Manifest
}

type PingResponse struct {
	Status string `xml:"status,attr"` // Always "ok".
}

type EventResponse struct {
	Status string `xml:"status,attr"` // Always "ok".
}

type OS struct {
	Platform    string `xml:"platform,attr,omitempty"`
	Version     string `xml:"version,attr,omitempty"`
	ServicePack string `xml:"sp,attr,omitempty"`
	Arch        string `xml:"arch,attr,omitempty"`
}

type Event struct {
	Type            EventType   `xml:"eventtype,attr"`
	Result          EventResult `xml:"eventresult,attr"`
	PreviousVersion string      `xml:"previousversion,attr,omitempty"`
	ErrorCode       string      `xml:"errorcode,attr,omitempty"`
	Status          string      `xml:"status,attr,omitempty"`
}

type URL struct {
	CodeBase string `xml:"codebase,attr"`
}

type Manifest struct {
	Packages []*Package `xml:"packages>package"`
	Actions  []*Action  `xml:"actions>action"`
	Version  string     `xml:"version,attr"`
}

func (m *Manifest) AddPackage() *Package {
	p := new(Package)
	m.Packages = append(m.Packages, p)
	return p
}

func (m *Manifest) AddPackageFromPath(path string) (*Package, error) {
	p := new(Package)
	if err := p.FromPath(path); err != nil {
		return nil, err
	}

	m.Packages = append(m.Packages, p)
	return p, nil
}

func (m *Manifest) AddAction(event string) *Action {
	a := &Action{Event: event}
	m.Actions = append(m.Actions, a)
	return a
}

type Action struct {
	Event string `xml:"event,attr"`

	// update engine extensions for event="postinstall"
	DisplayVersion        string `xml:"DisplayVersion,attr,omitempty"`
	Sha256                string `xml:"sha256,attr,omitempty"`
	NeedsAdmin            bool   `xml:"needsadmin,attr,omitempty"`
	IsDeltaPayload        bool   `xml:"IsDeltaPayload,attr,omitempty"`
	DisablePayloadBackoff bool   `xml:"DisablePayloadBackoff,attr,omitempty"`
	MaxFailureCountPerURL uint   `xml:"MaxFailureCountPerUrl,attr,omitempty"`
	MetadataSignatureRsa  string `xml:"MetadataSignatureRsa,attr,omitempty"`
	MetadataSize          string `xml:"MetadataSize,attr,omitempty"`
	Deadline              string `xml:"deadline,attr,omitempty"`
	MoreInfo              string `xml:"MoreInfo,attr,omitempty"`
	Prompt                bool   `xml:"Prompt,attr,omitempty"`
}
