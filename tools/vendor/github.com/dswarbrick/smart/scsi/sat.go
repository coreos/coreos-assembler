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

// SCSI / ATA Translation functions.

package scsi

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/dswarbrick/smart/ata"
	"github.com/dswarbrick/smart/drivedb"
	"github.com/dswarbrick/smart/utils"
)

// SATDevice is a simple wrapper around an embedded SCSIDevice type, which handles sending ATA
// commands via SCSI pass-through (SCSI-ATA Translation).
type SATDevice struct {
	SCSIDevice
}

func (d *SATDevice) identify() (ata.IdentifyDeviceData, error) {
	var identBuf ata.IdentifyDeviceData

	respBuf := make([]byte, 512)

	cdb16 := CDB16{SCSI_ATA_PASSTHRU_16}
	cdb16[1] = 0x08                     // ATA protocol (4 << 1, PIO data-in)
	cdb16[2] = 0x0e                     // BYT_BLOK = 1, T_LENGTH = 2, T_DIR = 1
	cdb16[14] = ata.ATA_IDENTIFY_DEVICE // command

	if err := d.sendCDB(cdb16[:], &respBuf); err != nil {
		return identBuf, fmt.Errorf("sendCDB ATA IDENTIFY: %v", err)
	}

	binary.Read(bytes.NewBuffer(respBuf), utils.NativeEndian, &identBuf)

	return identBuf, nil
}

// Read SMART log page (WIP / experimental)
func (d *SATDevice) readSMARTLog(logPage uint8) ([]byte, error) {
	respBuf := make([]byte, 512)

	cdb := CDB16{SCSI_ATA_PASSTHRU_16}
	cdb[1] = 0x08               // ATA protocol (4 << 1, PIO data-in)
	cdb[2] = 0x0e               // BYT_BLOK = 1, T_LENGTH = 2, T_DIR = 1
	cdb[4] = ata.SMART_READ_LOG // feature LSB
	cdb[6] = 0x01               // sector count
	cdb[8] = logPage            // SMART log page number
	cdb[10] = 0x4f              // low lba_mid
	cdb[12] = 0xc2              // low lba_high
	cdb[14] = ata.ATA_SMART     // command

	if err := d.sendCDB(cdb[:], &respBuf); err != nil {
		return respBuf, fmt.Errorf("sendCDB SMART READ LOG: %v", err)
	}

	return respBuf, nil
}

func (d *SATDevice) PrintSMART(db *drivedb.DriveDb, w io.Writer) error {
	// Standard SCSI INQUIRY command
	inqResp, err := d.inquiry()
	if err != nil {
		return fmt.Errorf("SgExecute INQUIRY: %v", err)
	}

	fmt.Fprintln(w, "SCSI INQUIRY:", inqResp)

	identBuf, err := d.identify()
	if err != nil {
		return err
	}

	fmt.Fprintln(w, "\nATA IDENTIFY data follows:")
	fmt.Fprintf(w, "Serial Number: %s\n", identBuf.SerialNumber())
	fmt.Fprintln(w, "LU WWN Device Id:", identBuf.WWN())
	fmt.Fprintf(w, "Firmware Revision: %s\n", identBuf.FirmwareRevision())
	fmt.Fprintf(w, "Model Number: %s\n", identBuf.ModelNumber())
	fmt.Fprintf(w, "Rotation Rate: %d\n", identBuf.RotationRate)
	fmt.Fprintf(w, "SMART support available: %v\n", identBuf.Word87>>14 == 1)
	fmt.Fprintf(w, "SMART support enabled: %v\n", identBuf.Word85&0x1 != 0)
	fmt.Fprintln(w, "ATA Major Version:", identBuf.ATAMajorVersion())
	fmt.Fprintln(w, "ATA Minor Version:", identBuf.ATAMinorVersion())
	fmt.Fprintln(w, "Transport:", identBuf.Transport())

	thisDrive := db.LookupDrive(identBuf.ModelNumber())
	fmt.Fprintf(w, "Drive DB contains %d entries. Using model: %s\n", len(db.Drives), thisDrive.Family)

	// FIXME: Check that device supports SMART before trying to read data page

	/*
	 * SMART READ DATA
	 */
	cdb := CDB16{SCSI_ATA_PASSTHRU_16}
	cdb[1] = 0x08                // ATA protocol (4 << 1, PIO data-in)
	cdb[2] = 0x0e                // BYT_BLOK = 1, T_LENGTH = 2, T_DIR = 1
	cdb[4] = ata.SMART_READ_DATA // feature LSB
	cdb[10] = 0x4f               // low lba_mid
	cdb[12] = 0xc2               // low lba_high
	cdb[14] = ata.ATA_SMART      // command

	respBuf := make([]byte, 512)

	if err := d.sendCDB(cdb[:], &respBuf); err != nil {
		return fmt.Errorf("sendCDB SMART READ DATA: %v", err)
	}

	smart := ata.SmartPage{}
	binary.Read(bytes.NewBuffer(respBuf[:362]), utils.NativeEndian, &smart)
	ata.PrintSMARTPage(smart, thisDrive, w)

	// Read SMART log directory
	logBuf, err := d.readSMARTLog(0x00)
	if err != nil {
		return err
	}

	smartLogDir := ata.SmartLogDirectory{}
	binary.Read(bytes.NewBuffer(logBuf), utils.NativeEndian, &smartLogDir)
	fmt.Fprintf(w, "\nSMART log directory: %+v\n", smartLogDir)

	// Read SMART error log
	logBuf, err = d.readSMARTLog(0x01)
	if err != nil {
		return err
	}

	sumErrLog := ata.SmartSummaryErrorLog{}
	binary.Read(bytes.NewBuffer(logBuf), utils.NativeEndian, &sumErrLog)
	fmt.Fprintf(w, "\nSummary SMART error log: %+v\n", sumErrLog)

	// Read SMART self-test log
	logBuf, err = d.readSMARTLog(0x06)
	if err != nil {
		return err
	}

	selfTestLog := ata.SmartSelfTestLog{}
	binary.Read(bytes.NewBuffer(logBuf), utils.NativeEndian, &selfTestLog)
	fmt.Fprintf(w, "\nSMART self-test log: %+v\n", selfTestLog)

	return nil
}
