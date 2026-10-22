//go:build windows

package main

import (
	"fmt"
	"math"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	pdhFmtDouble = 0x00000200
	pdhFmtLarge  = 0x00000400
	pdhMoreData  = 0x800007D2
	pdhValid     = 0
	pdhNewData   = 1

	dxgiErrorNotFound   = 0x887A0002
	dxgiAdapterSoftware = 2
)

var (
	pdhDLL                 = windows.NewLazySystemDLL("pdh.dll")
	procPdhOpenQuery       = pdhDLL.NewProc("PdhOpenQueryW")
	procPdhAddCounter      = pdhDLL.NewProc("PdhAddEnglishCounterW")
	procPdhCollect         = pdhDLL.NewProc("PdhCollectQueryData")
	procPdhFormattedArray  = pdhDLL.NewProc("PdhGetFormattedCounterArrayW")
	dxgiDLL                = windows.NewLazySystemDLL("dxgi.dll")
	procCreateDXGIFactory1 = dxgiDLL.NewProc("CreateDXGIFactory1")
)

// iidIDXGIFactory1 is 770aae78-f26f-4dba-a829-253c83d1b387.
var iidIDXGIFactory1 = windows.GUID{
	Data1: 0x770aae78,
	Data2: 0xf26f,
	Data3: 0x4dba,
	Data4: [8]byte{0xa8, 0x29, 0x25, 0x3c, 0x83, 0xd1, 0xb3, 0x87},
}

type pdhFmtItem struct {
	name    uintptr
	cStatus uint32
	_       uint32
	raw     uint64
}

type dxgiAdapterDesc1 struct {
	Description           [128]uint16
	VendorId              uint32
	DeviceId              uint32
	SubSysId              uint32
	Revision              uint32
	DedicatedVideoMemory  uintptr
	DedicatedSystemMemory uintptr
	SharedSystemMemory    uintptr
	LowPart               uint32
	HighPart              int32
	Flags                 uint32
}

type gpuQuery struct {
	mu     sync.Mutex
	query  uintptr
	util   uintptr
	mem    uintptr
	opened bool
	failed bool
}

var systemGPU gpuQuery

func readGPUFields() map[string]string {
	adapters := listGPUAdapters()
	adapter, ok := pickGPU(adapters)
	if !ok {
		return nil
	}
	percent, percentOK, used, usedOK := systemGPU.sample(adapter.LUID)
	return gpuStatFields(adapter, percent, percentOK, used, usedOK)
}

func (q *gpuQuery) sample(luid string) (percent int, percentOK bool, used uint64, usedOK bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.failed {
		return 0, false, 0, false
	}
	if !q.opened && !q.open() {
		q.failed = true
		return 0, false, 0, false
	}
	if r, _, _ := procPdhCollect.Call(q.query); r != 0 && uint32(r) != 0x800007D5 {
		return 0, false, 0, false
	}
	util := readCounters(q.util, pdhFmtDouble)
	mem := readCounters(q.mem, pdhFmtLarge)
	percent, percentOK = gpuBusyPercent(util, luid)
	used, usedOK = gpuMemoryUsed(mem, luid)
	return percent, percentOK, used, usedOK
}

func (q *gpuQuery) open() bool {
	var query uintptr
	if r, _, _ := procPdhOpenQuery.Call(0, 0, uintptr(unsafe.Pointer(&query))); r != 0 {
		return false
	}
	util, okUtil := addCounter(query, `\GPU Engine(*)\Utilization Percentage`)
	mem, okMem := addCounter(query, `\GPU Adapter Memory(*)\Dedicated Usage`)
	if !okUtil && !okMem {
		return false
	}
	q.query, q.util, q.mem, q.opened = query, util, mem, true
	return true
}

func addCounter(query uintptr, path string) (uintptr, bool) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, false
	}
	var counter uintptr
	r, _, _ := procPdhAddCounter.Call(query, uintptr(unsafe.Pointer(name)), 0, uintptr(unsafe.Pointer(&counter)))
	return counter, r == 0 && counter != 0
}

func readCounters(counter uintptr, format uintptr) []gpuCounter {
	if counter == 0 {
		return nil
	}
	var size, count uint32
	buf := make([]byte, 64<<10)
	size = uint32(len(buf))
	r, _, _ := procPdhFormattedArray.Call(counter, format, uintptr(unsafe.Pointer(&size)), uintptr(unsafe.Pointer(&count)), uintptr(unsafe.Pointer(&buf[0])))
	if uint32(r) == pdhMoreData {
		buf = make([]byte, size)
		r, _, _ = procPdhFormattedArray.Call(counter, format, uintptr(unsafe.Pointer(&size)), uintptr(unsafe.Pointer(&count)), uintptr(unsafe.Pointer(&buf[0])))
	}
	if r != 0 || count == 0 {
		return nil
	}
	stride := unsafe.Sizeof(pdhFmtItem{})
	if uintptr(len(buf)) < uintptr(count)*stride {
		return nil
	}
	out := make([]gpuCounter, 0, count)
	for i := uint32(0); i < count; i++ {
		item := (*pdhFmtItem)(unsafe.Pointer(&buf[uintptr(i)*stride]))
		if item.cStatus != pdhValid && item.cStatus != pdhNewData {
			continue
		}
		value := math.Float64frombits(item.raw)
		if format == pdhFmtLarge {
			value = float64(item.raw)
		}
		out = append(out, gpuCounter{name: utf16At(item.name), value: value})
	}
	return out
}

func utf16At(ptr uintptr) string {
	if ptr == 0 {
		return ""
	}
	chars := make([]uint16, 0, 64)
	for len(chars) < 512 {
		c := *(*uint16)(unsafe.Pointer(ptr))
		if c == 0 {
			break
		}
		chars = append(chars, c)
		ptr += 2
	}
	return windows.UTF16ToString(chars)
}

func listGPUAdapters() []gpuAdapter {
	var factory uintptr
	hr, _, _ := procCreateDXGIFactory1.Call(uintptr(unsafe.Pointer(&iidIDXGIFactory1)), uintptr(unsafe.Pointer(&factory)))
	if int32(hr) < 0 || factory == 0 {
		return nil
	}
	defer comRelease(factory)
	var out []gpuAdapter
	for i := uintptr(0); i < 16; i++ {
		var adapter uintptr
		// IDXGIFactory extends IDXGIObject, so EnumAdapters1 is slot 12.
		hr := comHR(factory, 12, i, uintptr(unsafe.Pointer(&adapter)))
		if hr == dxgiErrorNotFound || int32(hr) < 0 || adapter == 0 {
			break
		}
		var desc dxgiAdapterDesc1
		hr = comHR(adapter, 10, uintptr(unsafe.Pointer(&desc)))
		comRelease(adapter)
		if int32(hr) < 0 {
			continue
		}
		if desc.Flags&dxgiAdapterSoftware != 0 {
			continue
		}
		out = append(out, gpuAdapter{
			Name:      windows.UTF16ToString(desc.Description[:]),
			LUID:      fmt.Sprintf("luid_0x%08x_0x%08x", uint32(desc.HighPart), desc.LowPart),
			Dedicated: uint64(desc.DedicatedVideoMemory),
		})
	}
	return out
}

func comHR(obj uintptr, index int, args ...uintptr) uint32 {
	vt := *(*uintptr)(unsafe.Pointer(obj))
	proc := *(*uintptr)(unsafe.Pointer(vt + uintptr(index)*unsafe.Sizeof(uintptr(0))))
	hr, _, _ := syscall.SyscallN(proc, append([]uintptr{obj}, args...)...)
	return uint32(hr)
}

func comRelease(obj uintptr) {
	if obj == 0 {
		return
	}
	_ = comHR(obj, 2)
}
