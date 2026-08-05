package naiveproxy

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	core "github.com/mhsanaei/3x-ui/v3/internal/naiveproxy"
	"github.com/mhsanaei/3x-ui/v3/internal/web/runtime/driver"
)

type fakeRunner struct{ commands []core.Command }

func (r *fakeRunner) Run(_ context.Context, cmd core.Command) error {
	r.commands = append(r.commands, cmd)
	return nil
}

func (r *fakeRunner) Observe(context.Context, string) (core.Observation, error) {
	return core.Observation{State: core.BackendActive}, nil
}

type fakeStore struct{ server core.Server }

func (s *fakeStore) Load(context.Context) (core.Server, error) { return s.server, nil }
func (s *fakeStore) AtomicWrite(_ context.Context, server core.Server, _ []byte) (core.Backup, error) {
	old := s.server
	s.server = server
	return core.Backup{OldServer: old, HadPrevious: old.Endpoint.Domain != ""}, nil
}
func (s *fakeStore) Commit(core.Backup) error     { return nil }
func (s *fakeStore) Rollback(b core.Backup) error { s.server = b.OldServer; return nil }
func (s *fakeStore) Delete(context.Context) error { s.server = core.Server{}; return nil }

type fakeHealth struct{}

func (fakeHealth) Verify(context.Context, core.Endpoint) error { return nil }

func TestNaiveProxyDriverContract(t *testing.T) {
	store := &fakeStore{}
	rt, err := core.NewRuntime(&fakeRunner{}, store, fakeHealth{})
	if err != nil {
		t.Fatal(err)
	}
	d := New(rt)
	payload := map[string]any{
		"endpoint": core.Endpoint{Domain: "example.test", ListenIP: "127.0.0.1", Port: 443, ACMEEmail: "ops@example.test"},
		"users":    []core.User{{ID: "u1", Username: "alice", Password: "secret-123456", Enabled: true}},
	}
	raw, _ := json.Marshal(payload)
	inbound := &model.Inbound{Id: 1, Tag: "naive", Port: 443, Protocol: "naiveproxy", Enable: true, Settings: string(raw)}

	if _, err := d.Create(context.Background(), inbound); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if store.server.Endpoint.Domain != "example.test" || len(store.server.Users) != 1 {
		t.Fatalf("stored server = %#v", store.server)
	}
	if st, err := d.Status(context.Background(), inbound); err != nil || st.Status != model.EndpointActive {
		t.Fatalf("Status = %+v, %v", st, err)
	}
	if _, err := d.Clients().Delete(context.Background(), inbound, "alice"); !errors.Is(err, driver.ErrUnsupportedOperation) {
		t.Fatalf("client delete error = %v", err)
	}
}
