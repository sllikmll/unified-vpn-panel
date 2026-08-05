package service

import (
	"errors"
	"testing"

	"gorm.io/gorm"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func TestManagedOnlyClientOwnershipAndGhostCleanup(t *testing.T) {
	initManagedEndpointServiceDB(t)
	db := database.GetDB()
	svc := &ClientService{}

	managed := model.ClientRecord{Email: "managed-only@example.com", SubID: "managed-sub", Enable: true}
	legacy := model.ClientRecord{Email: "legacy@example.com", SubID: "legacy-sub", Enable: true}
	ghost := model.ClientRecord{Email: "ghost@example.com", SubID: "ghost-sub", Enable: true}
	for _, row := range []*model.ClientRecord{&managed, &legacy, &ghost} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	endpoint := model.ManagedEndpoint{UserId: 1, RuntimeKind: model.RuntimeMieru, Protocol: "mieru", Tag: "mieru", Port: 32002, Enable: true, Status: model.EndpointActive, DesiredConfig: `{}`}
	if err := db.Create(&endpoint).Error; err != nil {
		t.Fatal(err)
	}
	managedLink := model.ManagedEndpointClient{EndpointId: endpoint.Id, ClientId: managed.Id, SubID: managed.SubID, Email: managed.Email, Enable: true, State: model.EndpointClientApplied}
	if err := db.Create(&managedLink).Error; err != nil {
		t.Fatal(err)
	}
	inbound := model.Inbound{Port: 9443, Protocol: model.VLESS, Tag: "legacy", Enable: true, Settings: `{"clients":[]}`}
	if err := db.Create(&inbound).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ClientInbound{ClientId: legacy.Id, InboundId: inbound.Id}).Error; err != nil {
		t.Fatal(err)
	}

	managedOnly, err := svc.ManagedOnlyEmails([]string{managed.Email, legacy.Email})
	if err != nil {
		t.Fatal(err)
	}
	if len(managedOnly) != 1 || managedOnly[0] != managed.Email {
		t.Fatalf("ManagedOnlyEmails = %#v", managedOnly)
	}
	listed, err := svc.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 2 || listed[0].Email != legacy.Email || listed[1].Email != ghost.Email {
		t.Fatalf("List = %#v, want legacy-owned and unattached records only", listed)
	}
	paged, err := svc.ListPaged(&InboundService{}, nil, ClientPageParams{PageSize: 50})
	if err != nil {
		t.Fatal(err)
	}
	if paged.Total != 2 || paged.Filtered != 2 || len(paged.Items) != 2 || paged.Items[0].Email != legacy.Email || paged.Items[1].Email != ghost.Email {
		t.Fatalf("ListPaged = %#v, want legacy-owned and unattached records only", paged)
	}

	if err := svc.DeleteRecordIfUnattached(managed.Email, managed.SubID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.GetRecordByEmail(nil, managed.Email); err != nil {
		t.Fatalf("attached managed identity was deleted: %v", err)
	}
	if err := svc.DeleteRecordIfUnattached(ghost.Email, ghost.SubID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.GetRecordByEmail(nil, ghost.Email); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("ghost identity still exists: %v", err)
	}
}
