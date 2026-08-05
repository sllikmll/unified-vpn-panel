package mieru

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

type Runner interface {
	Run(ctx context.Context, name string, args ...string) (RunnerResult, error)
}

type RunnerResult struct {
	Output string
}

type FileSystem interface {
	ReadFile(path string) ([]byte, error)
	WriteFile(path string, data []byte, perm uint32) error
	Rename(oldPath, newPath string) error
	Remove(path string) error
	Exists(path string) bool
	TempFile(dir, pattern string) (string, error)
}

type Runtime struct {
	Runner     Runner
	FS         FileSystem
	ConfigPath string
}

type TrustedConfigPath string

type InstallArtifact struct {
	Version     string `json:"version"`
	OS          string `json:"os"`
	Arch        string `json:"arch"`
	URL         string `json:"url"`
	SHA256      string `json:"sha256"`
	BinaryName  string `json:"binaryName"`
	Destination string `json:"destination"`
}

type InstallPlan struct {
	Artifacts []InstallArtifact `json:"artifacts"`
}

func NewRuntime(runner Runner, fs FileSystem) Runtime {
	return Runtime{Runner: runner, FS: fs, ConfigPath: DefaultConfigPath}
}

func NewProductionRuntime(runner Runner, fs FileSystem, configPath string) (Runtime, error) {
	if configPath == "" {
		configPath = DefaultConfigPath
	}
	if configPath != DefaultConfigPath {
		return Runtime{}, fmt.Errorf("mieru config path must be %q", DefaultConfigPath)
	}
	return Runtime{Runner: runner, FS: fs, ConfigPath: configPath}, nil
}

func NewTrustedLocalRuntime(runner Runner, fs FileSystem, configPath TrustedConfigPath) Runtime {
	path := string(configPath)
	if path == "" {
		path = DefaultConfigPath
	}
	return Runtime{Runner: runner, FS: fs, ConfigPath: path}
}

func (r Runtime) Detect(ctx context.Context) (Status, error) {
	if r.Runner == nil {
		return MapStatus("", false), nil
	}
	result, err := r.Runner.Run(ctx, BinaryName, "version")
	if errors.Is(err, ErrMissingBinary) {
		return MapStatus("", false), nil
	}
	if err != nil {
		return Status{State: StatusError}, sanitizeCommandError(err)
	}
	status := MapStatus("STOPPED", true)
	status.Version = parseVersion(result.Output)
	return status, nil
}

func NewInstallPlan(artifacts []InstallArtifact) (InstallPlan, error) {
	if len(artifacts) == 0 {
		return InstallPlan{}, fmt.Errorf("install artifacts are empty")
	}
	for _, artifact := range artifacts {
		if artifact.Version == "" || artifact.OS == "" || artifact.Arch == "" {
			return InstallPlan{}, fmt.Errorf("install artifact identity is incomplete")
		}
		if artifact.BinaryName != BinaryName {
			return InstallPlan{}, fmt.Errorf("install artifact binary %q is not allowed", artifact.BinaryName)
		}
		if artifact.Destination == "" || filepath.Base(artifact.Destination) != BinaryName {
			return InstallPlan{}, fmt.Errorf("install destination must end with %q", BinaryName)
		}
		if !strings.HasPrefix(artifact.URL, "https://"+DefaultDownloadHost+"/enfein/mieru/") {
			return InstallPlan{}, fmt.Errorf("install URL host or repository is not allowlisted")
		}
		sum, err := hex.DecodeString(artifact.SHA256)
		if err != nil || len(sum) != sha256.Size {
			return InstallPlan{}, fmt.Errorf("install SHA256 must be a hex-encoded SHA-256 digest")
		}
	}
	return InstallPlan{Artifacts: append([]InstallArtifact(nil), artifacts...)}, nil
}

func (r Runtime) ApplyInstallPlan(context.Context, InstallPlan) error {
	return ErrUnsupportedOperation
}

func (r Runtime) Uninstall(context.Context) error {
	return ErrUnsupportedOperation
}

func (r Runtime) Start(ctx context.Context) error {
	_, err := r.run(ctx, "start")
	return sanitizeCommandError(err)
}

func (r Runtime) Stop(ctx context.Context) error {
	_, err := r.run(ctx, "stop")
	return sanitizeCommandError(err)
}

func (r Runtime) Restart(ctx context.Context) error {
	if err := r.Stop(ctx); err != nil {
		return err
	}
	return r.Start(ctx)
}

func (r Runtime) Observe(ctx context.Context) (Status, error) {
	result, err := r.run(ctx, "status")
	if errors.Is(err, ErrMissingBinary) {
		return MapStatus("", false), nil
	}
	if err != nil {
		return Status{State: StatusError, Installed: true}, sanitizeCommandError(err)
	}
	return MapStatus(result.Output, true), nil
}

func (r Runtime) Delete(ctx context.Context) error {
	if r.FS == nil {
		return fmt.Errorf("filesystem is not configured")
	}
	configDelete := r.ConfigPath + ".delete"
	backupPath := r.ConfigPath + ".bak"
	backupDelete := backupPath + ".delete"
	hadConfig := r.FS.Exists(r.ConfigPath)
	hadBackup := r.FS.Exists(backupPath)
	if hadConfig {
		_ = r.FS.Remove(configDelete)
		if err := r.FS.Rename(r.ConfigPath, configDelete); err != nil {
			return err
		}
	}
	if hadBackup {
		_ = r.FS.Remove(backupDelete)
		if err := r.FS.Rename(backupPath, backupDelete); err != nil {
			if hadConfig {
				_ = r.FS.Rename(configDelete, r.ConfigPath)
			}
			return err
		}
	}
	if _, err := r.run(ctx, "stop"); err != nil {
		r.restoreDeletedState(configDelete, backupDelete, hadConfig, hadBackup)
		return sanitizeCommandError(err)
	}
	var errs []error
	if hadConfig {
		errs = append(errs, r.FS.Remove(configDelete))
	}
	if hadBackup {
		errs = append(errs, r.FS.Remove(backupDelete))
	}
	if err := errors.Join(errs...); err != nil {
		r.restoreDeletedState(configDelete, backupDelete, hadConfig, hadBackup)
		return err
	}
	return nil
}

func (r Runtime) ApplyConfig(ctx context.Context, c ServerConfig) error {
	if err := c.ValidateFull(); err != nil {
		return err
	}
	body, err := CanonicalJSON(c)
	if err != nil {
		return err
	}
	return r.atomicApply(ctx, body)
}

func (r Runtime) ApplyUser(ctx context.Context, user User) error {
	if err := user.Validate(); err != nil {
		return err
	}
	current, err := r.loadConfig()
	if err != nil {
		return err
	}
	next, err := MergeUsers(current, user)
	if err != nil {
		return err
	}
	return r.ApplyConfig(ctx, next)
}

func (r Runtime) DeleteUser(ctx context.Context, name string) error {
	if name == "" {
		return fmt.Errorf("user name is not set")
	}
	current, err := r.loadConfig()
	if err != nil {
		return err
	}
	return r.ApplyConfig(ctx, DeleteUsers(current, name))
}

func (r Runtime) Traffic(context.Context) (TrafficCounters, error) {
	return TrafficCounters{Supported: false}, ErrUnsupportedTraffic
}

func (r Runtime) run(ctx context.Context, args ...string) (RunnerResult, error) {
	if r.Runner == nil {
		return RunnerResult{}, ErrMissingBinary
	}
	for _, arg := range args {
		if strings.ContainsAny(arg, "\x00\r\n") {
			return RunnerResult{}, fmt.Errorf("unsafe argv")
		}
	}
	return r.Runner.Run(ctx, BinaryName, args...)
}

func (r Runtime) loadConfig() (ServerConfig, error) {
	if r.FS == nil {
		return ServerConfig{}, fmt.Errorf("filesystem is not configured")
	}
	body, err := r.FS.ReadFile(r.ConfigPath)
	if err != nil {
		return ServerConfig{}, err
	}
	var c ServerConfig
	if err := jsonUnmarshal(body, &c); err != nil {
		return ServerConfig{}, err
	}
	return c, nil
}

func (r Runtime) atomicApply(ctx context.Context, body []byte) error {
	if r.FS == nil {
		return fmt.Errorf("filesystem is not configured")
	}
	dir := filepath.Dir(r.ConfigPath)
	tmp, err := r.FS.TempFile(dir, "server-*.json")
	if err != nil {
		return err
	}
	if err := r.FS.WriteFile(tmp, body, 0o600); err != nil {
		_ = r.FS.Remove(tmp)
		return err
	}
	backup := r.ConfigPath + ".bak"
	hadOriginal := r.FS.Exists(r.ConfigPath)
	if hadOriginal {
		if err := r.FS.Rename(r.ConfigPath, backup); err != nil {
			_ = r.FS.Remove(tmp)
			return err
		}
	}
	if err := r.FS.Rename(tmp, r.ConfigPath); err != nil {
		if hadOriginal {
			_ = r.FS.Rename(backup, r.ConfigPath)
		}
		_ = r.FS.Remove(tmp)
		return err
	}
	if _, err := r.run(ctx, "apply", "config", r.ConfigPath); err != nil {
		rollbackErr := r.rollback(ctx, backup, hadOriginal)
		return errors.Join(sanitizeCommandError(err), rollbackErr)
	}
	if err := r.restartAndVerify(ctx); err != nil {
		rollbackErr := r.rollback(ctx, backup, hadOriginal)
		return errors.Join(sanitizeCommandError(err), rollbackErr)
	}
	if hadOriginal {
		_ = r.FS.Remove(backup)
	}
	return nil
}

func (r Runtime) restartAndVerify(ctx context.Context) error {
	status, err := r.Observe(ctx)
	if err != nil {
		return err
	}
	if status.State == StatusRunning || status.State == StatusStarting {
		if _, err := r.run(ctx, "stop"); err != nil {
			return err
		}
	}
	if _, err := r.run(ctx, "start"); err != nil {
		return err
	}
	if _, err := r.run(ctx, "describe", "config"); err != nil {
		return err
	}
	return nil
}

func (r Runtime) rollback(ctx context.Context, backup string, hadOriginal bool) error {
	var errs []error
	errs = append(errs, r.FS.Remove(r.ConfigPath))
	if hadOriginal {
		if err := r.FS.Rename(backup, r.ConfigPath); err != nil {
			errs = append(errs, err)
			return sanitizeRollbackErrors(errs...)
		}
		if _, err := r.run(ctx, "apply", "config", r.ConfigPath); err != nil {
			errs = append(errs, sanitizeCommandError(err))
		}
		if err := r.restartAndVerify(ctx); err != nil {
			errs = append(errs, sanitizeCommandError(err))
		}
	} else {
		if _, err := r.run(ctx, "stop"); err != nil {
			errs = append(errs, sanitizeCommandError(err))
		}
	}
	return sanitizeRollbackErrors(errs...)
}

func (r Runtime) restoreDeletedState(configDelete, backupDelete string, hadConfig, hadBackup bool) {
	if hadConfig {
		_ = r.FS.Rename(configDelete, r.ConfigPath)
	}
	if hadBackup {
		_ = r.FS.Rename(backupDelete, r.ConfigPath+".bak")
	}
}

func sanitizeCommandError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrMissingBinary) {
		return ErrMissingBinary
	}
	return ErrCommandFailed
}

func sanitizeRollbackErrors(errs ...error) error {
	err := errors.Join(errs...)
	if err == nil {
		return nil
	}
	return fmt.Errorf("mieru rollback failed: %w", sanitizeCommandError(err))
}

func parseVersion(raw string) string {
	fields := strings.Fields(raw)
	for _, field := range fields {
		if strings.HasPrefix(field, "v") || len(field) > 0 && field[0] >= '0' && field[0] <= '9' {
			return strings.Trim(field, `"`)
		}
	}
	return ""
}
