// Copyright 2020 Red Hat, Inc.
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

package system

import (
	"bufio"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/coreos/coreos-assembler/mantle/system/exec"
)

// GetProcessors returns a count for number of cores we should use;
// this value is appropriate to pass to e.g. make -J as well as
// qemu -smp for example.
func GetProcessors() (uint, error) {
	// Get available CPU count, including sched_getaffinity()
	nprocBuf, err := exec.Command("nproc").CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("executing nproc: %w", err)
	}
	nproc, err := strconv.ParseUint(strings.TrimSpace(string(nprocBuf)), 10, 32)
	if err != nil {
		return 0, fmt.Errorf("parsing nproc output: %w", err)
	}

	// Compute the available CPU quota
	quota, err := getCpuQuota()
	if err != nil {
		return 0, err
	}

	if quota < uint(nproc) {
		return quota, nil
	}
	return uint(nproc), nil
}

// getCpuQuota() returns the cpu quota associated with the current
// cgroup (v2) or math.MaxUint if not in a cgroup.
func getCpuQuota() (uint, error) {
	// cgroups v2
	buf, err := os.ReadFile("/sys/fs/cgroup/cpu.max")
	if os.IsNotExist(err) {
		return math.MaxUint, nil
	} else if err != nil {
		return 0, fmt.Errorf("reading cpu.max: %w", err)
	}
	vals := strings.SplitN(strings.TrimSpace(string(buf)), " ", 2)
	if len(vals) != 2 {
		return 0, fmt.Errorf("invalid cpu.max value")
	}
	if vals[0] == "max" {
		return math.MaxUint, nil
	}
	quota, err := strconv.ParseUint(vals[0], 10, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid CPU quota: %w", err)
	}
	period, err := strconv.ParseUint(vals[1], 10, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid CPU period: %w", err)
	}
	if quota > 0 && period > 0 {
		return uint((quota + period - 1) / period), nil
	}
	return math.MaxUint, nil
}

// GetTotalMemoryMiB returns the total system memory in MiB, taking into
// account cgroup v2 memory limits. This is suitable for determining
// the total memory budget at startup.
func GetTotalMemoryMiB() (uint, error) {
	sysMem, err := readMeminfoFieldMiB("MemTotal")
	if err != nil {
		return 0, err
	}

	cgroupMem, err := getCgroupMemoryLimitMiB()
	if err != nil {
		return 0, err
	}

	if cgroupMem < sysMem {
		return cgroupMem, nil
	}
	return sysMem, nil
}

// GetCurrentMemAvailableMiB returns the memory currently available in
// MiB. This reads MemAvailable from /proc/meminfo and also checks
// cgroup memory usage against limits. It is designed to be called
// repeatedly at runtime to check whether enough memory is free to
// start a new QEMU instance.
func GetCurrentMemAvailableMiB() (uint, error) {
	sysAvail, err := readMeminfoFieldMiB("MemAvailable")
	if err != nil {
		return 0, err
	}

	cgroupAvail, err := getCgroupMemoryAvailableMiB()
	if err != nil {
		return 0, err
	}

	if cgroupAvail < sysAvail {
		return cgroupAvail, nil
	}
	return sysAvail, nil
}

// readMeminfoFieldMiB reads a specific field from /proc/meminfo and
// returns the value in MiB (the kernel reports values in kB).
func readMeminfoFieldMiB(field string) (uint, error) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, fmt.Errorf("reading /proc/meminfo: %w", err)
	}
	defer f.Close()

	prefix := field + ":"
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, prefix) {
			fields := strings.Fields(line)
			if len(fields) < 2 {
				return 0, fmt.Errorf("unexpected %s format: %s", field, line)
			}
			kB, err := strconv.ParseUint(fields[1], 10, 64)
			if err != nil {
				return 0, fmt.Errorf("parsing %s: %w", field, err)
			}
			return uint(kB / 1024), nil
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, fmt.Errorf("scanning /proc/meminfo: %w", err)
	}
	return 0, fmt.Errorf("%s not found in /proc/meminfo", field)
}

// getCgroupMemoryLimitMiB returns the cgroup v2 memory limit in MiB,
// or math.MaxUint if no limit is set.
func getCgroupMemoryLimitMiB() (uint, error) {
	buf, err := os.ReadFile("/sys/fs/cgroup/memory.max")
	if os.IsNotExist(err) {
		return math.MaxUint, nil
	} else if err != nil {
		return 0, fmt.Errorf("reading memory.max: %w", err)
	}
	val := strings.TrimSpace(string(buf))
	if val == "max" {
		return math.MaxUint, nil
	}
	limit, err := strconv.ParseUint(val, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid memory.max value: %w", err)
	}
	return uint(limit / (1024 * 1024)), nil
}

// getCgroupMemoryAvailableMiB returns the available memory within the
// cgroup v2 in MiB, or math.MaxUint if no limit is set. It computes
// available memory as: limit - (current - inactive_file) where inactive_file
// is not actively used file caches that can be evicted if needed.
// (current - inactive_file) is similar to the "workingSet" calculation over in [1].
// More context on this also in [2]. This is similar to how /proc/meminfo computes
// MemAvailable by considering reclaimable caches.
//
// [1] https://github.com/kubernetes/kubernetes/blob/ac10370ad2aebde82c2d268dd80d08df0ffc2532/test/e2e/node/node_problem_detector.go#L290-L344
// [2] https://github.com/kata-containers/kata-containers/issues/10280
func getCgroupMemoryAvailableMiB() (uint, error) {
	maxBuf, err := os.ReadFile("/sys/fs/cgroup/memory.max")
	if os.IsNotExist(err) {
		return math.MaxUint, nil
	} else if err != nil {
		return 0, fmt.Errorf("reading memory.max: %w", err)
	}
	val := strings.TrimSpace(string(maxBuf))
	if val == "max" {
		return math.MaxUint, nil
	}
	limit, err := strconv.ParseUint(val, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid memory.max value: %w", err)
	}
	curBuf, err := os.ReadFile("/sys/fs/cgroup/memory.current")
	if err != nil {
		return 0, fmt.Errorf("reading memory.current: %w", err)
	}
	current, err := strconv.ParseUint(strings.TrimSpace(string(curBuf)), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid memory.current value: %w", err)
	}

	// Read inactive_file size from memory.stat to exclude reclaimable
	// file-backed memory from the usage calculation.
	inactiveFile, err := getCgroupMemoryStatField("inactive_file")
	if err != nil {
		return 0, err
	}

	// Subtract the inactive_file size from the memory.current. This
	// cache should always be less than the memory.current but add
	// a check and do nothing just in case.
	usage := current
	if inactiveFile < usage {
		usage -= inactiveFile
	}

	// This also shouldn't happen, but in case the usage is larger
	// than the limit let's just return that there's 0 available memory.
	if usage >= limit {
		return 0, nil
	}
	return uint((limit - usage) / (1024 * 1024)), nil
}

// getCgroupMemoryStatField reads a specific field from
// /sys/fs/cgroup/memory.stat and returns its value in bytes.
// The file contains key-value pairs like "file 123456789".
// Returns 0 if the file does not exist or the field is not found.
func getCgroupMemoryStatField(field string) (uint64, error) {
	f, err := os.Open("/sys/fs/cgroup/memory.stat")
	if os.IsNotExist(err) {
		return 0, nil
	} else if err != nil {
		return 0, fmt.Errorf("reading memory.stat: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		parts := strings.Fields(scanner.Text())
		if len(parts) == 2 && parts[0] == field {
			val, err := strconv.ParseUint(parts[1], 10, 64)
			if err != nil {
				return 0, fmt.Errorf("parsing memory.stat field %s: %w", field, err)
			}
			return val, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, fmt.Errorf("scanning memory.stat: %w", err)
	}
	// Field not found; return 0 so callers degrade gracefully.
	return 0, nil
}
