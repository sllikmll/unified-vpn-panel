package service

import (
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/web/runtime/driver"
)

func TestManagedAWGTrafficCollectorStoresDeltasAndHandlesCounterReset(t *testing.T) {
	initManagedEndpointServiceDB(t)
	db := database.GetDB()
	record := model.ClientRecord{Email: "peer@example.test", SubID: "sub-peer", Enable: true}
	if err := db.Create(&record).Error; err != nil {
		t.Fatal(err)
	}
	endpoint := model.ManagedEndpoint{UserId: 1, RuntimeKind: model.RuntimeAmneziaWG, Protocol: model.ManagedProtocol("amneziawg"), Tag: "awg2", Port: 51820, Enable: true, Status: model.EndpointActive, DesiredConfig: `{}`}
	if err := db.Create(&endpoint).Error; err != nil {
		t.Fatal(err)
	}
	client := model.ManagedEndpointClient{EndpointId: endpoint.Id, ClientId: record.Id, Email: record.Email, Enable: true, State: model.EndpointClientApplied, PublicIdentity: "client-1", Address: "10.66.66.2/32"}
	if err := db.Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1720000100, 0)
	collector := ManagedAWGTrafficCollector{Now: func() time.Time { return now }}
	if err := collector.storeEndpointSnapshot(db, endpoint, []driver.PeerStatusResult{{ClientID: "client-1", Enabled: true, LastHandshakeUnix: 1720000000, RxBytes: 100, TxBytes: 200}}); err != nil {
		t.Fatal(err)
	}
	if err := collector.storeEndpointSnapshot(db, endpoint, []driver.PeerStatusResult{{ClientID: "client-1", Enabled: true, LastHandshakeUnix: 1720000001, RxBytes: 130, TxBytes: 260}}); err != nil {
		t.Fatal(err)
	}
	// A runtime restart resets counters. The first post-reset sample is a new delta.
	if err := collector.storeEndpointSnapshot(db, endpoint, []driver.PeerStatusResult{{ClientID: "client-1", Enabled: true, LastHandshakeUnix: 1720000002, RxBytes: 7, TxBytes: 9}}); err != nil {
		t.Fatal(err)
	}
	var traffic model.ManagedEndpointClientTraffic
	if err := db.First(&traffic, "endpoint_id = ? AND email = ?", endpoint.Id, record.Email).Error; err != nil {
		t.Fatal(err)
	}
	if traffic.Up != 137 || traffic.Down != 269 || traffic.LastUpCounter != 7 || traffic.LastDownCounter != 9 {
		t.Fatalf("traffic = %+v", traffic)
	}
	clients, err := (ManagedEndpointMutationService{}).ListClients(1, endpoint.Id)
	if err != nil {
		t.Fatal(err)
	}
	if len(clients) != 1 || clients[0].TrafficUp != 137 || clients[0].TrafficDown != 269 || clients[0].LatestHandshake != 1720000002 {
		t.Fatalf("clients = %+v", clients)
	}
}

func TestMonotonicDelta(t *testing.T) {
	for _, tc := range []struct{ previous, current, want int64 }{{10, 15, 5}, {15, 4, 4}, {0, 0, 0}} {
		if got := monotonicDelta(tc.previous, tc.current); got != tc.want {
			t.Fatalf("monotonicDelta(%d,%d)=%d want %d", tc.previous, tc.current, got, tc.want)
		}
	}
}
