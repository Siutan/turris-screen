//go:build !windows

package turris

import "fmt"

func Open() (*Device, error) {
	return nil, fmt.Errorf("the cooler connection needs Windows")
}

func resetLink(link) error {
	return fmt.Errorf("the cooler connection needs Windows")
}

func findDevices() ([]candidate, error) {
	return nil, fmt.Errorf("the cooler connection needs Windows")
}
