package main

import "testing"

func TestPickGPUPrefersTheDiscreteCard(t *testing.T) {
	got, ok := pickGPU([]gpuAdapter{
		{Name: "Intel", LUID: "luid_0x00000000_0x00013797", Dedicated: 128 << 20},
		{Name: "NVIDIA", LUID: "luid_0x00000000_0x000111ea", Dedicated: 8 << 30},
	})
	if !ok || got.Name != "NVIDIA" {
		t.Fatalf("%v %+v", ok, got)
	}
}

func TestGPUBusyPercentSumsOneEngine(t *testing.T) {
	luid := "luid_0x00000000_0x000111ea"
	samples := []gpuCounter{
		{name: "pid_1_" + luid + "_phys_0_eng_0_engtype_3d", value: 20},
		{name: "pid_2_" + luid + "_phys_0_eng_1_engtype_3d", value: 15},
		{name: "pid_1_" + luid + "_phys_0_eng_2_engtype_copy", value: 4},
		{name: "pid_9_luid_0x00000000_0x00013797_phys_0_eng_0_engtype_3d", value: 90},
	}
	got, ok := gpuBusyPercent(samples, luid)
	if !ok || got != 35 {
		t.Fatalf("%v %d", ok, got)
	}
	used, ok := gpuMemoryUsed([]gpuCounter{
		{name: luid + "_phys_0", value: 4817235968},
		{name: "luid_0x00000000_0x00013797_phys_0", value: 100},
	}, luid)
	if !ok || used != 4817235968 {
		t.Fatalf("%v %d", ok, used)
	}
}

func TestGPUStatFields(t *testing.T) {
	fields := gpuStatFields(gpuAdapter{Name: "NVIDIA", Dedicated: 8 << 30}, 12, true, 4<<30, true)
	if fields["gpu"] != "12" || fields["gpuName"] != "NVIDIA" || fields["gpuMemPercent"] != "50" {
		t.Fatalf("%+v", fields)
	}
	if fields["gpuMemUsed"] != "4.0 GB" || fields["gpuMemTotal"] != "8.0 GB" {
		t.Fatalf("%+v", fields)
	}
}
