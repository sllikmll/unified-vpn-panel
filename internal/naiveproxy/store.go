package naiveproxy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

var ErrConfigNotFound = errors.New("naiveproxy config state not found")

type OSConfigStore struct {
	configPath string
	statePath  string
}

func NewOSConfigStore() *OSConfigStore {
	return &OSConfigStore{configPath: FixedConfigPath, statePath: FixedStatePath}
}

func newOSConfigStoreForTest(configPath, statePath string) *OSConfigStore {
	return &OSConfigStore{configPath: configPath, statePath: statePath}
}

func (s *OSConfigStore) Load(context.Context) (Server, error) {
	data, err := os.ReadFile(s.statePath)
	if errors.Is(err, os.ErrNotExist) {
		return Server{}, ErrConfigNotFound
	}
	if err != nil {
		return Server{}, fmt.Errorf("read naiveproxy typed state: %w", err)
	}
	var server Server
	if err := json.Unmarshal(data, &server); err != nil {
		return Server{}, errors.New("read naiveproxy typed state: invalid persisted state")
	}
	if err := server.Validate(); err != nil {
		return Server{}, fmt.Errorf("read naiveproxy typed state: %w", err)
	}
	return server, nil
}

func (s *OSConfigStore) AtomicWrite(ctx context.Context, server Server, rendered []byte) (Backup, error) {
	if err := server.Validate(); err != nil {
		return Backup{}, err
	}
	backup := Backup{ConfigPath: s.configPath, StatePath: s.statePath}
	old, err := s.Load(ctx)
	if err == nil {
		backup.OldServer = old
		backup.HadPrevious = true
	} else if !errors.Is(err, ErrConfigNotFound) {
		return Backup{}, err
	}
	if backup.HadPrevious {
		backup.OldConfig, err = os.ReadFile(s.configPath)
		if err != nil {
			return Backup{}, fmt.Errorf("backup naiveproxy config: %w", err)
		}
		backup.OldState, err = os.ReadFile(s.statePath)
		if err != nil {
			return Backup{}, fmt.Errorf("backup naiveproxy typed state: %w", err)
		}
	}

	state, err := json.MarshalIndent(server, "", "  ")
	if err != nil {
		return Backup{}, err
	}
	state = append(state, '\n')
	if err := atomicWrite0600(s.configPath, rendered); err != nil {
		return Backup{}, fmt.Errorf("write naiveproxy config: %w", err)
	}
	if err := ensureRuntimeConfigOwner(s.configPath); err != nil {
		return Backup{}, fmt.Errorf("secure naiveproxy config ownership: %w", err)
	}
	if err := atomicWrite0600(s.statePath, state); err != nil {
		_ = s.Rollback(backup)
		return Backup{}, fmt.Errorf("write naiveproxy typed state: %w", err)
	}
	return backup, nil
}

func (s *OSConfigStore) Commit(Backup) error {
	return nil
}

func (s *OSConfigStore) Rollback(backup Backup) error {
	if !backup.HadPrevious {
		if err := removeIfExists(s.configPath); err != nil {
			return err
		}
		return removeIfExists(s.statePath)
	}
	if err := atomicWrite0600(s.configPath, backup.OldConfig); err != nil {
		return err
	}
	if err := ensureRuntimeConfigOwner(s.configPath); err != nil {
		return err
	}
	return atomicWrite0600(s.statePath, backup.OldState)
}

func ensureRuntimeConfigOwner(path string) error {
	if filepath.Clean(path) != filepath.Clean(FixedConfigPath) {
		return nil
	}
	return os.Chown(path, 10001, 10001)
}

func (s *OSConfigStore) Delete(context.Context) error {
	if err := removeIfExists(s.configPath); err != nil {
		return err
	}
	return removeIfExists(s.statePath)
}

func atomicWrite0600(path string, contents []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		_ = os.Remove(tmpName)
	}()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(contents); err != nil {
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

func removeIfExists(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
