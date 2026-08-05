package naiveproxy

import "testing"

func TestOSRunnerAllowlistRejectsArbitraryCommands(t *testing.T) {
	allowed := []Command{
		validateCommand(FixedConfigPath),
		reloadCommand(FixedConfigPath),
		serviceCommand("start"),
		serviceCommand("stop"),
		serviceCommand("restart"),
	}
	for _, cmd := range allowed {
		if !isAllowedCommand(cmd) {
			t.Fatalf("expected command to be allowlisted: %#v", cmd)
		}
	}
	rejected := []Command{
		{Name: "sh", Argv: []string{"-c", "caddy-naive reload"}},
		{Name: FixedExecutableName, Argv: []string{"reload", "--config", "/tmp/caddy-naive/Caddyfile", "--adapter", "caddyfile"}},
		{Name: "systemctl", Argv: []string{"restart", "ssh.service"}},
		{Name: FixedExecutableName, Argv: []string{"reload", "--config", FixedConfigPath, "--adapter", "json"}},
	}
	for _, cmd := range rejected {
		if isAllowedCommand(cmd) {
			t.Fatalf("expected command to be rejected: %#v", cmd)
		}
	}
}
