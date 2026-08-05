package naiveproxy

import (
	"strings"
	"testing"
	"time"
)

func TestStatusRedactsRuntimeDetailsAndPasswords(t *testing.T) {
	status := MapObservation(Observation{
		State:       BackendFailed,
		Version:     "v150.0.7871.63-1",
		ServiceName: FixedServiceName,
		Executable:  FixedExecutableName,
		CheckedAt:   time.Unix(1, 0),
		Error:       "reload failed for alpha-password-123 on stdout",
	}, validServer().Users)
	if status.State != StateFailed {
		t.Fatalf("state = %s", status.State)
	}
	if strings.Contains(status.Message, "alpha-password") || strings.Contains(status.Message, "stdout") {
		t.Fatalf("status leaked runtime detail: %#v", status)
	}
	for _, u := range status.Users {
		if strings.Contains(u.Username, "password") {
			t.Fatalf("unexpected secret in user model: %#v", u)
		}
	}
}

func TestStateMapping(t *testing.T) {
	tests := map[BackendState]State{
		BackendNotFound:   StateMissing,
		BackendInactive:   StateStopped,
		BackendActive:     StateRunning,
		BackendActivating: StateStarting,
		BackendReloading:  StateReloading,
		BackendFailed:     StateFailed,
	}
	for in, want := range tests {
		if got := mapState(in); got != want {
			t.Fatalf("%s maps to %s, want %s", in, got, want)
		}
	}
}
