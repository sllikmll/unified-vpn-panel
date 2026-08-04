package naiveproxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

func GenerateCaddyfile(s Server) (string, error) {
	if err := s.Validate(); err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString("{\n")
	b.WriteString("  order forward_proxy before file_server\n")
	b.WriteString("  log {\n")
	b.WriteString("    exclude http.log.error\n")
	b.WriteString("  }\n")
	b.WriteString("}\n")
	fmt.Fprintf(&b, ":%d, %s {\n", s.Endpoint.Port, canonicalDomain(s.Endpoint.Domain))
	if s.Endpoint.ACMEEmail != "" {
		fmt.Fprintf(&b, "  tls %s\n", s.Endpoint.ACMEEmail)
	} else {
		b.WriteString("  tls\n")
	}
	b.WriteString("  encode\n")
	b.WriteString("  forward_proxy {\n")
	for _, u := range s.activeUsers() {
		fmt.Fprintf(&b, "    basic_auth %s %s\n", caddyQuote(u.Username), caddyQuote(u.Password))
	}
	b.WriteString("    hide_ip\n")
	b.WriteString("    hide_via\n")
	b.WriteString("    probe_resistance\n")
	b.WriteString("  }\n")
	b.WriteString("  file_server {\n")
	b.WriteString("    root /var/www/html\n")
	b.WriteString("  }\n")
	b.WriteString("}\n")
	return b.String(), nil
}

func GenerateCaddyJSON(s Server) ([]byte, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	users := make([]map[string]string, 0, len(s.activeUsers()))
	for _, u := range s.activeUsers() {
		users = append(users, map[string]string{"username": u.Username, "password": u.Password})
	}
	config := map[string]any{
		"apps": map[string]any{
			"http": map[string]any{
				"servers": map[string]any{
					"naiveproxy": map[string]any{
						"listen": []string{listenAddr(s.Endpoint)},
						"routes": []any{
							map[string]any{
								"match": []any{map[string]any{"host": []string{canonicalDomain(s.Endpoint.Domain)}}},
								"handle": []any{
									map[string]any{
										"handler":          "forward_proxy",
										"auth_credentials": users,
										"hide_ip":          true,
										"hide_via":         true,
										"probe_resistance": true,
									},
									map[string]any{"handler": "file_server", "root": "/var/www/html"},
								},
								"terminal": true,
							},
						},
					},
				},
			},
			"tls": map[string]any{
				"automation": map[string]any{
					"policies": []any{map[string]any{"subjects": []string{canonicalDomain(s.Endpoint.Domain)}}},
				},
			},
		},
	}
	if s.Endpoint.ACMEEmail != "" {
		config["apps"].(map[string]any)["tls"].(map[string]any)["automation"].(map[string]any)["policies"].([]any)[0].(map[string]any)["issuers"] = []any{
			map[string]any{"module": "acme", "email": s.Endpoint.ACMEEmail},
		}
	}
	out, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return nil, err
	}
	out = append(out, '\n')
	return out, nil
}

func listenAddr(e Endpoint) string {
	return fmt.Sprintf("%s:%d", e.ListenIP, e.Port)
}

func caddyQuote(s string) string {
	if s == "" {
		return `""`
	}
	if strings.ContainsAny(s, " \t\r\n\"\\{}") {
		var b bytes.Buffer
		enc := json.NewEncoder(&b)
		enc.SetEscapeHTML(false)
		_ = enc.Encode(s)
		return strings.TrimSpace(b.String())
	}
	return s
}
