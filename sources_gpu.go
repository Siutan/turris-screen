package main

import (
	"math"
	"strconv"
	"strings"
)

// gpuAdapter is one display adapter. LUID matches the Windows performance-counter name.
type gpuAdapter struct {
	Name      string
	LUID      string
	Dedicated uint64
}

// pickGPU chooses the adapter with the most dedicated memory, which is the discrete card
// when the machine also has an integrated one.
func pickGPU(adapters []gpuAdapter) (gpuAdapter, bool) {
	if len(adapters) == 0 {
		return gpuAdapter{}, false
	}
	best := adapters[0]
	for _, adapter := range adapters[1:] {
		if adapter.Dedicated > best.Dedicated {
			best = adapter
		}
	}
	return best, true
}

type gpuCounter struct {
	name  string
	value float64
}

// gpuBusyPercent is the busiest engine on one adapter. Windows reports each process
// separately, so engines of the same type are added, then the busiest type wins.
func gpuBusyPercent(samples []gpuCounter, luid string) (int, bool) {
	if luid == "" {
		return 0, false
	}
	token := strings.ToLower(luid)
	sums := map[string]float64{}
	seen := false
	for _, sample := range samples {
		name := strings.ToLower(sample.name)
		if !strings.Contains(name, token) {
			continue
		}
		seen = true
		kind := "other"
		if i := strings.Index(name, "engtype_"); i >= 0 {
			kind = name[i+len("engtype_"):]
		}
		sums[kind] += sample.value
	}
	if !seen {
		return 0, false
	}
	max := 0.0
	for _, value := range sums {
		if value > max {
			max = value
		}
	}
	if max > 100 {
		max = 100
	}
	return int(math.Round(max)), true
}

// gpuMemoryUsed sums dedicated-memory counters for one adapter.
func gpuMemoryUsed(samples []gpuCounter, luid string) (uint64, bool) {
	if luid == "" {
		return 0, false
	}
	token := strings.ToLower(luid)
	var total uint64
	seen := false
	for _, sample := range samples {
		if !strings.Contains(strings.ToLower(sample.name), token) {
			continue
		}
		seen = true
		if sample.value > 0 {
			total += uint64(sample.value)
		}
	}
	return total, seen
}

func gpuStatFields(adapter gpuAdapter, percent int, percentOK bool, used uint64, usedOK bool) map[string]string {
	out := map[string]string{}
	if adapter.Name != "" {
		out["gpuName"] = adapter.Name
	}
	if percentOK {
		out["gpu"] = strconv.Itoa(percent)
	}
	if usedOK {
		out["gpuMemUsed"] = formatBytes(float64(used))
	}
	if adapter.Dedicated > 0 {
		out["gpuMemTotal"] = formatBytes(float64(adapter.Dedicated))
		if usedOK {
			pct := int(math.Round(float64(used) / float64(adapter.Dedicated) * 100))
			if pct > 100 {
				pct = 100
			}
			out["gpuMemPercent"] = strconv.Itoa(pct)
		}
	}
	return out
}
