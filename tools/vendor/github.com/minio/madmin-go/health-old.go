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
	"encoding/json"
	"math/big"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	diskhw "github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/process"
)

// HealthInfoV0 - MinIO cluster's health Info
type HealthInfoV0 struct {
	TimeStamp time.Time         `json:"timestamp,omitempty"`
	Error     string            `json:"error,omitempty"`
	Perf      PerfInfoV0        `json:"perf,omitempty"`
	Minio     MinioHealthInfoV0 `json:"minio,omitempty"`
	Sys       SysHealthInfo     `json:"sys,omitempty"`
}

func (info HealthInfoV0) String() string {
	data, err := json.Marshal(info)
	if err != nil {
		panic(err) // This never happens.
	}
	return string(data)
}

// JSON returns this structure as JSON formatted string.
func (info HealthInfoV0) JSON() string {
	data, err := json.MarshalIndent(info, " ", "    ")
	if err != nil {
		panic(err) // This never happens.
	}
	return string(data)
}

// SysHealthInfo - Includes hardware and system information of the MinIO cluster
type SysHealthInfo struct {
	CPUInfo    []ServerCPUInfo    `json:"cpus,omitempty"`
	DiskHwInfo []ServerDiskHwInfo `json:"drives,omitempty"`
	OsInfo     []ServerOsInfo     `json:"osinfos,omitempty"`
	MemInfo    []ServerMemInfo    `json:"meminfos,omitempty"`
	ProcInfo   []ServerProcInfo   `json:"procinfos,omitempty"`
	Error      string             `json:"error,omitempty"`
}

// ServerProcInfo - Includes host process lvl information
type ServerProcInfo struct {
	Addr      string       `json:"addr"`
	Processes []SysProcess `json:"processes,omitempty"`
	Error     string       `json:"error,omitempty"`
}

// SysProcess - Includes process lvl information about a single process
type SysProcess struct {
	Pid             int32                       `json:"pid"`
	Background      bool                        `json:"background,omitempty"`
	CPUPercent      float64                     `json:"cpupercent,omitempty"`
	Children        []int32                     `json:"children,omitempty"`
	CmdLine         string                      `json:"cmd,omitempty"`
	ConnectionCount int                         `json:"connection_count,omitempty"`
	CreateTime      int64                       `json:"createtime,omitempty"`
	Cwd             string                      `json:"cwd,omitempty"`
	Exe             string                      `json:"exe,omitempty"`
	Gids            []int32                     `json:"gids,omitempty"`
	IOCounters      *process.IOCountersStat     `json:"iocounters,omitempty"`
	IsRunning       bool                        `json:"isrunning,omitempty"`
	MemInfo         *process.MemoryInfoStat     `json:"meminfo,omitempty"`
	MemMaps         *[]process.MemoryMapsStat   `json:"memmaps,omitempty"`
	MemPercent      float32                     `json:"mempercent,omitempty"`
	Name            string                      `json:"name,omitempty"`
	Nice            int32                       `json:"nice,omitempty"`
	NumCtxSwitches  *process.NumCtxSwitchesStat `json:"numctxswitches,omitempty"`
	NumFds          int32                       `json:"numfds,omitempty"`
	NumThreads      int32                       `json:"numthreads,omitempty"`
	PageFaults      *process.PageFaultsStat     `json:"pagefaults,omitempty"`
	Parent          int32                       `json:"parent,omitempty"`
	Ppid            int32                       `json:"ppid,omitempty"`
	Status          string                      `json:"status,omitempty"`
	Tgid            int32                       `json:"tgid,omitempty"`
	Times           *cpu.TimesStat              `json:"cputimes,omitempty"`
	Uids            []int32                     `json:"uids,omitempty"`
	Username        string                      `json:"username,omitempty"`
}

// GetOwner - returns owner of the process
func (sp SysProcess) GetOwner() string {
	return sp.Username
}

// ServerMemInfo - Includes host virtual and swap mem information
type ServerMemInfo struct {
	Addr       string                 `json:"addr"`
	SwapMem    *mem.SwapMemoryStat    `json:"swap,omitempty"`
	VirtualMem *mem.VirtualMemoryStat `json:"virtualmem,omitempty"`
	Error      string                 `json:"error,omitempty"`
}

// ServerOsInfo - Includes host os information
type ServerOsInfo struct {
	Addr    string                 `json:"addr"`
	Info    *host.InfoStat         `json:"info,omitempty"`
	Sensors []host.TemperatureStat `json:"sensors,omitempty"`
	Users   []host.UserStat        `json:"users,omitempty"`
	Error   string                 `json:"error,omitempty"`
}

// ServerCPUInfo - Includes cpu and timer stats of each node of the MinIO cluster
type ServerCPUInfo struct {
	Addr     string          `json:"addr"`
	CPUStat  []cpu.InfoStat  `json:"cpu,omitempty"`
	TimeStat []cpu.TimesStat `json:"time,omitempty"`
	Error    string          `json:"error,omitempty"`
}

// MinioHealthInfoV0 - Includes MinIO confifuration information
type MinioHealthInfoV0 struct {
	Info   InfoMessage `json:"info,omitempty"`
	Config interface{} `json:"config,omitempty"`
	Error  string      `json:"error,omitempty"`
}

// ServerDiskHwInfo - Includes usage counters, disk counters and partitions
type ServerDiskHwInfo struct {
	Addr       string                           `json:"addr"`
	Usage      []*diskhw.UsageStat              `json:"usages,omitempty"`
	Partitions []PartitionStat                  `json:"partitions,omitempty"`
	Counters   map[string]diskhw.IOCountersStat `json:"counters,omitempty"`
	Error      string                           `json:"error,omitempty"`
}

// GetTotalCapacity gets the total capacity a server holds.
func (s *ServerDiskHwInfo) GetTotalCapacity() (capacity uint64) {
	for _, u := range s.Usage {
		capacity += u.Total
	}
	return
}

// GetTotalFreeCapacity gets the total capacity that is free.
func (s *ServerDiskHwInfo) GetTotalFreeCapacity() (capacity uint64) {
	for _, u := range s.Usage {
		capacity += u.Free
	}
	return
}

// GetTotalUsedCapacity gets the total capacity used.
func (s *ServerDiskHwInfo) GetTotalUsedCapacity() (capacity uint64) {
	for _, u := range s.Usage {
		capacity += u.Used
	}
	return
}

// SmartInfo contains S.M.A.R.T data about the drive
type SmartInfo struct {
	Device string         `json:"device"`
	Scsi   *SmartScsiInfo `json:"scsi,omitempty"`
	Nvme   *SmartNvmeInfo `json:"nvme,omitempty"`
	Ata    *SmartAtaInfo  `json:"ata,omitempty"`
}

// SmartNvmeInfo contains NVMe drive info
type SmartNvmeInfo struct {
	SerialNum       string `json:"serialNum,omitempty"`
	VendorID        string `json:"vendorId,omitempty"`
	FirmwareVersion string `json:"firmwareVersion,omitempty"`
	ModelNum        string `json:"modelNum,omitempty"`
	SpareAvailable  string `json:"spareAvailable,omitempty"`
	SpareThreshold  string `json:"spareThreshold,omitempty"`
	Temperature     string `json:"temperature,omitempty"`
	CriticalWarning string `json:"criticalWarning,omitempty"`

	MaxDataTransferPages        int      `json:"maxDataTransferPages,omitempty"`
	ControllerBusyTime          *big.Int `json:"controllerBusyTime,omitempty"`
	PowerOnHours                *big.Int `json:"powerOnHours,omitempty"`
	PowerCycles                 *big.Int `json:"powerCycles,omitempty"`
	UnsafeShutdowns             *big.Int `json:"unsafeShutdowns,omitempty"`
	MediaAndDataIntegrityErrors *big.Int `json:"mediaAndDataIntgerityErrors,omitempty"`
	DataUnitsReadBytes          *big.Int `json:"dataUnitsReadBytes,omitempty"`
	DataUnitsWrittenBytes       *big.Int `json:"dataUnitsWrittenBytes,omitempty"`
	HostReadCommands            *big.Int `json:"hostReadCommands,omitempty"`
	HostWriteCommands           *big.Int `json:"hostWriteCommands,omitempty"`
}

// SmartScsiInfo contains SCSI drive Info
type SmartScsiInfo struct {
	CapacityBytes int64  `json:"scsiCapacityBytes,omitempty"`
	ModeSenseBuf  string `json:"scsiModeSenseBuf,omitempty"`
	RespLen       int64  `json:"scsirespLen,omitempty"`
	BdLen         int64  `json:"scsiBdLen,omitempty"`
	Offset        int64  `json:"scsiOffset,omitempty"`
	RPM           int64  `json:"sciRpm,omitempty"`
}

// SmartAtaInfo contains ATA drive info
type SmartAtaInfo struct {
	LUWWNDeviceID         string `json:"scsiLuWWNDeviceID,omitempty"`
	SerialNum             string `json:"serialNum,omitempty"`
	ModelNum              string `json:"modelNum,omitempty"`
	FirmwareRevision      string `json:"firmwareRevision,omitempty"`
	RotationRate          string `json:"RotationRate,omitempty"`
	ATAMajorVersion       string `json:"MajorVersion,omitempty"`
	ATAMinorVersion       string `json:"MinorVersion,omitempty"`
	SmartSupportAvailable bool   `json:"smartSupportAvailable,omitempty"`
	SmartSupportEnabled   bool   `json:"smartSupportEnabled,omitempty"`
	ErrorLog              string `json:"smartErrorLog,omitempty"`
	Transport             string `json:"transport,omitempty"`
}

// PartitionStat - includes data from both shirou/psutil.diskHw.PartitionStat as well as SMART data
type PartitionStat struct {
	Device     string    `json:"device"`
	Mountpoint string    `json:"mountpoint,omitempty"`
	Fstype     string    `json:"fstype,omitempty"`
	Opts       string    `json:"opts,omitempty"`
	SmartInfo  SmartInfo `json:"smartInfo,omitempty"`
}

// PerfInfoV0 - Includes Drive and Net perf info for the entire MinIO cluster
type PerfInfoV0 struct {
	DriveInfo   []ServerDrivesInfo    `json:"drives,omitempty"`
	Net         []ServerNetHealthInfo `json:"net,omitempty"`
	NetParallel ServerNetHealthInfo   `json:"net_parallel,omitempty"`
	Error       string                `json:"error,omitempty"`
}

// ServerDrivesInfo - Drive info about all drives in a single MinIO node
type ServerDrivesInfo struct {
	Addr     string            `json:"addr"`
	Serial   []DrivePerfInfoV0 `json:"serial,omitempty"`   // Drive perf info collected one drive at a time
	Parallel []DrivePerfInfoV0 `json:"parallel,omitempty"` // Drive perf info collected in parallel
	Error    string            `json:"error,omitempty"`
}

// DiskLatency holds latency information for write operations to the drive
type DiskLatency struct {
	Avg          float64 `json:"avg_secs,omitempty"`
	Percentile50 float64 `json:"percentile50_secs,omitempty"`
	Percentile90 float64 `json:"percentile90_secs,omitempty"`
	Percentile99 float64 `json:"percentile99_secs,omitempty"`
	Min          float64 `json:"min_secs,omitempty"`
	Max          float64 `json:"max_secs,omitempty"`
}

// DiskThroughput holds throughput information for write operations to the drive
type DiskThroughput struct {
	Avg          float64 `json:"avg_bytes_per_sec,omitempty"`
	Percentile50 float64 `json:"percentile50_bytes_per_sec,omitempty"`
	Percentile90 float64 `json:"percentile90_bytes_per_sec,omitempty"`
	Percentile99 float64 `json:"percentile99_bytes_per_sec,omitempty"`
	Min          float64 `json:"min_bytes_per_sec,omitempty"`
	Max          float64 `json:"max_bytes_per_sec,omitempty"`
}

// DrivePerfInfoV0 - Stats about a single drive in a MinIO node
type DrivePerfInfoV0 struct {
	Path       string         `json:"endpoint"`
	Latency    DiskLatency    `json:"latency,omitempty"`
	Throughput DiskThroughput `json:"throughput,omitempty"`
	Error      string         `json:"error,omitempty"`
}

// ServerNetHealthInfo - Network health info about a single MinIO node
type ServerNetHealthInfo struct {
	Addr  string          `json:"addr"`
	Net   []NetPerfInfoV0 `json:"net,omitempty"`
	Error string          `json:"error,omitempty"`
}

// NetLatency holds latency information for read/write operations to the drive
type NetLatency struct {
	Avg          float64 `json:"avg_secs,omitempty"`
	Percentile50 float64 `json:"percentile50_secs,omitempty"`
	Percentile90 float64 `json:"percentile90_secs,omitempty"`
	Percentile99 float64 `json:"percentile99_secs,omitempty"`
	Min          float64 `json:"min_secs,omitempty"`
	Max          float64 `json:"max_secs,omitempty"`
}

// NetThroughput holds throughput information for read/write operations to the drive
type NetThroughput struct {
	Avg          float64 `json:"avg_bytes_per_sec,omitempty"`
	Percentile50 float64 `json:"percentile50_bytes_per_sec,omitempty"`
	Percentile90 float64 `json:"percentile90_bytes_per_sec,omitempty"`
	Percentile99 float64 `json:"percentile99_bytes_per_sec,omitempty"`
	Min          float64 `json:"min_bytes_per_sec,omitempty"`
	Max          float64 `json:"max_bytes_per_sec,omitempty"`
}

// NetPerfInfoV0 - one-to-one network connectivity Stats between 2 MinIO nodes
type NetPerfInfoV0 struct {
	Addr       string        `json:"remote"`
	Latency    NetLatency    `json:"latency,omitempty"`
	Throughput NetThroughput `json:"throughput,omitempty"`
	Error      string        `json:"error,omitempty"`
}
