package naiveproxy

import "time"

type State string

const (
	StateUnknown   State = "unknown"
	StateMissing   State = "missing"
	StateStopped   State = "stopped"
	StateStarting  State = "starting"
	StateRunning   State = "running"
	StateReloading State = "reloading"
	StateFailed    State = "failed"
)

type BackendState string

const (
	BackendUnknown    BackendState = "unknown"
	BackendNotFound   BackendState = "not_found"
	BackendInactive   BackendState = "inactive"
	BackendActive     BackendState = "active"
	BackendActivating BackendState = "activating"
	BackendReloading  BackendState = "reloading"
	BackendFailed     BackendState = "failed"
)

type Observation struct {
	State       BackendState
	Version     string
	ServiceName string
	Executable  string
	CheckedAt   time.Time
	Error       string
}

type Status struct {
	State       State        `json:"state"`
	Version     string       `json:"version,omitempty"`
	ServiceName string       `json:"serviceName"`
	Executable  string       `json:"executable"`
	CheckedAt   time.Time    `json:"checkedAt"`
	Message     string       `json:"message,omitempty"`
	Users       []UserPublic `json:"users,omitempty"`
}

func MapObservation(obs Observation, users []User) Status {
	out := Status{
		State:       mapState(obs.State),
		Version:     obs.Version,
		ServiceName: obs.ServiceName,
		Executable:  obs.Executable,
		CheckedAt:   obs.CheckedAt,
		Message:     redact(obs.Error),
	}
	for _, u := range users {
		out.Users = append(out.Users, u.Public())
	}
	return out
}

func mapState(s BackendState) State {
	switch s {
	case BackendNotFound:
		return StateMissing
	case BackendInactive:
		return StateStopped
	case BackendActive:
		return StateRunning
	case BackendActivating:
		return StateStarting
	case BackendReloading:
		return StateReloading
	case BackendFailed:
		return StateFailed
	default:
		return StateUnknown
	}
}

func redact(s string) string {
	if s == "" {
		return ""
	}
	return "[redacted runtime detail]"
}
