package mieru

import (
	"encoding/json"
	"errors"
	"net/netip"
	"strings"
)

const DefaultClientSOCKS5Port = 1080

func ClientJSON(in ClientExport) ([]byte, error) {
	if strings.TrimSpace(in.ProfileName) == "" || strings.TrimSpace(in.UserName) == "" || in.Password == "" || len(in.Endpoints) == 0 {
		return nil, errors.New("invalid Mieru client export")
	}
	servers := make([]map[string]any, 0, len(in.Endpoints))
	for _, endpoint := range in.Endpoints {
		host := strings.TrimSpace(endpoint.Host)
		if host == "" || len(endpoint.PortBinding) == 0 {
			return nil, errors.New("invalid Mieru client endpoint")
		}
		server := map[string]any{"portBindings": endpoint.PortBinding}
		if addr, err := netip.ParseAddr(host); err == nil {
			if !addr.Is4() {
				return nil, errors.New("IPv6 Mieru endpoint is not supported")
			}
			server["ipAddress"] = addr.String()
		} else {
			server["domainName"] = host
		}
		servers = append(servers, server)
	}
	profile := map[string]any{
		"profileName": in.ProfileName,
		"user":        map[string]string{"name": in.UserName, "password": in.Password},
		"servers":     servers,
	}
	if in.MTU > 0 {
		profile["mtu"] = in.MTU
	}
	config := map[string]any{
		"profiles":             []any{profile},
		"activeProfile":        in.ProfileName,
		"socks5Port":           DefaultClientSOCKS5Port,
		"loggingLevel":         "INFO",
		"socks5ListenLAN":      false,
		"socks5Authentication": []any{},
	}
	return json.MarshalIndent(config, "", "  ")
}
