package mieru

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestValidationIPv4OnlyAndUpstreamRanges(t *testing.T) {
	valid := ServerConfig{
		PortBindings: []PortBinding{{PortRange: "443-444", Protocol: TransportTCP}, {Port: 443, Protocol: TransportTCP}, {Port: 5300, Protocol: TransportUDP}},
		Users:        []User{{Name: "alice@example.com", Password: "secret", Quotas: []Quota{{Days: 30, Megabytes: 1024}}}},
		MTU:          1280,
	}
	if err := valid.ValidateFull(); err != nil {
		t.Fatalf("ValidateFull() returned error: %v", err)
	}
	cases := []ServerConfig{
		{Users: valid.Users},
		{PortBindings: []PortBinding{{Port: 0, Protocol: TransportTCP}}},
		{PortBindings: []PortBinding{{Port: 70000, Protocol: TransportTCP}}},
		{PortBindings: []PortBinding{{Port: 443, Protocol: "QUIC"}}},
		{PortBindings: valid.PortBindings, MTU: 1200},
		{PortBindings: valid.PortBindings, Users: []User{{Name: "alice@example.com"}}},
	}
	for _, tc := range cases {
		if err := tc.ValidateFull(); err == nil {
			t.Fatalf("ValidateFull(%+v) got nil error", tc)
		}
	}
	export := ClientExport{
		ProfileName: "default",
		UserName:    "alice@example.com",
		Password:    "secret",
		Endpoints:   []Endpoint{{Host: "2001:db8::1", PortBinding: []PortBinding{{Port: 443, Protocol: TransportTCP}}}},
	}
	if _, err := SimpleLinks(export); err == nil {
		t.Fatal("SimpleLinks() accepted IPv6 endpoint")
	}
}

func TestCanonicalJSONGolden(t *testing.T) {
	config := ServerConfig{
		Users: []User{
			{Name: "zoe@example.com", Password: "zpass"},
			{Name: "alice@example.com", HashedPassword: "abcdef"},
		},
		PortBindings: []PortBinding{
			{Port: 8443, Protocol: TransportUDP},
			{PortRange: "443-444", Protocol: TransportTCP},
			{Port: 443, Protocol: TransportTCP},
		},
		MTU: 1500,
	}
	got, err := CanonicalJSON(config)
	if err != nil {
		t.Fatalf("CanonicalJSON() error: %v", err)
	}
	want := `{
    "portBindings": [
        {
            "port": 443,
            "protocol": "TCP"
        },
        {
            "port": 444,
            "protocol": "TCP"
        },
        {
            "port": 8443,
            "protocol": "UDP"
        }
    ],
    "users": [
        {
            "name": "alice@example.com",
            "hashedPassword": "abcdef"
        },
        {
            "name": "zoe@example.com",
            "password": "zpass"
        }
    ],
    "mtu": 1500
}`
	if string(got) != want {
		t.Fatalf("canonical JSON mismatch\nwant:\n%s\ngot:\n%s", want, string(got))
	}
	var decoded map[string]any
	if err := json.Unmarshal(got, &decoded); err != nil {
		t.Fatalf("golden JSON is invalid: %v", err)
	}
	if strings.Contains(string(got), "listen") || strings.Contains(string(got), "endpoint") {
		t.Fatalf("server config included unsupported listen/endpoint fields: %s", string(got))
	}
}

func TestMultiUserCRUDSemantics(t *testing.T) {
	config := ServerConfig{Users: []User{{Name: "b@example.com", Password: "b"}, {Name: "a@example.com", Password: "old"}}}
	merged, err := MergeUsers(config, User{Name: "a@example.com", Password: "new"}, User{Name: "c@example.com", Password: "c"})
	if err != nil {
		t.Fatalf("MergeUsers() error: %v", err)
	}
	if got := []string{merged.Users[0].Name, merged.Users[1].Name, merged.Users[2].Name}; got[0] != "a@example.com" || got[1] != "b@example.com" || got[2] != "c@example.com" {
		t.Fatalf("users not sorted after merge: %v", got)
	}
	if merged.Users[0].Password != "new" {
		t.Fatalf("existing user was not replaced: %+v", merged.Users[0])
	}
	remaining := DeleteUsers(merged, "b@example.com", "missing@example.com")
	if len(remaining.Users) != 2 || remaining.Users[0].Name != "a@example.com" || remaining.Users[1].Name != "c@example.com" {
		t.Fatalf("DeleteUsers() got %+v", remaining.Users)
	}
}

func TestSimpleLinksCompatibleWithSubscriptionParser(t *testing.T) {
	const upstreamReference = "/tmp/mieru-upstream/pkg/appctl/url.go:66"
	links, err := SimpleLinks(ClientExport{
		ProfileName: "default",
		UserName:    "alice@example.com",
		Password:    "p@ss word",
		MTU:         1280,
		Endpoints: []Endpoint{{
			Host: "203.0.113.10",
			PortBinding: []PortBinding{
				{Port: 443, Protocol: TransportTCP},
				{PortRange: "5000-5001", Protocol: TransportUDP},
			},
		}},
	})
	if err != nil {
		t.Fatalf("SimpleLinks() error: %v", err)
	}
	want := "mierus://alice%40example.com:p%40ss%20word@203.0.113.10?mtu=1280&port=443&port=5000-5001&profile=default&protocol=TCP&protocol=UDP"
	if links[0] != want {
		t.Fatalf("link mismatch against %s\nwant %s\ngot  %s", upstreamReference, want, links[0])
	}
}

func TestRedaction(t *testing.T) {
	public := RedactConfig(ServerConfig{
		PortBindings: []PortBinding{{Port: 443, Protocol: TransportTCP}},
		Users:        []User{{Name: "alice@example.com", Password: "secret", HashedPassword: "hash"}},
	})
	body, err := json.Marshal(public)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "secret") || strings.Contains(string(body), "hash") {
		t.Fatalf("public model leaked credential material: %s", string(body))
	}
	if !public.Users[0].HasCredential {
		t.Fatal("public user should disclose credential presence only")
	}
}

func TestStatusMapping(t *testing.T) {
	status := MapStatus("RUNNING", true)
	if status.State != StatusRunning || !status.Running || !status.Installed || status.MissingBinary {
		t.Fatalf("unexpected running status: %+v", status)
	}
	missing := MapStatus("", false)
	if !missing.MissingBinary || missing.State != StatusMissing {
		t.Fatalf("unexpected missing status: %+v", missing)
	}
}
