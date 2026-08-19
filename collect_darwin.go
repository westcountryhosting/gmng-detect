//go:build darwin

package main

import (
	"encoding/json"
	"os/exec"
	"strconv"
	"strings"
)

// macOS: system_profiler ships with every Mac and outputs clean JSON.
func collect() Spec {
	s := Spec{}
	out, err := exec.Command("system_profiler", "-json",
		"SPHardwareDataType", "SPDisplaysDataType", "SPMemoryDataType",
		"SPStorageDataType", "SPSoftwareDataType").Output()
	if err != nil {
		s.Notes = append(s.Notes, "Could not read hardware details on this Mac.")
		return s
	}
	var data map[string][]map[string]interface{}
	if json.Unmarshal(out, &data) != nil {
		return s
	}

	if sw := data["SPSoftwareDataType"]; len(sw) > 0 {
		s.OSVersion = str(sw[0]["os_version"])
	}
	if disp := data["SPDisplaysDataType"]; len(disp) > 0 {
		for _, ge := range disp {
			if nd, ok := ge["spdisplays_ndrvs"].([]interface{}); ok {
				for _, di := range nd {
					if m, ok := di.(map[string]interface{}); ok {
						r := firstNonEmpty(str(m["_spdisplays_resolution"]), str(m["spdisplays_resolution"]))
						if w, h := parseRes(r); w > 0 {
							s.Displays = append(s.Displays, Display{Width: w, Height: h})
						}
					}
				}
			}
		}
	}
	if stg := data["SPStorageDataType"]; len(stg) > 0 {
		for _, d := range stg {
			s.Storage = append(s.Storage, Disk{
				Model: str(d["_name"]), Kind: "SSD",
				SizeGB: bytesToGB(d["size_in_bytes"]), FreeGB: bytesToGB(d["free_space_in_bytes"])})
		}
	}

	if hw := data["SPHardwareDataType"]; len(hw) > 0 {
		h := hw[0]
		s.Manufacturer = "Apple"
		s.SystemModel = str(h["machine_name"])
		if s.SystemModel == "" {
			s.SystemModel = str(h["machine_model"])
		}
		// Apple Silicon reports "chip_type"; Intel Macs "cpu_type".
		s.CPU = firstNonEmpty(str(h["chip_type"]), str(h["cpu_type"]))
		s.CPUCores = atoiField(h["number_processors"])
		s.CPUThreads = s.CPUCores
		if pm := str(h["physical_memory"]); pm != "" {
			s.RAMTotalMB = memStringToMB(pm)
		}
		s.BIOS = str(h["boot_rom_version"])
	}

	if disp := data["SPDisplaysDataType"]; len(disp) > 0 {
		for _, d := range disp {
			if name := str(d["sppci_model"]); name != "" {
				s.GPU = append(s.GPU, name)
			}
		}
	}

	if mem := data["SPMemoryDataType"]; len(mem) > 0 {
		for _, blk := range mem {
			// Intel Macs expose dimms; Apple Silicon has unified memory (no modules).
			if items, ok := blk["_items"].([]interface{}); ok {
				for _, it := range items {
					if m, ok := it.(map[string]interface{}); ok {
						mod := RAMModule{Slot: str(m["_name"]), Type: str(m["dimm_type"])}
						mod.CapacityMB = memStringToMB(str(m["dimm_size"]))
						mod.SpeedMHz = firstInt(str(m["dimm_speed"]))
						if mod.CapacityMB > 0 {
							s.RAMModules = append(s.RAMModules, mod)
						}
					}
				}
			}
		}
	}
	if len(s.RAMModules) == 0 {
		s.Notes = append(s.Notes, "This Mac uses unified memory, so there are no upgradeable RAM slots.")
	}
	return s
}

func str(v interface{}) string {
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func atoiField(v interface{}) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(t))
		return n
	}
	return 0
}

// memStringToMB parses "16 GB" / "8192 MB" into megabytes.
func memStringToMB(s string) int {
	s = strings.TrimSpace(s)
	n := firstInt(s)
	if strings.Contains(strings.ToUpper(s), "GB") {
		return n * 1024
	}
	return n
}

func firstInt(s string) int {
	num := ""
	for _, r := range s {
		if r >= '0' && r <= '9' {
			num += string(r)
		} else if num != "" {
			break
		}
	}
	n, _ := strconv.Atoi(num)
	return n
}
