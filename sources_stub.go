//go:build !windows

package main

import "fmt"

func (a *App) SampleSources(queries []SourceQuery) []SourceSample {
	if queries == nil {
		return []SourceSample{}
	}
	return sampleAll(queries, platformReaders(a))
}

func platformReaders(*App) Readers {
	unavailable := func() (map[string]string, error) {
		return nil, fmt.Errorf("live sources need Windows")
	}
	return Readers{
		System:   unavailable,
		Temps:    func() (map[string]string, error) { return nil, fmt.Errorf(tempsUnavailable) },
		Media:    unavailable,
		ReadFile: readSourceFile,
	}
}
