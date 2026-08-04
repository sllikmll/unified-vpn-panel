package service

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func TestProbeParsesNetIO(t *testing.T) {
	patch, err := decodeHeartbeatStatus(strings.NewReader(`{"success":true,"obj":{"cpu":5,"cpuCores":4,"logicalPro":8,"cpuSpeedMhz":2396.4,"mem":{"current":1,"total":2},"swap":{"current":3,"total":4},"disk":{"current":5,"total":6},"netIO":{"up":1000,"down":2000},"netTraffic":{"sent":3000,"recv":4000},"tcpCount":9,"udpCount":10,"appStats":{"mem":11,"threads":12,"uptime":13},"publicIP":{"ipv4":"203.0.113.9","ipv6":"2001:db8::9"},"xray":{"version":"26.6.27","state":"running","errorMsg":""},"panelVersion":"0.0.1","panelGuid":"g","uptime":42}}`))
	if err != nil {
		t.Fatalf("decodeHeartbeatStatus: %v", err)
	}
	if patch.NetUp != 1000 || patch.NetDown != 2000 {
		t.Fatalf("net throughput not parsed from status: up=%d down=%d", patch.NetUp, patch.NetDown)
	}
	if patch.CpuCores != 4 || patch.LogicalPro != 8 || patch.CpuSpeedMhz != 2396.4 {
		t.Fatalf("cpu details not parsed: %+v", patch)
	}
	if patch.MemCurrent != 1 || patch.MemTotal != 2 || patch.SwapCurrent != 3 || patch.SwapTotal != 4 || patch.DiskCurrent != 5 || patch.DiskTotal != 6 {
		t.Fatalf("resource details not parsed: %+v", patch)
	}
	if patch.NetTrafficSent != 3000 || patch.NetTrafficRecv != 4000 || patch.TcpCount != 9 || patch.UdpCount != 10 {
		t.Fatalf("traffic/connection details not parsed: %+v", patch)
	}
	if patch.AppStatsMem != 11 || patch.AppStatsThreads != 12 || patch.AppStatsUptime != 13 {
		t.Fatalf("app stats not parsed: %+v", patch)
	}
	if patch.PublicIPV4 != "203.0.113.9" || patch.PublicIPV6 != "2001:db8::9" || patch.PanelVersion != "0.0.1" || patch.Guid != "g" {
		t.Fatalf("identity/public details not parsed: %+v", patch)
	}
}

func TestUpdateHeartbeatStoresNetMetrics(t *testing.T) {
	_ = setupSettingMtlsDB(t)
	s := &NodeService{}

	n := &model.Node{Name: "netn", Address: "1.2.3.4", Port: 2053, Scheme: "https", ApiToken: "t"}
	if err := database.GetDB().Create(n).Error; err != nil {
		t.Fatalf("create node: %v", err)
	}

	patch := HeartbeatPatch{
		Status:          "online",
		LastHeartbeat:   time.Now().Unix(),
		CpuCores:        4,
		LogicalPro:      8,
		CpuSpeedMhz:     2400,
		MemCurrent:      100,
		MemTotal:        200,
		SwapCurrent:     10,
		SwapTotal:       20,
		DiskCurrent:     300,
		DiskTotal:       400,
		NetUp:           111,
		NetDown:         222,
		NetTrafficSent:  333,
		NetTrafficRecv:  444,
		TcpCount:        5,
		UdpCount:        6,
		AppStatsMem:     777,
		AppStatsThreads: 8,
		AppStatsUptime:  999,
		PublicIPV4:      "203.0.113.10",
		PublicIPV6:      "2001:db8::10",
	}
	if err := s.UpdateHeartbeat(n.Id, patch); err != nil {
		t.Fatalf("UpdateHeartbeat: %v", err)
	}

	var got model.Node
	if err := database.GetDB().First(&got, n.Id).Error; err != nil {
		t.Fatalf("reload node: %v", err)
	}
	if got.NetUp != 111 || got.NetDown != 222 {
		t.Fatalf("net columns not persisted: up=%d down=%d", got.NetUp, got.NetDown)
	}
	if got.CpuCores != 4 || got.LogicalPro != 8 || got.CpuSpeedMhz != 2400 || got.MemCurrent != 100 || got.MemTotal != 200 {
		t.Fatalf("cpu/memory columns not persisted: %+v", got)
	}
	if got.SwapCurrent != 10 || got.SwapTotal != 20 || got.DiskCurrent != 300 || got.DiskTotal != 400 {
		t.Fatalf("swap/disk columns not persisted: %+v", got)
	}
	if got.NetTrafficSent != 333 || got.NetTrafficRecv != 444 || got.TcpCount != 5 || got.UdpCount != 6 {
		t.Fatalf("traffic/connection columns not persisted: %+v", got)
	}
	if got.AppStatsMem != 777 || got.AppStatsThreads != 8 || got.AppStatsUptime != 999 || got.PublicIPV4 != "203.0.113.10" || got.PublicIPV6 != "2001:db8::10" {
		t.Fatalf("app/public columns not persisted: %+v", got)
	}
	if len(s.AggregateNodeMetric(n.Id, "netUp", 2, 60)) == 0 {
		t.Fatal("expected netUp history points after an online heartbeat")
	}
	if len(s.AggregateNodeMetric(n.Id, "diskUsage", 2, 60)) == 0 {
		t.Fatal("expected diskUsage history points after an online heartbeat")
	}
}

func TestUpdateHeartbeatOfflinePreservesLastSuccessfulTelemetry(t *testing.T) {
	_ = setupSettingMtlsDB(t)
	s := &NodeService{}

	n := &model.Node{Name: "preserve", Address: "1.2.3.4", Port: 2053, Scheme: "https", ApiToken: "t"}
	if err := database.GetDB().Create(n).Error; err != nil {
		t.Fatalf("create node: %v", err)
	}

	online := HeartbeatPatch{
		Status:          "online",
		LastHeartbeat:   time.Now().Unix(),
		CpuPct:          55,
		MemCurrent:      100,
		MemTotal:        200,
		MemPct:          50,
		DiskCurrent:     300,
		DiskTotal:       600,
		NetTrafficSent:  700,
		NetTrafficRecv:  800,
		AppStatsThreads: 9,
		PublicIPV4:      "203.0.113.55",
		XrayVersion:     "26.6.27",
		PanelVersion:    "0.0.1",
		XrayState:       "running",
	}
	if err := s.UpdateHeartbeat(n.Id, online); err != nil {
		t.Fatalf("online UpdateHeartbeat: %v", err)
	}
	if err := s.UpdateHeartbeat(n.Id, HeartbeatPatch{
		Status:        "offline",
		LastHeartbeat: online.LastHeartbeat + 5,
		LastError:     "connection refused",
	}); err != nil {
		t.Fatalf("offline UpdateHeartbeat: %v", err)
	}

	var got model.Node
	if err := database.GetDB().First(&got, n.Id).Error; err != nil {
		t.Fatalf("reload node: %v", err)
	}
	if got.Status != "offline" || got.LastError != "connection refused" {
		t.Fatalf("health fields not updated on offline heartbeat: %+v", got)
	}
	if got.CpuPct != 55 || got.MemCurrent != 100 || got.DiskCurrent != 300 || got.NetTrafficSent != 700 || got.AppStatsThreads != 9 || got.PublicIPV4 != "203.0.113.55" {
		t.Fatalf("offline heartbeat wiped last successful telemetry: %+v", got)
	}
	if got.XrayVersion != "26.6.27" || got.PanelVersion != "0.0.1" || got.XrayState != "running" {
		t.Fatalf("offline heartbeat wiped last successful versions/state: %+v", got)
	}
}

func TestNodeMetricKeysIncludesNet(t *testing.T) {
	for _, k := range []string{"swap", "diskUsage", "netUp", "netDown", "tcpCount", "udpCount"} {
		if !slices.Contains(NodeMetricKeys, k) {
			t.Fatalf("NodeMetricKeys must include %q so the history endpoint accepts it", k)
		}
	}
}
