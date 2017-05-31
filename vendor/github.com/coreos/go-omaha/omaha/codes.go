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

package omaha

import (
	"fmt"
)

type EventType int

const (
	EventTypeUnknown                      EventType = 0
	EventTypeDownloadComplete             EventType = 1
	EventTypeInstallComplete              EventType = 2
	EventTypeUpdateComplete               EventType = 3
	EventTypeUninstall                    EventType = 4
	EventTypeDownloadStarted              EventType = 5
	EventTypeInstallStarted               EventType = 6
	EventTypeNewApplicationInstallStarted EventType = 9
	EventTypeSetupStarted                 EventType = 10
	EventTypeSetupFinished                EventType = 11
	EventTypeUpdateApplicationStarted     EventType = 12
	EventTypeUpdateDownloadStarted        EventType = 13
	EventTypeUpdateDownloadFinished       EventType = 14
	EventTypeUpdateInstallerStarted       EventType = 15
	EventTypeSetupUpdateBegin             EventType = 16
	EventTypeSetupUpdateComplete          EventType = 17
	EventTypeRegisterProductComplete      EventType = 20
	EventTypeOEMInstallFirstCheck         EventType = 30
	EventTypeAppSpecificCommandStarted    EventType = 40
	EventTypeAppSpecificCommandEnded      EventType = 41
	EventTypeSetupFailure                 EventType = 100
	EventTypeComServerFailure             EventType = 102
	EventTypeSetupUpdateFailure           EventType = 103
)

func (e EventType) String() string {
	switch e {
	case EventTypeUnknown:
		return "unknown"
	case EventTypeDownloadComplete:
		return "download complete"
	case EventTypeInstallComplete:
		return "install complete"
	case EventTypeUpdateComplete:
		return "update complete"
	case EventTypeUninstall:
		return "uninstall"
	case EventTypeDownloadStarted:
		return "download started"
	case EventTypeInstallStarted:
		return "install started"
	case EventTypeNewApplicationInstallStarted:
		return "new application install started"
	case EventTypeSetupStarted:
		return "setup started"
	case EventTypeSetupFinished:
		return "setup finished"
	case EventTypeUpdateApplicationStarted:
		return "update application started"
	case EventTypeUpdateDownloadStarted:
		return "update download started"
	case EventTypeUpdateDownloadFinished:
		return "update download finished"
	case EventTypeUpdateInstallerStarted:
		return "update installer started"
	case EventTypeSetupUpdateBegin:
		return "setup update begin"
	case EventTypeSetupUpdateComplete:
		return "setup update complete"
	case EventTypeRegisterProductComplete:
		return "register product complete"
	case EventTypeOEMInstallFirstCheck:
		return "OEM install first check"
	case EventTypeAppSpecificCommandStarted:
		return "app-specific command started"
	case EventTypeAppSpecificCommandEnded:
		return "app-specific command ended"
	case EventTypeSetupFailure:
		return "setup failure"
	case EventTypeComServerFailure:
		return "COM server failure"
	case EventTypeSetupUpdateFailure:
		return "setup update failure "
	default:
		return fmt.Sprintf("event %d", e)
	}
}

type EventResult int

const (
	EventResultError                 EventResult = 0
	EventResultSuccess               EventResult = 1
	EventResultSuccessReboot         EventResult = 2
	EventResultSuccessRestartBrowser EventResult = 3
	EventResultCancelled             EventResult = 4
	EventResultErrorInstallerMSI     EventResult = 5
	EventResultErrorInstallerOther   EventResult = 6
	EventResultNoUpdate              EventResult = 7
	EventResultInstallerSystem       EventResult = 8
	EventResultUpdateDeferred        EventResult = 9
	EventResultHandoffError          EventResult = 10
)

func (e EventResult) String() string {
	switch e {
	case EventResultError:
		return "error"
	case EventResultSuccess:
		return "success"
	case EventResultSuccessReboot:
		return "success reboot"
	case EventResultSuccessRestartBrowser:
		return "success restart browser"
	case EventResultCancelled:
		return "cancelled"
	case EventResultErrorInstallerMSI:
		return "error installer MSI"
	case EventResultErrorInstallerOther:
		return "error installer other"
	case EventResultNoUpdate:
		return "noupdate"
	case EventResultInstallerSystem:
		return "error installer system"
	case EventResultUpdateDeferred:
		return "update deferred"
	case EventResultHandoffError:
		return "handoff error"
	default:
		return fmt.Sprintf("result %d", e)
	}
}

type AppStatus string

const (
	// Standard values
	AppOK         AppStatus = "ok"
	AppRestricted AppStatus = "restricted"
	AppUnknownID  AppStatus = "error-unknownApplication"
	AppInvalidID  AppStatus = "error-invalidAppId"

	// Extra error values
	AppInvalidVersion AppStatus = "error-invalidVersion"
	AppInternalError  AppStatus = "error-internal"
)

// Make AppStatus easy to use as an error
func (a AppStatus) Error() string {
	return "omaha: app status " + string(a)
}

type UpdateStatus string

const (
	NoUpdate                   UpdateStatus = "noupdate"
	UpdateOK                   UpdateStatus = "ok"
	UpdateOSNotSupported       UpdateStatus = "error-osnotsupported"
	UpdateUnsupportedProtocol  UpdateStatus = "error-unsupportedProtocol"
	UpdatePluginRestrictedHost UpdateStatus = "error-pluginRestrictedHost"
	UpdateHashError            UpdateStatus = "error-hash"
	UpdateInternalError        UpdateStatus = "error-internal"
)

// Make UpdateStatus easy to use as an error
func (u UpdateStatus) Error() string {
	return "omaha: update status " + string(u)
}
