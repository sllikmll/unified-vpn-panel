package service

import "testing"

func TestNodeFullStackProvisionPlanContainsProductionProtocolPack(t *testing.T) {
	plan := BuildNodeFullStackProvisionPlan(NodeFullStackProvisionRequest{
		NodeName: "amstnew",
		BasePort: 32000,
		ClientEmails: []string{
			"pavel-1-keenetic",
			"pavel-2-openwrt",
		},
	})

	if plan.NodeName != "amstnew" {
		t.Fatalf("NodeName = %q, want amstnew", plan.NodeName)
	}
	if len(plan.Protocols) != 9 {
		t.Fatalf("protocol count = %d, want 9: %#v", len(plan.Protocols), plan.Protocols)
	}
	want := map[string]int{
		"vmess":       32011,
		"vless":       32012,
		"trojan":      32013,
		"shadowsocks": 32014,
		"wireguard":   32015,
		"hysteria2":   32016,
		"amneziawg":   32001,
		"mieru":       32002,
		"naiveproxy":  32003,
	}
	for _, step := range plan.Protocols {
		port, ok := want[step.Protocol]
		if !ok {
			t.Fatalf("unexpected protocol %q", step.Protocol)
		}
		if step.Port != port {
			t.Fatalf("%s port = %d, want %d", step.Protocol, step.Port, port)
		}
		delete(want, step.Protocol)
	}
	if len(want) != 0 {
		t.Fatalf("missing protocols: %#v", want)
	}
	if plan.Reality.ServerName != "yandex.ru" || plan.Reality.Target != "yandex.ru:443" || plan.Reality.SpiderX != "/" {
		t.Fatalf("bad reality defaults: %#v", plan.Reality)
	}
	if !plan.HasSeparateWireGuardAndAWG2 {
		t.Fatal("WireGuard and AWG2 must be separate provision steps")
	}
	if !plan.MTProxyExternalOnly {
		t.Fatal("Telegram MTProxy must stay external-only")
	}
	for _, email := range []string{"pavel-1-keenetic", "pavel-2-openwrt"} {
		if !plan.SubscriptionClients[email] {
			t.Fatalf("subscription client %q not selected in plan", email)
		}
	}
}
