// Copyright 2015 CoreOS, Inc.
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

package util

import (
	"bufio"
	"fmt"
	"io"
	"os"

	"github.com/coreos/ioprogress"
	"github.com/coreos/pkg/capnslog"
)

var plog = capnslog.NewPackageLogger("github.com/coreos/mantle", "util")

// LogFrom reads lines from reader r and sends them to logger l.
func LogFrom(l capnslog.LogLevel, r io.Reader) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		plog.Log(l, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		plog.Errorf("Reading %s failed: %v", r, err)
	}
}

// CopyProgress copies data from reader into writter, logging progress through level.
func CopyProgress(level capnslog.LogLevel, prefix string, writer io.Writer, reader io.Reader, total int64) (int64, error) {
	// TODO(marineam): would be nice to support this natively in
	// capnslog so the right output stream and formatter are used.
	if plog.LevelAt(level) {
		// ripped off from rkt, so another reason to add to capnslog
		fmtBytesSize := 18
		barSize := int64(80 - len(prefix) - fmtBytesSize)
		bar := ioprogress.DrawTextFormatBarForW(barSize, os.Stderr)
		fmtfunc := func(progress, total int64) string {
			if total < 0 {
				return fmt.Sprintf(
					"%s: %v of an unknown total size",
					prefix,
					ioprogress.ByteUnitStr(progress),
				)
			}
			return fmt.Sprintf(
				"%s: %s %s",
				prefix,
				bar(progress, total),
				ioprogress.DrawTextFormatBytes(progress, total),
			)
		}

		reader = &ioprogress.Reader{
			Reader:   reader,
			Size:     total,
			DrawFunc: ioprogress.DrawTerminalf(os.Stderr, fmtfunc),
		}
	}

	return io.Copy(writer, reader)
}
