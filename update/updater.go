// Copyright 2016 CoreOS, Inc.
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

package update

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"os"

	"github.com/coreos/pkg/capnslog"
	"github.com/golang/protobuf/proto"

	"github.com/coreos/mantle/update/metadata"
)

var (
	plog = capnslog.NewPackageLogger("github.com/coreos/mantle", "update")
)

type Updater struct {
	SrcPartition string
	DstPartition string

	payload *Payload
}

func (u *Updater) OpenPayload(file string) error {
	plog.Infof("Loading payload from %s", file)

	f, err := os.Open(file)
	if err != nil {
		return err
	}

	return u.UsePayload(f)
}

func (u *Updater) UsePayload(r io.Reader) (err error) {
	u.payload, err = NewPayloadFrom(r)
	return err
}

func (u *Updater) Update() error {
	for _, proc := range u.payload.Procedures() {
		var err error
		switch proc.GetType() {
		case installProcedure_partition:
			err = u.UpdatePartition(proc)
		case metadata.InstallProcedure_KERNEL:
			err = u.UpdateKernel(proc)
		default:
			err = fmt.Errorf("unknown procedure type %s", proc.GetType())
		}
		if err != nil {
			return err
		}
	}
	return u.payload.VerifySignature()
}

func (u *Updater) UpdatePartition(proc *metadata.InstallProcedure) error {
	return u.updateCommon(proc, "partition", u.SrcPartition, u.DstPartition)
}

func (u *Updater) UpdateKernel(proc *metadata.InstallProcedure) error {
	return fmt.Errorf("KERNEL")
}

func (u *Updater) updateCommon(proc *metadata.InstallProcedure, procName, srcPath, dstPath string) (err error) {
	var srcFile, dstFile *os.File
	if proc.OldInfo.GetSize() != 0 && len(proc.OldInfo.Hash) != 0 {
		if srcFile, err = os.Open(srcPath); err != nil {
			return err
		}
		defer srcFile.Close()

		if err = VerifyInfo(srcFile, proc.OldInfo); err != nil {
			return err
		}
	}

	dstFile, err = os.OpenFile(u.DstPartition, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0666)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	progress := 0
	for _, op := range u.payload.Operations(proc) {
		progress++
		plog.Infof("%s operation %d", procName, progress)
		if err := op.Apply(dstFile, srcFile); err != nil {
			return fmt.Errorf("%s operation %d: %v\n%s",
				procName, progress, err,
				proto.MarshalTextString(op.Operation))
		}
	}

	return VerifyInfo(dstFile, proc.NewInfo)
}

func VerifyInfo(file *os.File, info *metadata.InstallInfo) error {
	if _, err := file.Seek(0, os.SEEK_SET); err != nil {
		return err
	}

	sha := sha256.New()
	if n, err := io.CopyN(sha, file, int64(info.GetSize())); err == io.EOF {
		return fmt.Errorf("%s: expected %d bytes but read %d bytes",
			file.Name(), info.GetSize(), n)
	} else if err != nil {
		return err
	}

	sum := sha.Sum(nil)
	if !bytes.Equal(info.Hash, sum) {
		return fmt.Errorf("%s: expected hash %x but got %x",
			file.Name(), info.Hash, sum)
	}

	return nil
}
