package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	ole "github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
	"golang.org/x/sys/windows"
)

func (a *App) SampleSources(queries []SourceQuery) []SourceSample {
	if queries == nil {
		return []SourceSample{}
	}
	return sampleAll(queries, platformReaders(a))
}

func platformReaders(a *App) Readers {
	return Readers{
		System:   a.readSystem,
		Temps:    readTemps,
		Media:    readNowPlaying,
		ReadFile: readSourceFile,
	}
}

func (a *App) readSystem() (map[string]string, error) {
	idle, total, err := systemTimes()
	if err != nil {
		return nil, err
	}
	mem, err := memoryStatus()
	if err != nil {
		return nil, err
	}
	free, diskTotal, err := diskSpace()
	if err != nil {
		return nil, err
	}
	netIn, netOut, err := interfaceOctets()
	if err != nil {
		return nil, err
	}
	cpu, down, up := a.stats.snapshot(idle, total, netIn, netOut)
	used := mem.total - mem.avail
	ramPercent := 0
	if mem.total > 0 {
		ramPercent = int(math.Round(float64(used) / float64(mem.total) * 100))
	}
	return map[string]string{
		"cpu":        cpu,
		"ramUsed":    formatBytes(float64(used)),
		"ramTotal":   formatBytes(float64(mem.total)),
		"ramPercent": strconv.Itoa(ramPercent),
		"diskFree":   formatBytes(float64(free)),
		"diskTotal":  formatBytes(float64(diskTotal)),
		"netDown":    down,
		"netUp":      up,
	}, nil
}

type filetime struct {
	Low  uint32
	High uint32
}

func (f filetime) uint() uint64 {
	return uint64(f.High)<<32 | uint64(f.Low)
}

var (
	kernel32           = windows.NewLazySystemDLL("kernel32.dll")
	procGetSystemTimes = kernel32.NewProc("GetSystemTimes")
	procGlobalMemory   = kernel32.NewProc("GlobalMemoryStatusEx")
)

func systemTimes() (idle, total uint64, err error) {
	var idleT, kernelT, userT filetime
	r, _, callErr := procGetSystemTimes.Call(
		uintptr(unsafe.Pointer(&idleT)),
		uintptr(unsafe.Pointer(&kernelT)),
		uintptr(unsafe.Pointer(&userT)),
	)
	if r == 0 {
		if callErr != nil && callErr != syscall.Errno(0) {
			return 0, 0, callErr
		}
		return 0, 0, fmt.Errorf("could not read CPU time")
	}
	// Kernel time already includes idle time.
	return idleT.uint(), kernelT.uint() + userT.uint(), nil
}

type memoryCounters struct {
	total uint64
	avail uint64
}

type memoryStatusEx struct {
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

func memoryStatus() (memoryCounters, error) {
	var mem memoryStatusEx
	mem.Length = uint32(unsafe.Sizeof(mem))
	r, _, callErr := procGlobalMemory.Call(uintptr(unsafe.Pointer(&mem)))
	if r == 0 {
		if callErr != nil && callErr != syscall.Errno(0) {
			return memoryCounters{}, callErr
		}
		return memoryCounters{}, fmt.Errorf("could not read memory")
	}
	return memoryCounters{total: mem.TotalPhys, avail: mem.AvailPhys}, nil
}

func diskSpace() (free, total uint64, err error) {
	drive := os.Getenv("SystemDrive")
	if drive == "" {
		drive = "C:"
	}
	var avail uint64
	err = windows.GetDiskFreeSpaceEx(windows.StringToUTF16Ptr(drive+`\`), &avail, &total, &free)
	if err != nil {
		return 0, 0, err
	}
	return avail, total, nil
}

func interfaceOctets() (in, out uint64, err error) {
	var table *windows.MibIfTable2
	if err := windows.GetIfTable2Ex(windows.MibIfTableNormal, &table); err != nil {
		return 0, 0, err
	}
	defer windows.FreeMibTable(unsafe.Pointer(table))
	if table == nil || table.NumEntries == 0 {
		return 0, 0, nil
	}
	rowSize := unsafe.Sizeof(windows.MibIfRow2{})
	base := uintptr(unsafe.Pointer(&table.Table[0]))
	for i := uint32(0); i < table.NumEntries; i++ {
		row := (*windows.MibIfRow2)(unsafe.Pointer(base + uintptr(i)*rowSize))
		if row.Type == 24 {
			continue
		}
		in += row.InOctets
		out += row.OutOctets
	}
	return in, out, nil
}

func readTemps() (map[string]string, error) {
	if err := ole.CoInitializeEx(0, ole.COINIT_MULTITHREADED); err == nil {
		defer ole.CoUninitialize()
	}
	unknown, err := oleutil.CreateObject("WbemScripting.SWbemLocator")
	if err != nil {
		return nil, errors.New(tempsUnavailable)
	}
	defer unknown.Release()
	wmi, err := unknown.QueryInterface(ole.IID_IDispatch)
	if err != nil {
		return nil, errors.New(tempsUnavailable)
	}
	defer wmi.Release()
	serviceRaw, err := oleutil.CallMethod(wmi, "ConnectServer", nil, `root\LibreHardwareMonitor`)
	if err != nil {
		return nil, errors.New(tempsUnavailable)
	}
	service := serviceRaw.ToIDispatch()
	defer service.Release()
	resultRaw, err := oleutil.CallMethod(service, "ExecQuery", "SELECT Name, Value FROM Sensor WHERE SensorType = 'Temperature'")
	if err != nil {
		return nil, errors.New(tempsUnavailable)
	}
	result := resultRaw.ToIDispatch()
	defer result.Release()
	fields := map[string]string{}
	err = oleutil.ForEach(result, func(v *ole.VARIANT) error {
		item := v.ToIDispatch()
		if item == nil {
			return nil
		}
		defer item.Release()
		nameVar, err := oleutil.GetProperty(item, "Name")
		if err != nil {
			return nil
		}
		defer nameVar.Clear()
		valueVar, err := oleutil.GetProperty(item, "Value")
		if err != nil {
			return nil
		}
		defer valueVar.Clear()
		name := fmt.Sprint(nameVar.Value())
		temp := tempString(valueVar.Value())
		if temp == "" {
			return nil
		}
		if fields["cpu"] == "" && (strings.Contains(name, "CPU Package") || name == "Package") {
			fields["cpu"] = temp
		}
		if fields["gpu"] == "" && strings.Contains(name, "GPU Core") {
			fields["gpu"] = temp
		}
		return nil
	})
	if err != nil {
		return nil, errors.New(tempsUnavailable)
	}
	if len(fields) == 0 {
		return nil, fmt.Errorf("no temperature sensors reported")
	}
	return fields, nil
}

func tempString(value any) string {
	parsed, err := strconv.ParseFloat(strings.TrimSpace(fmt.Sprint(value)), 64)
	if err != nil || math.IsNaN(parsed) {
		return ""
	}
	return strconv.Itoa(int(math.Round(parsed)))
}

func readNowPlaying() (map[string]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", nowPlayingScript)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = err.Error()
		}
		return nil, fmt.Errorf("could not read now playing: %s", detail)
	}
	var parsed struct {
		Title  string `json:"title"`
		Artist string `json:"artist"`
		Album  string `json:"album"`
		State  string `json:"state"`
	}
	if err := jsonUnmarshal(stdout.Bytes(), &parsed); err != nil {
		return nil, fmt.Errorf("could not read now playing")
	}
	if parsed.State == "" {
		parsed.State = "none"
	}
	return map[string]string{
		"title":  parsed.Title,
		"artist": parsed.Artist,
		"album":  parsed.Album,
		"state":  parsed.State,
	}, nil
}

func jsonUnmarshal(data []byte, dest any) error {
	data = bytes.TrimSpace(data)
	if i := bytes.IndexByte(data, '{'); i > 0 {
		data = data[i:]
	}
	return json.Unmarshal(data, dest)
}

var nowPlayingScript = strings.Join([]string{
	"$ErrorActionPreference = 'Stop'",
	"[void][Windows.Media.Control.GlobalSystemMediaTransportControlsSessionManager, Windows.Media.Control, ContentType = WindowsRuntime]",
	"$asTask = @([System.WindowsRuntimeSystemExtensions].GetMethods() | Where-Object { $_.Name -eq 'AsTask' -and $_.GetParameters().Count -eq 1 -and $_.GetParameters()[0].ParameterType.Name -eq 'IAsyncOperation" + "`" + "1' })[0]",
	"function Await([object]$op) {",
	"  $task = $asTask.MakeGenericMethod($op.GetType().GenericTypeArguments[0]).Invoke($null, @(,$op))",
	"  if (-not $task.Wait(1500)) { throw 'timed out' }",
	"  if ($task.IsFaulted) { throw $task.Exception.GetBaseException().Message }",
	"  return $task.Result",
	"}",
	"$mgr = Await ([Windows.Media.Control.GlobalSystemMediaTransportControlsSessionManager]::RequestAsync())",
	"$session = $mgr.GetCurrentSession()",
	"if ($null -eq $session) { Write-Output '{\"title\":\"\",\"artist\":\"\",\"album\":\"\",\"state\":\"none\"}'; exit 0 }",
	"$props = Await ($session.TryGetMediaPropertiesAsync())",
	"$status = ([string]$session.GetPlaybackInfo().PlaybackStatus).ToLower()",
	"[ordered]@{ title = [string]$props.Title; artist = [string]$props.Artist; album = [string]$props.AlbumTitle; state = $status } | ConvertTo-Json -Compress",
}, "\n")
