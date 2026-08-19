//go:build linux

package main

import (
	"bufio"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"syscall"
)

func readFileTrim(p string) string {
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func collect() Spec {
	s := Spec{}
	dmi := "/sys/devices/virtual/dmi/id/"

	s.Manufacturer = readFileTrim(dmi + "sys_vendor")
	s.SystemModel = readFileTrim(dmi + "product_name")
	boardVendor := readFileTrim(dmi + "board_vendor")
	boardName := readFileTrim(dmi + "board_name")
	s.Motherboard = strings.TrimSpace(boardVendor + " " + boardName)
	s.BIOS = readFileTrim(dmi + "bios_version")

	// CPU model + core/thread counts from /proc/cpuinfo.
	if f, err := os.Open("/proc/cpuinfo"); err == nil {
		defer f.Close()
		sc := bufio.NewScanner(f)
		threads := 0
		coreIDs := map[string]bool{}
		curPhys, curCore := "", ""
		for sc.Scan() {
			line := sc.Text()
			if strings.HasPrefix(line, "model name") && s.CPU == "" {
				if i := strings.Index(line, ":"); i >= 0 {
					s.CPU = strings.TrimSpace(line[i+1:])
				}
			}
			if strings.HasPrefix(line, "processor") {
				threads++
			}
			if strings.HasPrefix(line, "physical id") {
				curPhys = strings.TrimSpace(line[strings.Index(line, ":")+1:])
			}
			if strings.HasPrefix(line, "core id") {
				curCore = strings.TrimSpace(line[strings.Index(line, ":")+1:])
				coreIDs[curPhys+"/"+curCore] = true
			}
		}
		s.CPUThreads = threads
		s.CPUCores = len(coreIDs)
		if s.CPUCores == 0 {
			s.CPUCores = threads
		}
	}

	// Total RAM from /proc/meminfo (kB).
	if v := grepMeminfo("MemTotal"); v > 0 {
		s.RAMTotalMB = v / 1024
	}

	// GPU names via lspci.
	if out, err := exec.Command("bash", "-c", "lspci 2>/dev/null | grep -Ei 'vga|3d|display'").Output(); err == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if i := strings.Index(line, ": "); i >= 0 {
				name := strings.TrimSpace(line[i+2:])
				if name != "" {
					s.GPU = append(s.GPU, name)
				}
			}
		}
	}

	// RAM modules + slot count need DMI decode (root). Best-effort.
	if out, err := exec.Command("dmidecode", "-t", "memory").Output(); err == nil {
		parseDmidecodeMemory(string(out), &s)
	} else {
		s.Notes = append(s.Notes, "Per-module RAM and free slots need elevated access; run again with sudo for those.")
	}

	// OS version.
	if osr := readFileTrim("/etc/os-release"); osr != "" {
		for _, line := range strings.Split(osr, "\n") {
			if strings.HasPrefix(line, "PRETTY_NAME=") {
				s.OSVersion = strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), "\"")
			}
		}
	}
	// Display resolution (best-effort; needs a running display server).
	if out, err := exec.Command("bash", "-c", "xrandr 2>/dev/null | grep '\\*' | head -1").Output(); err == nil {
		f := strings.Fields(string(out))
		if len(f) > 0 {
			if w, h := parseRes(strings.ReplaceAll(f[0], "x", " ")); w > 0 {
				s.Displays = append(s.Displays, Display{Width: w, Height: h})
			}
		}
	}
	// Storage: physical disks + free space on root.
	if out, err := exec.Command("lsblk", "-bdno", "NAME,SIZE,TYPE,MODEL").Output(); err == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			f := strings.Fields(line)
			if len(f) >= 3 && f[2] == "disk" {
				sz, _ := strconv.ParseInt(f[1], 10, 64)
				model := ""
				if len(f) > 3 {
					model = strings.Join(f[3:], " ")
				}
				s.Storage = append(s.Storage, Disk{Model: model, SizeGB: int(sz / (1024 * 1024 * 1024))})
			}
		}
	}
	var fs syscall.Statfs_t
	if syscall.Statfs("/", &fs) == nil && len(s.Storage) > 0 {
		s.Storage[0].FreeGB = int(int64(fs.Bavail) * int64(fs.Bsize) / (1024 * 1024 * 1024))
	}

	return s
}

func grepMeminfo(key string) int {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if strings.HasPrefix(sc.Text(), key) {
			fields := strings.Fields(sc.Text())
			if len(fields) >= 2 {
				n, _ := strconv.Atoi(fields[1])
				return n
			}
		}
	}
	return 0
}

var reSize = regexp.MustCompile(`(?i)Size:\s*(\d+)\s*([MG])B`)
var reSpeed = regexp.MustCompile(`(?i)Speed:\s*(\d+)\s*MT/s`)

func parseDmidecodeMemory(out string, s *Spec) {
	blocks := strings.Split(out, "Memory Device")
	slots := 0
	for _, b := range blocks {
		if !strings.Contains(b, "Locator:") {
			continue
		}
		slots++
		m := RAMModule{}
		if sz := reSize.FindStringSubmatch(b); sz != nil {
			n, _ := strconv.Atoi(sz[1])
			if strings.EqualFold(sz[2], "G") {
				n *= 1024
			}
			m.CapacityMB = n
		}
		if sp := reSpeed.FindStringSubmatch(b); sp != nil {
			m.SpeedMHz, _ = strconv.Atoi(sp[1])
		}
		for _, l := range strings.Split(b, "\n") {
			l = strings.TrimSpace(l)
			if strings.HasPrefix(l, "Locator:") && !strings.HasPrefix(l, "Bank") {
				m.Slot = strings.TrimSpace(strings.TrimPrefix(l, "Locator:"))
			}
			if strings.HasPrefix(l, "Type:") && (strings.Contains(l, "DDR")) {
				m.Type = strings.TrimSpace(strings.TrimPrefix(l, "Type:"))
			}
		}
		if m.CapacityMB > 0 {
			s.RAMModules = append(s.RAMModules, m)
		}
	}
	s.RAMSlotsTotal = slots
}
