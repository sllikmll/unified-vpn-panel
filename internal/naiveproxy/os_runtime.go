package naiveproxy

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

type OSRunner struct{}

func NewOSRunner() OSRunner {
	return OSRunner{}
}

func (OSRunner) Run(ctx context.Context, cmd Command) error {
	if !isAllowedCommand(cmd) {
		return errors.New("naiveproxy runtime command is not allowlisted")
	}
	if dockerContainerExists(ctx) {
		return runDockerCommand(ctx, cmd)
	}
	c := exec.CommandContext(ctx, cmd.Name, cmd.Argv...)
	if err := c.Run(); err != nil {
		return errors.New("naiveproxy runtime command failed")
	}
	return nil
}

func (OSRunner) Observe(ctx context.Context, service string) (Observation, error) {
	if service != FixedServiceName {
		return Observation{State: BackendNotFound}, nil
	}
	obs := Observation{
		ServiceName: FixedServiceName,
		Executable:  FixedExecutableName,
		CheckedAt:   time.Now().UTC(),
	}
	if dockerContainerExists(ctx) {
		obs.Executable = "docker:" + DockerContainerName
		out, err := exec.CommandContext(ctx, "docker", "container", "inspect", DockerContainerName, "--format", "{{.State.Status}}").Output()
		if err != nil {
			obs.State = BackendUnknown
			return obs, errors.New("naiveproxy docker runtime observation failed")
		}
		switch strings.TrimSpace(string(out)) {
		case "running":
			obs.State = BackendActive
		case "restarting":
			obs.State = BackendActivating
		case "exited", "created", "paused":
			obs.State = BackendInactive
		case "dead", "removing":
			obs.State = BackendFailed
		default:
			obs.State = BackendUnknown
		}
		return obs, nil
	}
	if _, err := exec.LookPath(FixedExecutableName); err != nil {
		obs.State = BackendNotFound
		return obs, nil
	}
	cmd := exec.CommandContext(ctx, "systemctl", "is-active", FixedServiceName+".service")
	out, err := cmd.Output()
	state := strings.TrimSpace(string(out))
	switch state {
	case "active":
		obs.State = BackendActive
	case "activating":
		obs.State = BackendActivating
	case "reloading":
		obs.State = BackendReloading
	case "failed":
		obs.State = BackendFailed
	case "inactive", "deactivating":
		obs.State = BackendInactive
	default:
		if err != nil {
			obs.State = BackendUnknown
		} else {
			obs.State = BackendInactive
		}
	}
	return obs, nil
}

func dockerContainerExists(ctx context.Context) bool {
	return exec.CommandContext(ctx, "docker", "container", "inspect", DockerContainerName).Run() == nil
}

func runDockerCommand(ctx context.Context, cmd Command) error {
	var argv []string
	switch cmd.Name {
	case FixedExecutableName:
		if err := exec.CommandContext(ctx, "docker", "start", DockerContainerName).Run(); err != nil {
			return errors.New("naiveproxy docker runtime start failed")
		}
		argv = append([]string{"exec", DockerContainerName, "caddy"}, cmd.Argv...)
	case "systemctl":
		argv = []string{cmd.Argv[0], DockerContainerName}
	default:
		return errors.New("naiveproxy docker command is not allowlisted")
	}
	if err := exec.CommandContext(ctx, "docker", argv...).Run(); err != nil {
		return errors.New("naiveproxy docker runtime command failed")
	}
	return nil
}

type HTTPSHealthVerifier struct {
	Timeout time.Duration
}

func NewHTTPSHealthVerifier(timeout time.Duration) HTTPSHealthVerifier {
	return HTTPSHealthVerifier{Timeout: timeout}
}

func (v HTTPSHealthVerifier) Verify(ctx context.Context, endpoint Endpoint) error {
	if err := endpoint.Validate(); err != nil {
		return err
	}
	timeout := v.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	dialer := &net.Dialer{Timeout: timeout}
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			ServerName: canonicalDomain(endpoint.Domain),
			MinVersion: tls.VersionTLS12,
		},
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, net.JoinHostPort(endpoint.ListenIP, fmt.Sprint(endpoint.Port)))
		},
		TLSHandshakeTimeout:   timeout,
		ResponseHeaderTimeout: timeout,
		ForceAttemptHTTP2:     true,
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: timeout}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+canonicalDomain(endpoint.Domain)+"/", nil)
	if err != nil {
		return errors.New("build naiveproxy health request failed")
	}
	resp, err := client.Do(req)
	if err != nil {
		return errors.New("naiveproxy https health check failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 500 {
		return errors.New("naiveproxy https health check returned unhealthy status")
	}
	return nil
}

func isAllowedCommand(cmd Command) bool {
	switch cmd.Name {
	case FixedExecutableName:
		return equalStrings(cmd.Argv, []string{"validate", "--config", FixedConfigPath, "--adapter", "caddyfile"}) ||
			equalStrings(cmd.Argv, []string{"reload", "--config", FixedConfigPath, "--adapter", "caddyfile"})
	case "systemctl":
		return equalStrings(cmd.Argv, []string{"start", FixedServiceName + ".service"}) ||
			equalStrings(cmd.Argv, []string{"stop", FixedServiceName + ".service"}) ||
			equalStrings(cmd.Argv, []string{"restart", FixedServiceName + ".service"})
	default:
		return false
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
