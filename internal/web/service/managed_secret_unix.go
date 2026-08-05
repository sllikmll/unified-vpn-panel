//go:build unix

package service

import (
	"os"
	"syscall"
)

func validateManagedSecretKeyFileSecurity(st os.FileInfo) error {
	if st.Mode().Perm()&^0o600 != 0 {
		return ErrManagedSecretUnavailable
	}
	if sys, ok := st.Sys().(*syscall.Stat_t); ok && sys.Uid != uint32(os.Getuid()) {
		return ErrManagedSecretUnavailable
	}
	return nil
}
