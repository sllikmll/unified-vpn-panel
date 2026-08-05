package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func TestPublicManagedEndpointConfigCoversAllNativeProtocolsWithoutSecrets(t *testing.T) {
	tests := []struct {
		name    string
		kind    model.RuntimeKind
		desired string
		want    string
	}{
		{
			name:    "amneziawg",
			kind:    model.RuntimeAmneziaWG,
			desired: `{"server":{"endpoint":"vpn.example:32001","interfaceName":"awg0","listenPort":32001,"ipv4Address":"10.66.66.1/24","ipv4Pool":"10.66.66.0/24","dns":"1.1.1.1","mtu":1420,"privateKey":"SECRET","obfuscation20":{"jc":4,"jmin":40,"jmax":70}},"clientDefaults":{"allowedIPs":"0.0.0.0/0","persistentKeepalive":25}}`,
			want:    "10.66.66.0/24",
		},
		{
			name:    "mieru",
			kind:    model.RuntimeMieru,
			desired: `{"mtu":1400,"portBindings":[{"port":32002,"protocol":"TCP"}],"users":[{"password":"SECRET"}]}`,
			want:    "32002",
		},
		{
			name:    "naiveproxy",
			kind:    model.RuntimeNaiveProxy,
			desired: `{"endpoint":{"domain":"naive.example","listenIp":"127.0.0.1","port":32003,"acmeEmail":"ops@example.com"},"users":[{"password":"SECRET"}]}`,
			want:    "naive.example",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, err := json.Marshal(publicManagedEndpointConfig(model.ManagedEndpoint{RuntimeKind: tt.kind, DesiredConfig: tt.desired}))
			if err != nil {
				t.Fatal(err)
			}
			text := string(raw)
			if !strings.Contains(text, tt.want) {
				t.Fatalf("public config %s does not contain %q", text, tt.want)
			}
			if strings.Contains(text, "SECRET") || strings.Contains(strings.ToLower(text), "privatekey") {
				t.Fatalf("public config leaked secret material: %s", text)
			}
			if tt.name == "naiveproxy" && !strings.Contains(text, `"listenIP":"127.0.0.1"`) {
				t.Fatalf("NaiveProxy listenIP casing/value was not preserved: %s", text)
			}
		})
	}
}
