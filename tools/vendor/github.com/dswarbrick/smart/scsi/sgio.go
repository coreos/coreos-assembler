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

// SCSI generic IO functions.

package scsi

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"unsafe"

	"golang.org/x/sys/unix"

	"github.com/dswarbrick/smart/drivedb"
	"github.com/dswarbrick/smart/ioctl"
	"github.com/dswarbrick/smart/utils"
)

const (
	SG_DXFER_NONE        = -1
	SG_DXFER_TO_DEV      = -2
	SG_DXFER_FROM_DEV    = -3
	SG_DXFER_TO_FROM_DEV = -4

	SG_INFO_OK_MASK = 0x1
	SG_INFO_OK      = 0x0

	SG_IO = 0x2285

	// Timeout in milliseconds
	DEFAULT_TIMEOUT = 20000
)

// SCSI generic ioctl header, defined as sg_io_hdr_t in <scsi/sg.h>
type sgIoHdr struct {
	interface_id    int32   // 'S' for SCSI generic (required)
	dxfer_direction int32   // data transfer direction
	cmd_len         uint8   // SCSI command length (<= 16 bytes)
	mx_sb_len       uint8   // max length to write to sbp
	iovec_count     uint16  // 0 implies no scatter gather
	dxfer_len       uint32  // byte count of data transfer
	dxferp          uintptr // points to data transfer memory or scatter gather list
	cmdp            uintptr // points to command to perform
	sbp             uintptr // points to sense_buffer memory
	timeout         uint32  // MAX_UINT -> no timeout (unit: millisec)
	flags           uint32  // 0 -> default, see SG_FLAG...
	pack_id         int32   // unused internally (normally)
	usr_ptr         uintptr // unused internally
	status          uint8   // SCSI status
	masked_status   uint8   // shifted, masked scsi status
	msg_status      uint8   // messaging level data (optional)
	sb_len_wr       uint8   // byte count actually written to sbp
	host_status     uint16  // errors from host adapter
	driver_status   uint16  // errors from software driver
	resid           int32   // dxfer_len - actual_transferred
	duration        uint32  // time taken by cmd (unit: millisec)
	info            uint32  // auxiliary information
}

type sgioError struct {
	scsiStatus   uint8
	hostStatus   uint16
	driverStatus uint16
	senseBuf     [32]byte // FIXME: This is not yet populated by anything
}

func (e sgioError) Error() string {
	return fmt.Sprintf("SCSI status: %#02x, host status: %#02x, driver status: %#02x",
		e.scsiStatus, e.hostStatus, e.driverStatus)
}

// Top-level device interface. All supported device types must implement these methods.
type Device interface {
	Open() error
	Close() error
	PrintSMART(*drivedb.DriveDb, io.Writer) error
}

// TODO: Make a constructor function for this.
type SCSIDevice struct {
	Name string
	fd   int
}

func (d *SCSIDevice) Open() (err error) {
	d.fd, err = unix.Open(d.Name, unix.O_RDWR, 0600)
	return err
}

func (d *SCSIDevice) Close() error {
	return unix.Close(d.fd)
}

func (d *SCSIDevice) execGenericIO(hdr *sgIoHdr) error {
	if err := ioctl.Ioctl(uintptr(d.fd), SG_IO, uintptr(unsafe.Pointer(hdr))); err != nil {
		return err
	}

	// See http://www.t10.org/lists/2status.htm for SCSI status codes
	if hdr.info&SG_INFO_OK_MASK != SG_INFO_OK {
		err := sgioError{
			scsiStatus:   hdr.status,
			hostStatus:   hdr.host_status,
			driverStatus: hdr.driver_status,
		}
		return err
	}

	return nil
}

// inquiry sends a SCSI INQUIRY command to a device and returns an InquiryResponse struct.
// TODO: Add support for Vital Product Data (VPD)
func (d *SCSIDevice) inquiry() (InquiryResponse, error) {
	var resp InquiryResponse

	respBuf := make([]byte, INQ_REPLY_LEN)

	cdb := CDB6{SCSI_INQUIRY}
	binary.BigEndian.PutUint16(cdb[3:], uint16(len(respBuf)))

	if err := d.sendCDB(cdb[:], &respBuf); err != nil {
		return resp, err
	}

	binary.Read(bytes.NewBuffer(respBuf), utils.NativeEndian, &resp)

	return resp, nil
}

// sendCDB sends a SCSI Command Descriptor Block to the device and writes the response into the
// supplied []byte pointer.
// TODO: Return SCSI status code, sense buf etc as part of error
func (d *SCSIDevice) sendCDB(cdb []byte, respBuf *[]byte) error {
	senseBuf := make([]byte, 32)

	// Populate required fields of "sg_io_hdr_t" struct
	hdr := sgIoHdr{
		interface_id:    'S',
		dxfer_direction: SG_DXFER_FROM_DEV,
		timeout:         DEFAULT_TIMEOUT,
		cmd_len:         uint8(len(cdb)),
		mx_sb_len:       uint8(len(senseBuf)),
		dxfer_len:       uint32(len(*respBuf)),
		dxferp:          uintptr(unsafe.Pointer(&(*respBuf)[0])),
		cmdp:            uintptr(unsafe.Pointer(&cdb[0])),
		sbp:             uintptr(unsafe.Pointer(&senseBuf[0])),
	}

	return d.execGenericIO(&hdr)
}

// modeSense sends a SCSI MODE SENSE(6) command to a device.
func (d *SCSIDevice) modeSense(pageNum, subPageNum, pageControl uint8) ([]byte, error) {
	respBuf := make([]byte, 64)

	cdb := CDB6{SCSI_MODE_SENSE_6}
	cdb[2] = (pageControl << 6) | (pageNum & 0x3f)
	cdb[3] = subPageNum
	cdb[4] = uint8(len(respBuf))

	if err := d.sendCDB(cdb[:], &respBuf); err != nil {
		return respBuf, err
	}

	return respBuf, nil
}

// readCapacity sends a SCSI READ CAPACITY(10) command to a device and returns the capacity in bytes.
func (d *SCSIDevice) readCapacity() (uint64, error) {
	respBuf := make([]byte, 8)
	cdb := CDB10{SCSI_READ_CAPACITY_10}

	if err := d.sendCDB(cdb[:], &respBuf); err != nil {
		return 0, err
	}

	lastLBA := binary.BigEndian.Uint32(respBuf[0:]) // max. addressable LBA
	LBsize := binary.BigEndian.Uint32(respBuf[4:])  // logical block (i.e., sector) size
	capacity := (uint64(lastLBA) + 1) * uint64(LBsize)

	return capacity, nil
}

// Regular SCSI (including SAS, but excluding SATA) SMART functions not yet fully implemented.
func (d *SCSIDevice) PrintSMART(db *drivedb.DriveDb, w io.Writer) error {
	capacity, _ := d.readCapacity()
	fmt.Fprintf(w, "Capacity: %d bytes (%s)\n", capacity, utils.FormatBytes(capacity))

	// WIP
	resp, _ := d.modeSense(RIGID_DISK_DRIVE_GEOMETRY_PAGE, 0, MPAGE_CONTROL_DEFAULT)
	fmt.Fprintf(w, "MODE SENSE buf: % x\n", resp)

	// TODO: Handle this elegantly for MODE SENSE(10) also
	respLen := resp[0] + 1
	bdLen := resp[3]
	offset := bdLen + 4
	fmt.Fprintf(w, "respLen: %d, bdLen: %d, offset: %d\n",
		respLen, bdLen, offset)

	fmt.Fprintf(w, "RPM: %d\n", binary.BigEndian.Uint16(resp[offset+20:]))

	return nil
}

func OpenSCSIAutodetect(name string) (Device, error) {
	dev := SCSIDevice{Name: name}

	if err := dev.Open(); err != nil {
		return nil, err
	}

	inquiry, err := dev.inquiry()
	if err != nil {
		return nil, err
	}

	// Check if device is an ATA device.
	// TODO: Handle USB-SATA bridges by probing the device with an ATA IDENTIFY command. Watch out
	// for ATAPI devices.
	if inquiry.VendorIdent == [8]byte{0x41, 0x54, 0x41, 0x20, 0x20, 0x20, 0x20, 0x20} {
		return &SATDevice{dev}, nil
	}

	return &dev, nil
}
