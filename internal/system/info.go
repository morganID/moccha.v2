package system

import (
	"encoding/json"
	"fmt"
	"runtime"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/net"
	"github.com/shirou/gopsutil/v3/process"
)

type SystemInfo struct {
	Hostname  string        `json:"hostname"`
	OS        string        `json:"os"`
	Arch      string        `json:"arch"`
	Uptime    uint64        `json:"uptime"`
	BootTime  uint64        `json:"bootTime"`
	CPU       CPUInfo       `json:"cpu"`
	Memory    MemoryInfo    `json:"memory"`
	Disk      []DiskUsage   `json:"disk"`
	Network   []NetworkInfo `json:"network"`
	Processes []ProcessInfo `json:"processes,omitempty"`
	Timestamp time.Time     `json:"timestamp"`
}

type CPUInfo struct {
	Model    string    `json:"model"`
	Cores    int       `json:"cores"`
	Physical int       `json:"physical"`
	Usage    float64   `json:"usage"`
	PerCPU   []float64 `json:"perCpu,omitempty"`
}

type MemoryInfo struct {
	Total        uint64  `json:"total"`
	Available    uint64  `json:"available"`
	Used         uint64  `json:"used"`
	Free         uint64  `json:"free"`
	UsagePercent float64 `json:"usagePercent"`
}

type DiskUsage struct {
	Mount        string  `json:"mount"`
	FsType       string  `json:"fsType"`
	Total        uint64  `json:"total"`
	Used         uint64  `json:"used"`
	Free         uint64  `json:"free"`
	UsagePercent float64 `json:"usagePercent"`
}

type NetworkInfo struct {
	Interface   string `json:"interface"`
	BytesSent   uint64 `json:"bytesSent"`
	BytesRecv   uint64 `json:"bytesRecv"`
	PacketsSent uint64 `json:"packetsSent"`
	PacketsRecv uint64 `json:"packetsRecv"`
	IPAddress   string `json:"ipAddress"`
}

type ProcessInfo struct {
	PID     int32   `json:"pid"`
	Name    string  `json:"name"`
	Status  string  `json:"status"`
	CPU     float64 `json:"cpu"`
	Memory  float64 `json:"memory"`
	User    string  `json:"user"`
	Command string  `json:"command"`
}

type System struct{}

func New() *System {
	return &System{}
}

func (s *System) GetInfo(includeProcesses bool) (*SystemInfo, error) {
	info := &SystemInfo{
		Timestamp: time.Now(),
	}

	hostInfo, err := host.Info()
	if err == nil {
		info.Hostname = hostInfo.Hostname
		info.OS = fmt.Sprintf("%s %s", hostInfo.OS, hostInfo.PlatformVersion)
		info.Arch = runtime.GOARCH
		info.Uptime = hostInfo.Uptime
		info.BootTime = hostInfo.BootTime
	}

	cpuInfo, err := cpu.Info()
	if err == nil && len(cpuInfo) > 0 {
		info.CPU.Model = cpuInfo[0].ModelName
		info.CPU.Cores = int(cpuInfo[0].Cores)
		info.CPU.Physical = 0
	}

	cpuPercent, err := cpu.Percent(0, false)
	if err == nil && len(cpuPercent) > 0 {
		info.CPU.Usage = cpuPercent[0]
	}

	perCPU, err := cpu.Percent(0, true)
	if err == nil {
		info.CPU.PerCPU = perCPU
	}

	memInfo, err := mem.VirtualMemory()
	if err == nil {
		info.Memory.Total = memInfo.Total
		info.Memory.Available = memInfo.Available
		info.Memory.Used = memInfo.Used
		info.Memory.Free = memInfo.Free
		info.Memory.UsagePercent = memInfo.UsedPercent
	}

	diskInfo, err := disk.Partitions(false)
	if err == nil {
		for _, d := range diskInfo {
			usage, err := disk.Usage(d.Mountpoint)
			if err == nil {
				info.Disk = append(info.Disk, DiskUsage{
					Mount:        d.Mountpoint,
					FsType:       d.Fstype,
					Total:        usage.Total,
					Used:         usage.Used,
					Free:         usage.Free,
					UsagePercent: usage.UsedPercent,
				})
			}
		}
	}

	netInterfaces, err := net.Interfaces()
	if err == nil {
		netCounters, _ := net.IOCounters(false)
		for _, iface := range netInterfaces {
			var bytesSent, bytesRecv, packetsSent, packetsRecv uint64
			for _, nc := range netCounters {
				if nc.Name == iface.Name {
					bytesSent = nc.BytesSent
					bytesRecv = nc.BytesRecv
					packetsSent = nc.PacketsSent
					packetsRecv = nc.PacketsRecv
					break
				}
			}
			info.Network = append(info.Network, NetworkInfo{
				Interface:   iface.Name,
				BytesSent:   bytesSent,
				BytesRecv:   bytesRecv,
				PacketsSent: packetsSent,
				PacketsRecv: packetsRecv,
			})
			for _, addr := range iface.Addrs {
				if addr.Addr != "127.0.0.1" && addr.Addr != "::1" {
					if len(info.Network) > 0 {
						info.Network[len(info.Network)-1].IPAddress = addr.Addr
					}
				}
			}
		}
	}

	if includeProcesses {
		procs, err := process.Processes()
		if err == nil {
			for i, p := range procs {
				if i >= 50 {
					break
				}
				name, _ := p.Name()
				status, _ := p.Status()
				cpu, _ := p.CPUPercent()
				mem, _ := p.MemoryPercent()

				info.Processes = append(info.Processes, ProcessInfo{
					PID:    p.Pid,
					Name:   name,
					Status: status[0],
					CPU:    cpu,
					Memory: float64(mem),
				})
			}
		}
	}

	return info, nil
}

func (s *System) GetProcesses() ([]ProcessInfo, error) {
	procs, err := process.Processes()
	if err != nil {
		return nil, err
	}

	result := make([]ProcessInfo, 0, len(procs))
	for _, p := range procs {
		name, _ := p.Name()
		status, _ := p.Status()
		cpu, _ := p.CPUPercent()
		mem, _ := p.MemoryPercent()

		result = append(result, ProcessInfo{
			PID:    p.Pid,
			Name:   name,
			Status: status[0],
			CPU:    cpu,
			Memory: float64(mem),
		})
	}

	return result, nil
}

func (s *System) GetNetwork() ([]NetworkInfo, error) {
	netInterfaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}

	netCounters, _ := net.IOCounters(false)
	result := make([]NetworkInfo, 0)
	for _, iface := range netInterfaces {
		var bytesSent, bytesRecv, packetsSent, packetsRecv uint64
		for _, nc := range netCounters {
			if nc.Name == iface.Name {
				bytesSent = nc.BytesSent
				bytesRecv = nc.BytesRecv
				packetsSent = nc.PacketsSent
				packetsRecv = nc.PacketsRecv
				break
			}
		}
		ni := NetworkInfo{
			Interface:   iface.Name,
			BytesSent:   bytesSent,
			BytesRecv:   bytesRecv,
			PacketsSent: packetsSent,
			PacketsRecv: packetsRecv,
		}
		for _, addr := range iface.Addrs {
			if addr.Addr != "127.0.0.1" && addr.Addr != "::1" {
				ni.IPAddress = addr.Addr
				break
			}
		}
		result = append(result, ni)
	}

	return result, nil
}

func (s *System) GetDisk() ([]DiskUsage, error) {
	diskInfo, err := disk.Partitions(false)
	if err != nil {
		return nil, err
	}

	result := make([]DiskUsage, 0)
	for _, d := range diskInfo {
		usage, err := disk.Usage(d.Mountpoint)
		if err == nil {
			result = append(result, DiskUsage{
				Mount:        d.Mountpoint,
				FsType:       d.Fstype,
				Total:        usage.Total,
				Used:         usage.Used,
				Free:         usage.Free,
				UsagePercent: usage.UsedPercent,
			})
		}
	}

	return result, nil
}

func (s *System) ToJSON(info *SystemInfo) ([]byte, error) {
	return json.Marshal(info)
}
