//go:build windows

package main

import (
	"encoding/json"
	"os/exec"
	"strings"
	"syscall"
)

// The app queries WMI itself via a hidden PowerShell process. The user never sees or
// types a command; this is entirely internal.
const psScript = `
$ErrorActionPreference='SilentlyContinue'
$cs=Get-CimInstance Win32_ComputerSystem
$os=Get-CimInstance Win32_OperatingSystem
$cpu=Get-CimInstance Win32_Processor | Select-Object -First 1
$vc=Get-CimInstance Win32_VideoController
$bb=Get-CimInstance Win32_BaseBoard
$mem=Get-CimInstance Win32_PhysicalMemory
$arr=Get-CimInstance Win32_PhysicalMemoryArray | Select-Object -First 1
$bios=Get-CimInstance Win32_BIOS | Select-Object -First 1
$ld=Get-CimInstance Win32_LogicalDisk -Filter "DriveType=3"
$dd=Get-CimInstance Win32_DiskDrive
$prim=$vc | Where-Object {$_.CurrentHorizontalResolution -gt 0} | Select-Object -First 1
$dgpu=$vc | Where-Object {$_.Name -match 'NVIDIA|GeForce|Radeon|Arc'} | Select-Object -First 1
[pscustomobject]@{
 manufacturer=$cs.Manufacturer; model=$cs.Model; ram_total=[int64]$cs.TotalPhysicalMemory
 os_caption=$os.Caption; os_version=$os.Version
 cpu=$cpu.Name; cores=$cpu.NumberOfCores; threads=$cpu.NumberOfLogicalProcessors
 gpus=@($vc | ForEach-Object { $_.Name })
 gpu_driver=$dgpu.DriverVersion
 res_w=[int]$prim.CurrentHorizontalResolution; res_h=[int]$prim.CurrentVerticalResolution; refresh=[int]$prim.CurrentRefreshRate
 board_mfr=$bb.Manufacturer; board=$bb.Product; bios=$bios.SMBIOSBIOSVersion
 slots=$arr.MemoryDevices
 modules=@($mem | ForEach-Object { @{ cap=[int64]$_.Capacity; speed=[int]$_.Speed; slot=$_.DeviceLocator; smtype=[int]$_.SMBIOSMemoryType } })
 disks=@($dd | ForEach-Object { @{ model=$_.Model; size=[int64]$_.Size } })
 free_bytes=([int64](($ld | Measure-Object -Property FreeSpace -Sum).Sum))
} | ConvertTo-Json -Depth 5 -Compress
`

type winData struct {
	Manufacturer string   `json:"manufacturer"`
	Model        string   `json:"model"`
	RAMTotal     int64    `json:"ram_total"`
	OSCaption    string   `json:"os_caption"`
	OSVersion    string   `json:"os_version"`
	CPU          string   `json:"cpu"`
	Cores        int      `json:"cores"`
	Threads      int      `json:"threads"`
	GPUs         []string `json:"gpus"`
	GPUDriver    string   `json:"gpu_driver"`
	ResW         int      `json:"res_w"`
	ResH         int      `json:"res_h"`
	Refresh      int      `json:"refresh"`
	BoardMfr     string   `json:"board_mfr"`
	Board        string   `json:"board"`
	BIOS         string   `json:"bios"`
	Slots        int      `json:"slots"`
	FreeBytes    int64    `json:"free_bytes"`
	Modules      []struct {
		Cap    int64  `json:"cap"`
		Speed  int    `json:"speed"`
		Slot   string `json:"slot"`
		SMType int    `json:"smtype"`
	} `json:"modules"`
	Disks []struct {
		Model string `json:"model"`
		Size  int64  `json:"size"`
	} `json:"disks"`
}

func collect() Spec {
	s := Spec{}
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", psScript)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()
	if err != nil {
		s.Notes = append(s.Notes, "Could not read hardware details on this PC.")
		return s
	}
	var d winData
	if json.Unmarshal(out, &d) != nil {
		return s
	}

	s.Manufacturer = strings.TrimSpace(d.Manufacturer)
	s.SystemModel = strings.TrimSpace(d.Model)
	s.OSVersion = strings.TrimSpace(d.OSCaption)
	s.CPU = strings.TrimSpace(d.CPU)
	s.CPUCores = d.Cores
	s.CPUThreads = d.Threads
	for _, g := range d.GPUs {
		if g = strings.TrimSpace(g); g != "" {
			s.GPU = append(s.GPU, g)
		}
	}
	s.GPUDriver = strings.TrimSpace(d.GPUDriver)
	s.RAMTotalMB = int(d.RAMTotal / (1024 * 1024))
	s.Motherboard = strings.TrimSpace(d.BoardMfr + " " + d.Board)
	s.BIOS = strings.TrimSpace(d.BIOS)
	s.RAMSlotsTotal = d.Slots
	for _, m := range d.Modules {
		if m.Cap <= 0 {
			continue
		}
		s.RAMModules = append(s.RAMModules, RAMModule{
			CapacityMB: int(m.Cap / (1024 * 1024)),
			SpeedMHz:   m.Speed,
			Slot:       strings.TrimSpace(m.Slot),
			Type:       smbiosType(m.SMType),
		})
	}
	if d.ResW > 0 {
		s.Displays = append(s.Displays, Display{Width: d.ResW, Height: d.ResH, RefreshHz: d.Refresh})
	}
	freeGB := int(d.FreeBytes / (1024 * 1024 * 1024))
	for i, dk := range d.Disks {
		disk := Disk{Model: strings.TrimSpace(dk.Model), Kind: diskKind(dk.Model), SizeGB: int(dk.Size / (1024 * 1024 * 1024))}
		if i == 0 {
			disk.FreeGB = freeGB // total free across fixed drives, attributed to the primary
		}
		s.Storage = append(s.Storage, disk)
	}
	return s
}

func diskKind(model string) string {
	m := strings.ToLower(model)
	switch {
	case strings.Contains(m, "nvme"):
		return "NVMe"
	case strings.Contains(m, "ssd"):
		return "SSD"
	default:
		return ""
	}
}

func smbiosType(code int) string {
	switch code {
	case 24:
		return "DDR3"
	case 26:
		return "DDR4"
	case 34, 35, 36:
		return "DDR5"
	}
	return ""
}
