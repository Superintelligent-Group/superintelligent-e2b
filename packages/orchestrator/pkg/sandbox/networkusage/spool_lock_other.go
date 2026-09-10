//go:build !linux

package networkusage

import (
	"errors"
	"os"
)

func lockSpool(string) (*os.File, error) {
	return nil, errors.New("network spool requires Linux host locking")
}
func unlockSpool(f *os.File) error { return f.Close() }
func openSpoolControl(path string, flags int) (*os.File, error) {
	return nil, errors.New("network spool requires Linux")
}
