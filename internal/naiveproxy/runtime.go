package naiveproxy

import (
	"context"
	"errors"
	"fmt"
)

type Command struct {
	Name string
	Argv []string
}

const (
	FixedConfigDir      = "/etc/caddy-naive"
	FixedConfigPath     = FixedConfigDir + "/Caddyfile.naiveproxy"
	FixedStatePath      = "/etc/caddy-naive/server.json"
	DockerContainerName = "unified-vpn-naive-runtime"
	DockerDataVolume    = "unified-vpn-naive-data"
	DockerConfigVolume  = "unified-vpn-naive-config"
)

type Backend interface {
	Detect(ctx context.Context) (Observation, error)
	Apply(ctx context.Context, server Server) error
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Restart(ctx context.Context) error
	Observe(ctx context.Context) (Observation, error)
	Delete(ctx context.Context) error
	ApplyUser(ctx context.Context, user User) error
	DeleteUser(ctx context.Context, userID string) error
}

type Runner interface {
	Run(ctx context.Context, cmd Command) error
	Observe(ctx context.Context, service string) (Observation, error)
}

type ConfigStore interface {
	Load(ctx context.Context) (Server, error)
	AtomicWrite(ctx context.Context, server Server, rendered []byte) (backup Backup, err error)
	Commit(backup Backup) error
	Rollback(backup Backup) error
	Delete(ctx context.Context) error
}

type Backup struct {
	ConfigPath  string
	StatePath   string
	OldConfig   []byte
	OldState    []byte
	OldServer   Server
	HadPrevious bool
}

type HealthVerifier interface {
	Verify(ctx context.Context, endpoint Endpoint) error
}

type Runtime struct {
	Runner     Runner
	Store      ConfigStore
	Health     HealthVerifier
	ConfigPath string
}

func NewRuntime(r Runner, s ConfigStore, h HealthVerifier) (*Runtime, error) {
	return newRuntimeWithConfigPath(r, s, h, FixedConfigPath)
}

func newRuntimeWithConfigPath(r Runner, s ConfigStore, h HealthVerifier, configPath string) (*Runtime, error) {
	if r == nil || s == nil || h == nil {
		return nil, errors.New("runner, store, and health verifier are required")
	}
	if configPath != FixedConfigPath {
		return nil, errors.New("config path must be the fixed package-owned caddy-naive path")
	}
	return &Runtime{Runner: r, Store: s, Health: h, ConfigPath: configPath}, nil
}

func (r *Runtime) Detect(ctx context.Context) (Observation, error) {
	obs, err := r.Runner.Observe(ctx, FixedServiceName)
	obs.ServiceName = FixedServiceName
	obs.Executable = FixedExecutableName
	return obs, err
}

func (r *Runtime) Apply(ctx context.Context, s Server) error {
	rendered, err := GenerateCaddyfile(s)
	if err != nil {
		return err
	}
	backup, err := r.Store.AtomicWrite(ctx, s, []byte(rendered))
	if err != nil {
		return err
	}
	if err := r.Runner.Run(ctx, validateCommand(r.ConfigPath)); err != nil {
		if restoreErr := r.restore(ctx, backup); restoreErr != nil {
			return fmt.Errorf("validate naiveproxy config: %w; rollback failed", err)
		}
		return fmt.Errorf("validate naiveproxy config: %w", err)
	}
	if err := r.Runner.Run(ctx, reloadCommand(r.ConfigPath)); err != nil {
		if restoreErr := r.restore(ctx, backup); restoreErr != nil {
			return fmt.Errorf("reload naiveproxy service: %w; rollback failed", err)
		}
		return fmt.Errorf("reload naiveproxy service: %w", err)
	}
	if err := r.Health.Verify(ctx, s.Endpoint); err != nil {
		if restoreErr := r.restore(ctx, backup); restoreErr != nil {
			return fmt.Errorf("verify naiveproxy https health: %w; rollback failed", err)
		}
		return fmt.Errorf("verify naiveproxy https health: %w", err)
	}
	if err := r.Store.Commit(backup); err != nil {
		return err
	}
	return nil
}

func (r *Runtime) restore(ctx context.Context, backup Backup) error {
	if err := r.Store.Rollback(backup); err != nil {
		return err
	}
	if !backup.HadPrevious {
		return r.Runner.Run(ctx, serviceCommand("stop"))
	}
	if err := r.Runner.Run(ctx, reloadCommand(r.ConfigPath)); err != nil {
		return err
	}
	return r.Health.Verify(ctx, backup.OldServer.Endpoint)
}

func (r *Runtime) Start(ctx context.Context) error {
	return r.Runner.Run(ctx, serviceCommand("start"))
}

func (r *Runtime) Stop(ctx context.Context) error {
	return r.Runner.Run(ctx, serviceCommand("stop"))
}

func (r *Runtime) Restart(ctx context.Context) error {
	server, err := r.Store.Load(ctx)
	if err != nil {
		return err
	}
	if err := r.Runner.Run(ctx, serviceCommand("restart")); err != nil {
		return err
	}
	if err := r.Health.Verify(ctx, server.Endpoint); err != nil {
		return fmt.Errorf("verify naiveproxy https health: %w", err)
	}
	return nil
}

func (r *Runtime) Observe(ctx context.Context) (Observation, error) {
	return r.Detect(ctx)
}

func (r *Runtime) Delete(ctx context.Context) error {
	if err := r.Stop(ctx); err != nil {
		return err
	}
	return r.Store.Delete(ctx)
}

func (r *Runtime) ApplyUser(ctx context.Context, user User) error {
	server, err := r.Store.Load(ctx)
	if err != nil {
		return err
	}
	if err := server.UpsertUser(user); err != nil {
		return err
	}
	return r.Apply(ctx, server)
}

func (r *Runtime) DeleteUser(ctx context.Context, userID string) error {
	server, err := r.Store.Load(ctx)
	if err != nil {
		return err
	}
	if err := server.DeleteUser(userID); err != nil {
		return err
	}
	return r.Apply(ctx, server)
}

func validateCommand(configPath string) Command {
	return Command{Name: FixedExecutableName, Argv: []string{"validate", "--config", configPath, "--adapter", "caddyfile"}}
}

func reloadCommand(configPath string) Command {
	return Command{Name: FixedExecutableName, Argv: []string{"reload", "--config", configPath, "--adapter", "caddyfile"}}
}

func serviceCommand(action string) Command {
	return Command{Name: "systemctl", Argv: []string{action, FixedServiceName + ".service"}}
}
