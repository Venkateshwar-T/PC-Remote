package win32

import (
	"fmt"
	"math"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

var (
	modKernel32 = syscall.NewLazyDLL("kernel32.dll")

	procGetSystemPowerStatus = modKernel32.NewProc("GetSystemPowerStatus")
	procGlobalMemoryStatusEx = modKernel32.NewProc("GlobalMemoryStatusEx")
	procGetTickCount64       = modKernel32.NewProc("GetTickCount64")
	procGetSystemTimes       = modKernel32.NewProc("GetSystemTimes")
)

type SystemPowerStatus struct {
	ACLineStatus        byte
	BatteryFlag         byte
	BatteryLifePercent  byte
	SystemStatusFlag    byte
	BatteryLifeTime     uint32
	BatteryFullLifeTime uint32
}

type MemoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

type FileTime struct {
	LowDateTime  uint32
	HighDateTime uint32
}

func (ft FileTime) ToUint64() uint64 {
	return (uint64(ft.HighDateTime) << 32) | uint64(ft.LowDateTime)
}

type TelemetryData struct {
	DeviceName    string  `json:"deviceName"`
	Battery       int     `json:"battery"`
	Charging      bool    `json:"charging"`
	BatteryStatus string  `json:"batteryStatus"`
	CpuPercent    int     `json:"cpu"`
	RamPercent    int     `json:"ramPercent"`
	RamUsedGb     float64 `json:"ramUsedGb"`
	RamTotalGb    float64 `json:"ramTotalGb"`
	Uptime        string  `json:"uptime"`
	NetworkName   string  `json:"networkName"`
	NetworkType   string  `json:"networkType"`
	IsLocked      bool    `json:"isLocked"`
}

var (
	cpuLock     sync.Mutex
	lastIdle    uint64
	lastKernel  uint64
	lastUser    uint64
	lastCpuCalc int
	lastCpuTime time.Time
)

// GetCpuLoad returns current CPU usage percentage (0-100)
func GetCpuLoad() int {
	cpuLock.Lock()
	defer cpuLock.Unlock()

	now := time.Now()
	if now.Sub(lastCpuTime) < 1500*time.Millisecond && lastCpuTime != (time.Time{}) {
		return lastCpuCalc
	}

	var idle, kernel, user FileTime
	ret, _, _ := procGetSystemTimes.Call(
		uintptr(unsafe.Pointer(&idle)),
		uintptr(unsafe.Pointer(&kernel)),
		uintptr(unsafe.Pointer(&user)),
	)
	if ret == 0 {
		return 0
	}

	curIdle := idle.ToUint64()
	curKernel := kernel.ToUint64()
	curUser := user.ToUint64()

	if lastIdle == 0 {
		lastIdle = curIdle
		lastKernel = curKernel
		lastUser = curUser
		lastCpuTime = now
		return 0
	}

	diffIdle := curIdle - lastIdle
	diffKernel := curKernel - lastKernel
	diffUser := curUser - lastUser

	total := diffKernel + diffUser
	if total == 0 {
		return 0
	}

	pct := int(((total - diffIdle) * 100) / total)
	if pct < 0 {
		pct = 0
	} else if pct > 100 {
		pct = 100
	}

	lastIdle = curIdle
	lastKernel = curKernel
	lastUser = curUser
	lastCpuCalc = pct
	lastCpuTime = now

	return pct
}

// GetBatteryInfo queries Windows power subsystem
func GetBatteryInfo() (pct int, charging bool, statusText string) {
	var sps SystemPowerStatus
	ret, _, _ := procGetSystemPowerStatus.Call(uintptr(unsafe.Pointer(&sps)))
	if ret == 0 {
		return 100, false, "AC Power"
	}

	pct = int(sps.BatteryLifePercent)
	if pct > 100 || sps.BatteryFlag&128 != 0 { // 128 = No system battery (desktop)
		return 100, false, "AC Connected"
	}

	isPlugged := (sps.ACLineStatus == 1)
	isCharging := (sps.BatteryFlag&8 != 0) || (isPlugged && pct < 100)

	switch {
	case isCharging:
		statusText = "Charging"
	case isPlugged:
		statusText = "Plugged In"
	default:
		statusText = "On Battery"
	}

	return pct, isCharging, statusText
}

// GetMemoryInfo returns RAM used, total, and percentage
func GetMemoryInfo() (usedGb float64, totalGb float64, pct int) {
	var mem MemoryStatusEx
	mem.Length = uint32(unsafe.Sizeof(mem))

	ret, _, _ := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&mem)))
	if ret == 0 {
		return 0, 0, 0
	}

	const gb = 1024 * 1024 * 1024
	totalGb = math.Round(float64(mem.TotalPhys)/gb*10) / 10
	availGb := math.Round(float64(mem.AvailPhys)/gb*10) / 10
	usedGb = math.Round((totalGb-availGb)*10) / 10
	pct = int(mem.MemoryLoad)

	return usedGb, totalGb, pct
}

// GetFormattedUptime returns formatted system uptime (e.g., "2d 4h 12m")
func GetFormattedUptime() string {
	ret, _, _ := procGetTickCount64.Call()
	ms := uint64(ret)
	d := time.Duration(ms) * time.Millisecond

	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	mins := int(d.Minutes()) % 60

	if days > 0 {
		return fmt.Sprintf("%dd %dh %dm", days, hours, mins)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, mins)
	}
	return fmt.Sprintf("%dm", mins)
}

var (
	netMu         sync.Mutex
	cachedNetName = "Connected"
	cachedNetType = "Wi-Fi"
	lastNetCheck  time.Time

	lockMu             sync.Mutex
	lastLockActionTime time.Time
	lastLockCheckTime  time.Time
	cachedIsLocked     bool
)

func SetLockedManually() {
	lockMu.Lock()
	defer lockMu.Unlock()
	lastLockActionTime = time.Now()
	cachedIsLocked = true
}

func IsWorkstationLocked() bool {
	lockMu.Lock()
	defer lockMu.Unlock()

	if time.Since(lastLockActionTime) < 6*time.Second && !lastLockActionTime.IsZero() {
		return true
	}

	if time.Since(lastLockCheckTime) < 3*time.Second {
		return cachedIsLocked
	}

	cmd := exec.Command("tasklist.exe", "/FI", "IMAGENAME eq LogonUI.exe")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()
	cachedIsLocked = (err == nil && strings.Contains(string(out), "LogonUI.exe"))
	lastLockCheckTime = time.Now()
	return cachedIsLocked
}

func GetNetworkInfo() (string, string) {
	netMu.Lock()
	defer netMu.Unlock()

	if time.Since(lastNetCheck) < 60*time.Second && cachedNetName != "Connected" {
		return cachedNetName, cachedNetType
	}

	// Single command retrieves both Name and InterfaceAlias together
	cmd := exec.Command("powershell", "-NoProfile", "-Command", "$p = Get-NetConnectionProfile | Select-Object -First 1; if ($p) { Write-Output ($p.Name + '|' + $p.InterfaceAlias) }")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()
	if err == nil {
		parts := strings.Split(strings.TrimSpace(string(out)), "|")
		if len(parts) >= 2 {
			name := strings.TrimSpace(parts[0])
			alias := strings.TrimSpace(parts[1])
			if name != "" {
				cachedNetName = name
			}
			if alias != "" {
				cachedNetType = alias
			}
		} else if len(parts) == 1 && parts[0] != "" {
			cachedNetName = strings.TrimSpace(parts[0])
		}
	}

	lastNetCheck = time.Now()
	return cachedNetName, cachedNetType
}

// QueryTelemetry collects all telemetry metrics in microseconds
func QueryTelemetry(deviceName string) TelemetryData {
	bat, charging, batStatus := GetBatteryInfo()
	usedGb, totalGb, ramPct := GetMemoryInfo()
	cpu := GetCpuLoad()
	uptime := GetFormattedUptime()
	netName, netType := GetNetworkInfo()
	locked := IsWorkstationLocked()

	return TelemetryData{
		DeviceName:    deviceName,
		Battery:       bat,
		Charging:      charging,
		BatteryStatus: batStatus,
		CpuPercent:    cpu,
		RamPercent:    ramPct,
		RamUsedGb:     usedGb,
		RamTotalGb:    totalGb,
		Uptime:        uptime,
		NetworkName:   netName,
		NetworkType:   netType,
		IsLocked:      locked,
	}
}

