package service

import (
	"sort"
	"strings"
)

type NodeFullStackProvisionRequest struct {
	NodeName     string   `json:"nodeName"`
	BasePort     int      `json:"basePort"`
	ClientEmails []string `json:"clientEmails"`
}

type NodeFullStackRealityDefaults struct {
	ServerName  string `json:"serverName"`
	Target      string `json:"target"`
	SpiderX     string `json:"spiderX"`
	Fingerprint string `json:"fingerprint"`
}

type NodeFullStackProtocolStep struct {
	Protocol     string `json:"protocol"`
	RuntimeKind  string `json:"runtimeKind,omitempty"`
	Tag          string `json:"tag"`
	Remark       string `json:"remark"`
	Port         int    `json:"port"`
	Transport    string `json:"transport,omitempty"`
	Managed      bool   `json:"managed"`
	Subscription bool   `json:"subscription"`
}

type NodeFullStackProvisionPlan struct {
	NodeName                    string                       `json:"nodeName"`
	BasePort                    int                          `json:"basePort"`
	Reality                     NodeFullStackRealityDefaults `json:"reality"`
	Protocols                   []NodeFullStackProtocolStep  `json:"protocols"`
	SubscriptionClients         map[string]bool              `json:"subscriptionClients"`
	HasSeparateWireGuardAndAWG2 bool                         `json:"hasSeparateWireGuardAndAWG2"`
	MTProxyExternalOnly         bool                         `json:"mtProxyExternalOnly"`
	ManualSQLRequired           bool                         `json:"manualSqlRequired"`
}

func BuildNodeFullStackProvisionPlan(req NodeFullStackProvisionRequest) NodeFullStackProvisionPlan {
	nodeName := sanitizeProvisionNodeName(req.NodeName)
	base := req.BasePort
	if base <= 0 {
		base = 31000
	}
	clients := make(map[string]bool, len(req.ClientEmails))
	for _, email := range req.ClientEmails {
		email = strings.TrimSpace(email)
		if email != "" {
			clients[email] = true
		}
	}
	protocols := []NodeFullStackProtocolStep{
		{Protocol: "amneziawg", RuntimeKind: "amneziawg", Tag: "awg2-" + nodeName + "-production", Remark: "AmneziaWG 2.0 " + nodeName, Port: base + 1, Managed: true, Subscription: true},
		{Protocol: "mieru", RuntimeKind: "mieru", Tag: "mieru-" + nodeName + "-production", Remark: "Mieru " + nodeName, Port: base + 2, Managed: true, Subscription: true},
		{Protocol: "naiveproxy", RuntimeKind: "naiveproxy", Tag: "naive-" + nodeName + "-production", Remark: "NaiveProxy " + nodeName, Port: base + 3, Managed: true, Subscription: true},
		{Protocol: "vmess", Tag: "vmess-" + nodeName + "-production", Remark: "VMess " + nodeName, Port: base + 11, Transport: "tcp", Subscription: true},
		{Protocol: "vless", Tag: "vless-reality-" + nodeName + "-production", Remark: "VLESS Reality " + nodeName, Port: base + 12, Transport: "tcp+reality", Subscription: true},
		{Protocol: "trojan", Tag: "trojan-" + nodeName + "-production", Remark: "Trojan TLS " + nodeName, Port: base + 13, Transport: "tcp+tls", Subscription: true},
		{Protocol: "shadowsocks", Tag: "ss2022-" + nodeName + "-production", Remark: "Shadowsocks 2022 " + nodeName, Port: base + 14, Transport: "tcp+udp", Subscription: true},
		{Protocol: "wireguard", Tag: "wireguard-" + nodeName + "-production", Remark: "WireGuard " + nodeName, Port: base + 15, Transport: "udp", Subscription: true},
		{Protocol: "hysteria2", Tag: "hysteria2-" + nodeName + "-production", Remark: "Hysteria2 " + nodeName, Port: base + 16, Transport: "udp", Subscription: true},
	}
	return NodeFullStackProvisionPlan{
		NodeName:                    nodeName,
		BasePort:                    base,
		Reality:                     NodeFullStackRealityDefaults{ServerName: "yandex.ru", Target: "yandex.ru:443", SpiderX: "/", Fingerprint: "firefox"},
		Protocols:                   protocols,
		SubscriptionClients:         clients,
		HasSeparateWireGuardAndAWG2: true,
		MTProxyExternalOnly:         true,
		ManualSQLRequired:           false,
	}
}

func sanitizeProvisionNodeName(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	if name == "" {
		return "node"
	}
	var b strings.Builder
	lastDash := false
	for _, r := range name {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if ok {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "node"
	}
	return out
}

func (p NodeFullStackProvisionPlan) ProtocolNames() []string {
	out := make([]string, 0, len(p.Protocols))
	for _, proto := range p.Protocols {
		out = append(out, proto.Protocol)
	}
	sort.Strings(out)
	return out
}
