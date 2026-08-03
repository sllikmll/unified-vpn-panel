package service

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/util/common"
	"github.com/mhsanaei/3x-ui/v3/internal/util/netsafe"
)

const defaultNodePreflightTimeout = 12 * time.Second

type NodePreflightRequest struct {
	Address              string  `json:"address" form:"address" validate:"required" example:"node1.example.com"`
	Port                 int     `json:"port" form:"port" validate:"gte=1,lte=65535" example:"22"`
	Username             string  `json:"username" form:"username" validate:"required" example:"root"`
	AuthMethod           string  `json:"authMethod" form:"authMethod" validate:"required,oneof=password privateKey" example:"privateKey"`
	Password             *string `json:"password,omitempty" form:"password" example:"<write-only password>"`
	PrivateKey           *string `json:"privateKey,omitempty" form:"privateKey" example:"-----BEGIN OPENSSH PRIVATE KEY-----"`
	PrivateKeyPassphrase *string `json:"privateKeyPassphrase,omitempty" form:"privateKeyPassphrase" example:"<write-only passphrase>"`
	HostKeyMode          string  `json:"hostKeyMode" form:"hostKeyMode" validate:"omitempty,oneof=known_hosts pin insecure" example:"known_hosts"`
	KnownHosts           string  `json:"knownHosts" form:"knownHosts" example:"node1.example.com ssh-ed25519 AAAA..."`
	HostKeyFingerprint   string  `json:"hostKeyFingerprint" form:"hostKeyFingerprint" example:"SHA256:abc..."`
	TimeoutSeconds       int     `json:"timeoutSeconds" form:"timeoutSeconds" example:"12"`
}

func (r NodePreflightRequest) MarshalJSON() ([]byte, error) {
	type publicRequest NodePreflightRequest
	out := publicRequest(r)
	out.Password = nil
	out.PrivateKey = nil
	out.PrivateKeyPassphrase = nil
	return json.Marshal(out)
}

func (r *NodePreflightRequest) Validate() error {
	if r == nil {
		return common.NewError("node preflight request is required")
	}
	addr, err := netsafe.NormalizeHost(r.Address)
	if err != nil {
		return common.NewError(err.Error())
	}
	r.Address = addr
	r.Username = strings.TrimSpace(r.Username)
	if r.Port <= 0 {
		r.Port = 22
	}
	if r.Port > 65535 {
		return common.NewError("ssh port must be 1-65535")
	}
	if r.Username == "" {
		return common.NewError("ssh username is required")
	}
	r.AuthMethod = strings.TrimSpace(r.AuthMethod)
	if r.AuthMethod == "" {
		r.AuthMethod = "privateKey"
	}
	password := trimSecretPtr(r.Password)
	privateKey := trimSecretPtr(r.PrivateKey)
	passphrase := trimSecretPtr(r.PrivateKeyPassphrase)
	r.Password, r.PrivateKey, r.PrivateKeyPassphrase = password, privateKey, passphrase
	if password != nil && privateKey != nil {
		return common.NewError("password and privateKey are mutually exclusive")
	}
	switch r.AuthMethod {
	case "password":
		if password == nil {
			return common.NewError("password is required for password auth")
		}
	case "privateKey":
		if privateKey == nil {
			return common.NewError("privateKey is required for privateKey auth")
		}
	default:
		return common.NewError("authMethod must be password or privateKey")
	}
	if r.HostKeyMode == "" {
		r.HostKeyMode = "known_hosts"
	}
	switch r.HostKeyMode {
	case "known_hosts":
		if strings.TrimSpace(r.KnownHosts) == "" {
			return common.NewError("knownHosts is required for known_hosts host key mode")
		}
	case "pin":
		if _, err := ParseSSHFingerprint(r.HostKeyFingerprint); err != nil {
			return err
		}
	case "insecure":
	default:
		return common.NewError("hostKeyMode must be known_hosts, pin, or insecure")
	}
	if r.TimeoutSeconds <= 0 {
		r.TimeoutSeconds = int(defaultNodePreflightTimeout / time.Second)
	}
	if r.TimeoutSeconds > 60 {
		r.TimeoutSeconds = 60
	}
	return nil
}

func trimSecretPtr(v *string) *string {
	if v == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*v)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

type NodePreflightResult struct {
	OS            string               `json:"os" example:"ubuntu"`
	Arch          string               `json:"arch" example:"amd64"`
	Hostname      string               `json:"hostname" example:"edge-1"`
	Root          bool                 `json:"root" example:"true"`
	Sudo          bool                 `json:"sudo" example:"true"`
	Systemd       bool                 `json:"systemd" example:"true"`
	Docker        bool                 `json:"docker" example:"false"`
	FreeDiskBytes int64                `json:"freeDiskBytes" example:"10737418240"`
	OccupiedPorts []int                `json:"occupiedPorts" example:"[22,80,443]"`
	Errors        []NodePreflightError `json:"errors"`
	Provisioning  NodeProvisioningPlan `json:"provisioning"`
}

type NodePreflightError struct {
	Code    string `json:"code" example:"docker_missing"`
	Message string `json:"message" example:"Docker is not installed"`
}

type NodeProvisioningPlan struct {
	CanInstall bool     `json:"canInstall" example:"true"`
	Warnings   []string `json:"warnings" example:"[\"Docker will be installed\"]"`
}

type nodeSSHRunner interface {
	Run(context.Context, NodeSSHConfig, string, time.Duration) (string, error)
}

func (s *NodeService) Preflight(ctx context.Context, req *NodePreflightRequest) (*NodePreflightResult, error) {
	if err := req.Validate(); err != nil {
		return nil, RedactNodePreflightSecrets(req, err)
	}
	timeout := time.Duration(req.TimeoutSeconds) * time.Second
	exec := s.sshExecutor
	if exec == nil {
		exec = SSHExecutor{}
	}
	cfg := NodeSSHConfigFromPreflight(req)
	out, err := exec.Run(ctx, cfg, nodePreflightScript(), timeout)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			err = common.NewError("ssh preflight timeout")
		}
		return nil, RedactNodePreflightSecrets(req, err)
	}
	result := ParseNodePreflightOutput(out)
	result.Errors = append(result.Errors, result.detectErrors()...)
	result.Provisioning = result.provisioningPlan()
	return result, nil
}

func RedactNodePreflightSecrets(req *NodePreflightRequest, err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	for _, secret := range preflightSecrets(req) {
		msg = strings.ReplaceAll(msg, secret, "[redacted]")
		for _, line := range strings.Split(secret, "\n") {
			line = strings.TrimSpace(line)
			if len(line) >= 3 {
				msg = strings.ReplaceAll(msg, line, "[redacted]")
			}
		}
	}
	return common.NewError(msg)
}

func preflightSecrets(req *NodePreflightRequest) []string {
	if req == nil {
		return nil
	}
	var out []string
	for _, ptr := range []*string{req.Password, req.PrivateKey, req.PrivateKeyPassphrase} {
		if ptr != nil && *ptr != "" {
			out = append(out, *ptr)
		}
	}
	return out
}

func ParseNodePreflightOutput(out string) *NodePreflightResult {
	values := make(map[string]string)
	for _, line := range strings.Split(out, "\n") {
		key, val, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok || !strings.HasPrefix(key, "__XUI_PRE_") {
			continue
		}
		values[strings.TrimPrefix(key, "__XUI_PRE_")] = val
	}
	freeKB, _ := strconv.ParseInt(values["FREE_KB"], 10, 64)
	return &NodePreflightResult{
		OS:            firstNonEmpty(values["OS"], values["ID"]),
		Arch:          normalizeSSHArch(values["ARCH"]),
		Hostname:      values["HOSTNAME"],
		Root:          values["ROOT"] == "1",
		Sudo:          values["SUDO"] == "1",
		Systemd:       values["SYSTEMD"] == "1",
		Docker:        values["DOCKER"] == "1",
		FreeDiskBytes: freeKB * 1024,
		OccupiedPorts: parsePorts(values["PORTS"]),
	}
}

func (r *NodePreflightResult) detectErrors() []NodePreflightError {
	if r == nil {
		return nil
	}
	var errs []NodePreflightError
	if !r.Root && !r.Sudo {
		errs = append(errs, NodePreflightError{Code: "privilege_missing", Message: "SSH user must be root or have passwordless sudo"})
	}
	if !r.Systemd {
		errs = append(errs, NodePreflightError{Code: "systemd_missing", Message: "systemd is required for managed node services"})
	}
	if !r.Docker {
		errs = append(errs, NodePreflightError{Code: "docker_missing", Message: "Docker is not installed"})
	}
	if r.FreeDiskBytes > 0 && r.FreeDiskBytes < 2*1024*1024*1024 {
		errs = append(errs, NodePreflightError{Code: "disk_low", Message: "less than 2 GiB free disk is available"})
	}
	return errs
}

func (r *NodePreflightResult) provisioningPlan() NodeProvisioningPlan {
	if r == nil {
		return NodeProvisioningPlan{}
	}
	plan := NodeProvisioningPlan{CanInstall: true}
	if !r.Root && !r.Sudo {
		plan.CanInstall = false
	}
	if !r.Systemd {
		plan.CanInstall = false
	}
	if !r.Docker {
		plan.Warnings = append(plan.Warnings, "Docker will be installed before node services")
	}
	if r.FreeDiskBytes > 0 && r.FreeDiskBytes < 2*1024*1024*1024 {
		plan.CanInstall = false
	}
	return plan
}

func parsePorts(raw string) []int {
	seen := map[int]struct{}{}
	for _, part := range regexp.MustCompile(`[^0-9]+`).Split(raw, -1) {
		if part == "" {
			continue
		}
		port, err := strconv.Atoi(part)
		if err == nil && port > 0 && port <= 65535 {
			seen[port] = struct{}{}
		}
	}
	ports := make([]int, 0, len(seen))
	for port := range seen {
		ports = append(ports, port)
	}
	sort.Ints(ports)
	return ports
}

func normalizeSSHArch(arch string) string {
	switch strings.ToLower(strings.TrimSpace(arch)) {
	case "x86_64", "amd64":
		return "amd64"
	case "aarch64", "arm64":
		return "arm64"
	default:
		return strings.TrimSpace(arch)
	}
}

func firstNonEmpty(values ...string) string {
	for _, val := range values {
		if strings.TrimSpace(val) != "" {
			return strings.TrimSpace(val)
		}
	}
	return ""
}

func nodePreflightScript() string {
	return `set -eu
os=""
if [ -r /etc/os-release ]; then . /etc/os-release; os="${ID:-}"; fi
ports="$( (ss -H -ltnup 2>/dev/null || netstat -ltnup 2>/dev/null || true) | awk '{print $4}' | sed -E 's/.*:([0-9]+)$/\1/' | grep -E '^[0-9]+$' | sort -n | uniq | paste -sd, - )"
sudo_ok=0
if command -v sudo >/dev/null 2>&1 && sudo -n true >/dev/null 2>&1; then sudo_ok=1; fi
systemd_ok=0
if command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ]; then systemd_ok=1; fi
docker_ok=0
if command -v docker >/dev/null 2>&1; then docker_ok=1; fi
root_ok=0
if [ "$(id -u)" = "0" ]; then root_ok=1; fi
printf '__XUI_PRE_OS=%s\n' "$os"
printf '__XUI_PRE_ARCH=%s\n' "$(uname -m)"
printf '__XUI_PRE_HOSTNAME=%s\n' "$(hostname)"
printf '__XUI_PRE_ROOT=%s\n' "$root_ok"
printf '__XUI_PRE_SUDO=%s\n' "$sudo_ok"
printf '__XUI_PRE_SYSTEMD=%s\n' "$systemd_ok"
printf '__XUI_PRE_DOCKER=%s\n' "$docker_ok"
printf '__XUI_PRE_FREE_KB=%s\n' "$(df -Pk / | awk 'NR==2 {print $4}')"
printf '__XUI_PRE_PORTS=%s\n' "$ports"
`
}

func NodeSSHConfigFromPreflight(req *NodePreflightRequest) NodeSSHConfig {
	cfg := NodeSSHConfig{
		Address:            req.Address,
		Port:               req.Port,
		Username:           req.Username,
		AuthMethod:         req.AuthMethod,
		HostKeyMode:        req.HostKeyMode,
		KnownHosts:         req.KnownHosts,
		HostKeyFingerprint: req.HostKeyFingerprint,
	}
	if req.Password != nil {
		cfg.Password = *req.Password
	}
	if req.PrivateKey != nil {
		cfg.PrivateKey = *req.PrivateKey
	}
	if req.PrivateKeyPassphrase != nil {
		cfg.PrivateKeyPassphrase = *req.PrivateKeyPassphrase
	}
	return cfg
}
