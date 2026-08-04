package database

import (
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestNodeTelemetryColumnsAutoMigrate(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.Node{}); err != nil {
		t.Fatalf("AutoMigrate Node: %v", err)
	}

	cols := []string{
		"cpu_cores", "logical_pro", "cpu_speed_mhz",
		"mem_current", "mem_total", "swap_current", "swap_total",
		"disk_current", "disk_total",
		"net_traffic_sent", "net_traffic_recv",
		"tcp_count", "udp_count",
		"app_stats_mem", "app_stats_threads", "app_stats_uptime",
		"public_ip_v4", "public_ip_v6",
	}
	for _, col := range cols {
		if !db.Migrator().HasColumn(&model.Node{}, col) {
			t.Fatalf("nodes table missing telemetry column %q", col)
		}
	}
}
