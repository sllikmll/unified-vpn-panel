package service

import (
	"strings"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func TestClientCreateRejectsMixedLegacyAndManagedTargetsBeforeDB(t *testing.T) {
	payload := &ClientCreatePayload{
		Client:             model.Client{Email: "mixed@example.com", SubID: "mixed-sub", Enable: true},
		InboundIds:         []int{1},
		ManagedEndpointIds: []string{"managed:1"},
	}
	_, err := (&ClientService{}).Create(&InboundService{}, payload)
	if err == nil || !strings.Contains(err.Error(), "cannot be mixed") {
		t.Fatalf("Create error = %v, want mixed-lifecycle rejection", err)
	}
}
