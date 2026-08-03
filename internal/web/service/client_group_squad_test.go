package service

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/web/runtime"
)

func TestGroupPolicyValidationRejectsInvalidDefaults(t *testing.T) {
	setupBulkDB(t)
	svc := &ClientService{}
	inboundSvc := &InboundService{}

	for _, tc := range []struct {
		name   string
		policy GroupPolicy
		want   string
	}{
		{name: "negative traffic", policy: GroupPolicy{DefaultTotalGB: -1}, want: "traffic"},
		{name: "negative expiry", policy: GroupPolicy{DefaultExpiryTime: -1}, want: "expiry"},
		{name: "past expiry", policy: GroupPolicy{DefaultExpiryTime: time.Now().Add(-time.Hour).UnixMilli()}, want: "future"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.CreateGroupWithConfig(inboundSvc, GroupUpsertRequest{
				Name:   "bad-" + strings.ReplaceAll(tc.name, " ", "-"),
				Enable: true,
				Policy: tc.policy,
			})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("CreateGroupWithConfig error = %v, want containing %q", err, tc.want)
			}
		})
	}
}

func TestApplyGroupAssignmentsAttachesAssignedInboundAndDefaults(t *testing.T) {
	setupBulkDB(t)
	nodeID, fake := setupNodeRuntime(t)
	ib := nodeInbound(t, nodeID, 30101, nil)
	expiry := time.Now().Add(24 * time.Hour).UnixMilli()

	rec := &model.ClientRecord{
		Email:     "squad-user@x",
		SubID:     "sub-squad-user",
		UUID:      uuid.NewString(),
		Group:     "ops",
		Enable:    true,
		CreatedAt: time.Now().UnixMilli(),
	}
	if err := database.GetDB().Create(rec).Error; err != nil {
		t.Fatalf("create client: %v", err)
	}
	group := &model.ClientGroup{
		Name:               "ops",
		Enable:             true,
		AssignedInboundIds: marshalGroupInboundIDs([]int{ib.Id}),
		DefaultTotalGB:     12345,
		DefaultExpiryTime:  expiry,
	}
	if err := database.GetDB().Create(group).Error; err != nil {
		t.Fatalf("create group: %v", err)
	}

	result, err := (&ClientService{}).ApplyGroupAssignments(&InboundService{}, "ops")
	if err != nil {
		t.Fatalf("ApplyGroupAssignments: %v", err)
	}
	if result.Attached != 1 || result.Updated != 1 {
		t.Fatalf("result = %+v, want one attach and one policy update", result)
	}
	if got := fake.addClient.Load(); got != 1 {
		t.Fatalf("runtime AddClient calls = %d, want 1", got)
	}
	if got := fake.updateUser.Load(); got != 1 {
		t.Fatalf("runtime UpdateUser calls = %d, want 1", got)
	}

	var stored model.ClientRecord
	if err := database.GetDB().Where("email = ?", rec.Email).First(&stored).Error; err != nil {
		t.Fatalf("load client: %v", err)
	}
	if stored.TotalGB != 12345 || stored.ExpiryTime != expiry {
		t.Fatalf("policy defaults not applied: total=%d expiry=%d", stored.TotalGB, stored.ExpiryTime)
	}
}

func TestApplyGroupAssignmentsUsesLocalRuntimeOverride(t *testing.T) {
	setupBulkDB(t)
	prev := runtime.GetManager()
	mgr := runtime.NewManager(runtime.LocalDeps{APIPort: func() int { return 62789 }, SetNeedRestart: func() {}})
	fake := &fakeNodeRuntime{}
	mgr.SetLocalRuntimeOverride(fake)
	runtime.SetManager(mgr)
	t.Cleanup(func() { runtime.SetManager(prev) })

	ib := mkInbound(t, 30102, model.VLESS, `{"clients":[]}`)
	rec := &model.ClientRecord{Email: "local-squad@x", SubID: "sub-local", UUID: uuid.NewString(), Group: "local", Enable: true}
	if err := database.GetDB().Create(rec).Error; err != nil {
		t.Fatalf("create client: %v", err)
	}
	if err := database.GetDB().Create(&model.ClientGroup{
		Name:               "local",
		Enable:             true,
		AssignedInboundIds: marshalGroupInboundIDs([]int{ib.Id}),
	}).Error; err != nil {
		t.Fatalf("create group: %v", err)
	}

	if _, err := (&ClientService{}).ApplyGroupAssignments(&InboundService{}, "local"); err != nil {
		t.Fatalf("ApplyGroupAssignments: %v", err)
	}
	if got := fake.addUser.Load(); got != 1 {
		t.Fatalf("local runtime AddUser calls = %d, want 1", got)
	}
}

func TestRemoveFromGroupDetachesOnlyConfiguredSquadInbounds(t *testing.T) {
	setupBulkDB(t)
	nodeID, fake := setupNodeRuntime(t)
	c := model.Client{Email: "detach-squad@x", ID: uuid.NewString(), SubID: "sub-detach", Group: "ops", Enable: true}
	assigned := nodeInbound(t, nodeID, 30103, []model.Client{c})
	other := nodeInbound(t, nodeID, 30104, []model.Client{c})
	if err := database.GetDB().Create(&model.ClientGroup{
		Name:               "ops",
		Enable:             true,
		AssignedInboundIds: marshalGroupInboundIDs([]int{assigned.Id}),
	}).Error; err != nil {
		t.Fatalf("create group: %v", err)
	}

	result, err := (&ClientService{}).RemoveFromGroupAndApply(&InboundService{}, []string{c.Email})
	if err != nil {
		t.Fatalf("RemoveFromGroupAndApply: %v", err)
	}
	if result.Detached != 1 {
		t.Fatalf("result = %+v, want one configured detach", result)
	}
	if got := fake.deleteUser.Load(); got != 1 {
		t.Fatalf("runtime DeleteUser calls = %d, want 1", got)
	}
	ids, err := (&ClientService{}).GetInboundIdsForRecord(lookupClientRecord(t, c.Email).Id)
	if err != nil {
		t.Fatalf("GetInboundIdsForRecord: %v", err)
	}
	if len(ids) != 1 || ids[0] != other.Id {
		t.Fatalf("remaining inbound ids = %v, want only %d", ids, other.Id)
	}
}
