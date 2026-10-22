//go:build windows

package main

import (
	"testing"
	"time"
	"unsafe"
)

func TestGPUFieldsFromThisPC(t *testing.T) {
	if unsafe.Sizeof(pdhFmtItem{}) != 24 {
		t.Fatalf("counter item size %d", unsafe.Sizeof(pdhFmtItem{}))
	}
	adapters := listGPUAdapters()
	if len(adapters) == 0 {
		t.Fatal("no display adapter")
	}
	adapter, ok := pickGPU(adapters)
	if !ok || adapter.Name == "" || adapter.LUID == "" || adapter.Dedicated == 0 {
		t.Fatalf("%+v", adapters)
	}
	_ = readGPUFields()
	time.Sleep(400 * time.Millisecond)
	fields := readGPUFields()
	t.Logf("adapter %+v fields %+v", adapter, fields)
	if fields["gpuName"] != adapter.Name {
		t.Fatalf("name %+v", fields)
	}
	if fields["gpuMemTotal"] == "" || fields["gpuMemUsed"] == "" || fields["gpu"] == "" {
		t.Fatalf("fields %+v", fields)
	}
}
