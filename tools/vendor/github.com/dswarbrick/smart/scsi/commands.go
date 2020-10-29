// Copyright 2017-18 Daniel Swarbrick. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// SCSI command definitions.

package scsi

import (
	"fmt"
)

const (
	// SCSI commands used by this package
	SCSI_INQUIRY          = 0x12
	SCSI_MODE_SENSE_6     = 0x1a
	SCSI_READ_CAPACITY_10 = 0x25
	SCSI_ATA_PASSTHRU_16  = 0x85

	// Minimum length of standard INQUIRY response
	INQ_REPLY_LEN = 36

	// SCSI-3 mode pages
	RIGID_DISK_DRIVE_GEOMETRY_PAGE = 0x04

	// Mode page control field
	MPAGE_CONTROL_DEFAULT = 2
)

// SCSI CDB types
type CDB6 [6]byte
type CDB10 [10]byte
type CDB16 [16]byte

// SCSI INQUIRY response
type InquiryResponse struct {
	Peripheral   byte // peripheral qualifier, device type
	_            byte
	Version      byte
	_            [5]byte
	VendorIdent  [8]byte
	ProductIdent [16]byte
	ProductRev   [4]byte
}

func (inq InquiryResponse) String() string {
	return fmt.Sprintf("%.8s  %.16s  %.4s", inq.VendorIdent, inq.ProductIdent, inq.ProductRev)
}
