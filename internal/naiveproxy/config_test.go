package naiveproxy

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestGenerateCaddyfileGolden(t *testing.T) {
	got, err := GenerateCaddyfile(validServer())
	if err != nil {
		t.Fatal(err)
	}
	want := `{
  order forward_proxy before file_server
  log {
    exclude http.log.error
  }
}
:443, example.com:443 {
  tls ops@example.com
  encode
  forward_proxy {
    basic_auth alpha alpha-password-123
    basic_auth zeta.user "zeta password 123"
    hide_ip
    hide_via
    probe_resistance
  }
  file_server {
    root /var/www/html
  }
}
`
	if got != want {
		t.Fatalf("golden mismatch\nwant:\n%s\ngot:\n%s", want, got)
	}
	if strings.Contains(got, "beta-password") {
		t.Fatalf("disabled user leaked into config:\n%s", got)
	}
}

func TestGenerateCaddyJSONIsCanonicalAndValidJSON(t *testing.T) {
	got, err := GenerateCaddyJSON(validServer())
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(got, &decoded); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, got)
	}
	again, err := GenerateCaddyJSON(validServer())
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(again) {
		t.Fatalf("json should be deterministic")
	}
	if strings.Contains(string(got), "beta-password") {
		t.Fatalf("disabled user leaked into json:\n%s", got)
	}
}
