package service

import (
	"context"
	"encoding/base64"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/util/common"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

type NodeSSHConfig struct {
	Address              string
	Port                 int
	Username             string
	AuthMethod           string
	Password             string
	PrivateKey           string
	PrivateKeyPassphrase string
	HostKeyMode          string
	KnownHosts           string
	HostKeyFingerprint   string
}

type SSHExecutor struct{}

func (SSHExecutor) Run(ctx context.Context, cfg NodeSSHConfig, command string, timeout time.Duration) (string, error) {
	callback, err := BuildSSHHostKeyCallback(cfg)
	if err != nil {
		return "", err
	}
	auth, err := sshAuthMethods(cfg)
	if err != nil {
		return "", err
	}
	if timeout <= 0 {
		timeout = defaultNodePreflightTimeout
	}
	dialCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	addr := net.JoinHostPort(cfg.Address, strconv.Itoa(cfg.Port))
	conn, err := (&net.Dialer{}).DialContext(dialCtx, "tcp", addr)
	if err != nil {
		if dialCtx.Err() != nil {
			return "", context.DeadlineExceeded
		}
		return "", err
	}
	sshConn, chans, reqs, err := ssh.NewClientConn(conn, addr, &ssh.ClientConfig{
		User:            cfg.Username,
		Auth:            auth,
		HostKeyCallback: callback,
		Timeout:         timeout,
	})
	if err != nil {
		_ = conn.Close()
		return "", err
	}
	client := ssh.NewClient(sshConn, chans, reqs)
	defer client.Close()
	session, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()

	type result struct {
		out []byte
		err error
	}
	done := make(chan result, 1)
	go func() {
		out, runErr := session.CombinedOutput(command)
		done <- result{out: out, err: runErr}
	}()
	select {
	case res := <-done:
		return string(res.out), res.err
	case <-dialCtx.Done():
		_ = client.Close()
		return "", context.DeadlineExceeded
	}
}

func sshAuthMethods(cfg NodeSSHConfig) ([]ssh.AuthMethod, error) {
	switch cfg.AuthMethod {
	case "password":
		if cfg.Password == "" {
			return nil, common.NewError("password is required for password auth")
		}
		return []ssh.AuthMethod{ssh.Password(cfg.Password)}, nil
	case "privateKey":
		if cfg.PrivateKey == "" {
			return nil, common.NewError("privateKey is required for privateKey auth")
		}
		var signer ssh.Signer
		var err error
		if cfg.PrivateKeyPassphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase([]byte(cfg.PrivateKey), []byte(cfg.PrivateKeyPassphrase))
		} else {
			signer, err = ssh.ParsePrivateKey([]byte(cfg.PrivateKey))
		}
		if err != nil {
			return nil, err
		}
		return []ssh.AuthMethod{ssh.PublicKeys(signer)}, nil
	default:
		return nil, common.NewError("authMethod must be password or privateKey")
	}
}

func BuildSSHHostKeyCallback(cfg NodeSSHConfig) (ssh.HostKeyCallback, error) {
	mode := cfg.HostKeyMode
	if mode == "" {
		mode = "known_hosts"
	}
	switch mode {
	case "known_hosts":
		callback, err := knownHostsCallback(cfg.KnownHosts)
		if err != nil {
			return nil, err
		}
		return callback, nil
	case "pin":
		pin, err := ParseSSHFingerprint(cfg.HostKeyFingerprint)
		if err != nil {
			return nil, err
		}
		return func(_ string, _ net.Addr, key ssh.PublicKey) error {
			if ssh.FingerprintSHA256(key) != pin {
				return common.NewError("ssh host key fingerprint mismatch")
			}
			return nil
		}, nil
	case "insecure":
		return ssh.InsecureIgnoreHostKey(), nil
	default:
		return nil, common.NewError("hostKeyMode must be known_hosts, pin, or insecure")
	}
}

func ParseSSHFingerprint(fp string) (string, error) {
	fp = strings.TrimSpace(fp)
	if fp == "" {
		return "", common.NewError("hostKeyFingerprint is required for pin host key mode")
	}
	if !strings.HasPrefix(fp, "SHA256:") {
		return "", common.NewError("hostKeyFingerprint must be an OpenSSH SHA256 fingerprint")
	}
	raw := strings.TrimPrefix(fp, "SHA256:")
	if raw == "" {
		return "", common.NewError("hostKeyFingerprint is required for pin host key mode")
	}
	if _, err := base64.RawStdEncoding.DecodeString(raw); err != nil {
		return "", common.NewError("hostKeyFingerprint must be a valid SHA256 fingerprint")
	}
	return fp, nil
}

func knownHostsCallback(data string) (ssh.HostKeyCallback, error) {
	data = strings.TrimSpace(data)
	if data == "" {
		return nil, common.NewError("knownHosts is required for known_hosts host key mode")
	}
	tmp, err := os.CreateTemp("", "xui-known-hosts-*")
	if err != nil {
		return nil, err
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }()
	if _, err := tmp.WriteString(data + "\n"); err != nil {
		_ = tmp.Close()
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		return nil, err
	}
	callback, err := knownhosts.New(name)
	if err != nil {
		return nil, err
	}
	return callback, nil
}
