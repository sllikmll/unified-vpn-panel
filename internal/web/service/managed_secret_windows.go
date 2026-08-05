//go:build windows

package service

import "os"

func validateManagedSecretKeyFileSecurity(os.FileInfo) error {
	return nil
}
