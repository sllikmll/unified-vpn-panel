package mieru

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestClientJSONIsStandaloneAndUsesRemoteEndpoint(t *testing.T) {
	content, err := ClientJSON(ClientExport{
		ProfileName: "Mieru amstnew",
		UserName:    "pavel-production",
		Password:    "secret",
		Endpoints: []Endpoint{{
			Host:        "3xamstnew.dogonin.ru",
			PortBinding: []PortBinding{{Port: 32002, Protocol: TransportTCP}},
		}},
		MTU: 1280,
	})
	if err != nil {
		t.Fatalf("ClientJSON: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(content, &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if got["activeProfile"] != "Mieru amstnew" || got["socks5Port"] != float64(DefaultClientSOCKS5Port) {
		t.Fatalf("standalone fields = %#v", got)
	}
	text := string(content)
	for _, want := range []string{"3xamstnew.dogonin.ru", "pavel-production", `"port": 32002`, `"protocol": "TCP"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("client JSON missing %q: %s", want, text)
		}
	}
}

func TestClientJSONRejectsIPv6Endpoint(t *testing.T) {
	_, err := ClientJSON(ClientExport{ProfileName: "p", UserName: "u", Password: "p", Endpoints: []Endpoint{{Host: "2001:db8::1", PortBinding: []PortBinding{{Port: 1, Protocol: TransportTCP}}}}})
	if err == nil {
		t.Fatal("ClientJSON accepted IPv6 endpoint")
	}
}
