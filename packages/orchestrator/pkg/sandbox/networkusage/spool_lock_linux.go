package networkusage

import (
	"errors"
	"golang.org/x/sys/unix"
	"os"
)

func lockSpool(path string) (*os.File, error) {
	f, err := openSpoolControl(path, os.O_CREATE|os.O_RDWR)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil || info.Size() != 0 {
		f.Close()
		return nil, errors.Join(errors.New("invalid spool lock"), err)
	}
	if err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}
func openSpoolControl(path string, flags int) (*os.File, error) {
	fd, err := unix.Open(path, flags|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0600)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		f.Close()
		return nil, errors.Join(errors.New("invalid spool control file"), err)
	}
	return f, nil
}
func unlockSpool(f *os.File) error {
	return errors.Join(unix.Flock(int(f.Fd()), unix.LOCK_UN), f.Close())
}
