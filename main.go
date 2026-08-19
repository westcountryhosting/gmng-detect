// GMNG Detect — a tiny local helper that reads this machine's hardware and hands it to
// gmng.co.uk. It makes one outbound HTTPS upload keyed by a random token, then opens your
// browser to finish. No listening socket, no telemetry beyond that upload, no paste.
package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// parseRes pulls "W x H" out of a resolution string like "2560 x 1600 @ 60Hz".
func parseRes(s string) (int, int) {
	var nums []int
	cur := ""
	for _, r := range s {
		if r >= '0' && r <= '9' {
			cur += string(r)
		} else if cur != "" {
			n, _ := strconv.Atoi(cur)
			nums = append(nums, n)
			cur = ""
			if len(nums) >= 2 {
				break
			}
		}
	}
	if cur != "" && len(nums) < 2 {
		n, _ := strconv.Atoi(cur)
		nums = append(nums, n)
	}
	if len(nums) >= 2 {
		return nums[0], nums[1]
	}
	return 0, 0
}

// bytesToGB coerces a JSON number/string of bytes into whole gigabytes.
func bytesToGB(v interface{}) int {
	var b float64
	switch t := v.(type) {
	case float64:
		b = t
	case int64:
		b = float64(t)
	case int:
		b = float64(t)
	case string:
		num := ""
		for _, r := range t {
			if r >= '0' && r <= '9' {
				num += string(r)
			} else if num != "" {
				break
			}
		}
		f, _ := strconv.ParseFloat(num, 64)
		b = f
	}
	return int(b / (1024 * 1024 * 1024))
}

const version = "1.2.0"

func siteURL() string {
	if s := os.Getenv("GMNG_SITE"); s != "" {
		return strings.TrimRight(s, "/")
	}
	return "https://gmng.co.uk"
}

type RAMModule struct {
	CapacityMB int    `json:"capacity_mb"`
	SpeedMHz   int    `json:"speed_mhz"`
	Type       string `json:"type"`
	Slot       string `json:"slot"`
}

type Display struct {
	Width     int `json:"width"`
	Height    int `json:"height"`
	RefreshHz int `json:"refresh_hz"`
}

type Disk struct {
	Model  string `json:"model"`
	Kind   string `json:"kind"` // SSD / NVMe / HDD
	SizeGB int    `json:"size_gb"`
	FreeGB int    `json:"free_gb"`
}

type Spec struct {
	Tool          string      `json:"tool"`
	Version       string      `json:"version"`
	OS            string      `json:"os"`
	OSVersion     string      `json:"os_version"`
	Manufacturer  string      `json:"manufacturer"`
	SystemModel   string      `json:"system_model"`
	CPU           string      `json:"cpu"`
	CPUCores      int         `json:"cpu_cores"`
	CPUThreads    int         `json:"cpu_threads"`
	GPU           []string    `json:"gpu"`
	GPUDriver     string      `json:"gpu_driver"`
	RAMTotalMB    int         `json:"ram_total_mb"`
	RAMModules    []RAMModule `json:"ram_modules"`
	RAMSlotsTotal int         `json:"ram_slots_total"`
	Motherboard   string      `json:"motherboard"`
	BIOS          string      `json:"bios"`
	Displays      []Display   `json:"displays"`
	Storage       []Disk      `json:"storage"`
	Notes         []string    `json:"notes"`
	CollectedAt   string      `json:"collected_at"`
}

// discreteFirst orders GPUs so a real add-in card (NVIDIA/AMD) comes before integrated
// or virtual adapters, so downstream picks the card that actually matters.
func discreteFirst(gpus []string) []string {
	var disc, integ, other []string
	for _, g := range gpus {
		l := strings.ToLower(g)
		switch {
		case strings.Contains(l, "virtual") || strings.Contains(l, "basic") || strings.Contains(l, "meta ") || strings.Contains(l, "parsec"):
			other = append(other, g)
		case strings.Contains(l, "geforce") || strings.Contains(l, "radeon rx") || strings.Contains(l, "nvidia") || strings.Contains(l, "arc a") || strings.Contains(l, "arc b"):
			disc = append(disc, g)
		case strings.Contains(l, "iris") || strings.Contains(l, "uhd") || strings.Contains(l, "vega") || strings.Contains(l, "radeon graphics") || strings.Contains(l, "hd graphics"):
			integ = append(integ, g)
		default:
			other = append(other, g)
		}
	}
	return append(append(disc, integ...), other...)
}

func newToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}

func dash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "not detected"
	}
	return s
}

func main() {
	fmt.Println("GMNG Detect " + version)
	fmt.Println("Reading your hardware...")

	spec := collect()
	spec.Tool = "gmng-detect"
	spec.Version = version
	spec.OS = runtime.GOOS
	spec.GPU = discreteFirst(spec.GPU)
	spec.CollectedAt = time.Now().UTC().Format(time.RFC3339)

	fmt.Println()
	if spec.SystemModel != "" {
		fmt.Printf("  System : %s %s\n", spec.Manufacturer, spec.SystemModel)
	}
	fmt.Printf("  CPU    : %s (%d cores / %d threads)\n", dash(spec.CPU), spec.CPUCores, spec.CPUThreads)
	fmt.Printf("  GPU    : %s\n", dash(strings.Join(spec.GPU, ", ")))
	fmt.Printf("  RAM    : %d GB across %d module(s)\n", spec.RAMTotalMB/1024, len(spec.RAMModules))
	if len(spec.Displays) > 0 {
		d := spec.Displays[0]
		fmt.Printf("  Screen : %dx%d @ %d Hz\n", d.Width, d.Height, d.RefreshHz)
	}
	if len(spec.Storage) > 0 {
		fmt.Printf("  Storage: %d drive(s)\n", len(spec.Storage))
	}
	if spec.Motherboard != "" {
		fmt.Printf("  Board  : %s\n", spec.Motherboard)
	}
	fmt.Println()

	token := newToken()
	body, _ := json.Marshal(spec)

	client := &http.Client{Timeout: 20 * time.Second}
	url := siteURL() + "/api/detect-ingest?token=" + token
	fmt.Println("Sending to GMNG...")
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "GMNG-Detect/"+version)
	resp, err := client.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		if resp != nil {
			resp.Body.Close()
		}
		fmt.Println("Could not reach gmng.co.uk. Check your internet connection and try again.")
		pause()
		return
	}
	resp.Body.Close()

	finish := fmt.Sprintf("%s/detect?token=%s", siteURL(), token)
	fmt.Println("Done. Opening your browser to finish. If it does not open, go to:")
	fmt.Println("  " + finish)
	openBrowser(finish)
	time.Sleep(2 * time.Second)
}

func pause() {
	fmt.Println("Press Enter to close.")
	fmt.Scanln()
}
