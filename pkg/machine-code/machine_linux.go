//go:build linux
// +build linux

package machinecode

import (
	"fmt"
	"os"
	"strings"
)

func GetMachineId() (string, error) {
	path := []string{"/etc/machine-id", "/var/lib/dbus/machine-id"}
	var err error
	for _, p := range path {
		if _, err = os.Stat(p); err == nil {
			var content []byte
			content, err = os.ReadFile(p)
			if err != nil {
				return "", err
			}
			id := strings.TrimSpace(string(content))
			if id != "" {
				return id, err
			}
			continue
		}
	}
	return "", fmt.Errorf("%s", err)
}
