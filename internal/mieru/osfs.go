package mieru

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

type OSRunner struct{}

func (OSRunner) Run(ctx context.Context, name string, args ...string) (RunnerResult, error) {
	if err := validateOSCommand(name, args); err != nil {
		return RunnerResult{}, err
	}
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist) {
			return RunnerResult{}, ErrMissingBinary
		}
		return RunnerResult{}, ErrCommandFailed
	}
	return RunnerResult{Output: string(out)}, nil
}

type OSFileSystem struct{}

func (OSFileSystem) ReadFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

func (OSFileSystem) WriteFile(path string, data []byte, perm uint32) error {
	if os.FileMode(perm) != 0o600 {
		return fmt.Errorf("mieru config file mode must be 0600")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".mieru-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func (OSFileSystem) Rename(oldPath, newPath string) error {
	return os.Rename(oldPath, newPath)
}

func (OSFileSystem) Remove(path string) error {
	return os.Remove(path)
}

func (OSFileSystem) Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func (OSFileSystem) TempFile(dir, pattern string) (string, error) {
	f, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return "", err
	}
	name := f.Name()
	if err := f.Close(); err != nil {
		_ = os.Remove(name)
		return "", err
	}
	return name, nil
}

func validateOSCommand(name string, args []string) error {
	if name != BinaryName {
		return fmt.Errorf("mieru runner binary must be %q", BinaryName)
	}
	for _, arg := range args {
		if arg == "" || containsUnsafeArgByte(arg) {
			return fmt.Errorf("unsafe argv")
		}
	}
	switch len(args) {
	case 1:
		switch args[0] {
		case "version", "status", "start", "stop", "reload":
			return nil
		}
	case 2:
		if args[0] == "describe" && args[1] == "config" {
			return nil
		}
	case 3:
		if args[0] == "apply" && args[1] == "config" && args[2] == DefaultConfigPath {
			return nil
		}
	}
	return fmt.Errorf("mieru command is not allowlisted")
}

func containsUnsafeArgByte(arg string) bool {
	for _, r := range arg {
		if r == '\x00' || r == '\r' || r == '\n' {
			return true
		}
	}
	return false
}
