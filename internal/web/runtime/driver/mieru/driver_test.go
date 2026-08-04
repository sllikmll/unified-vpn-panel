package mieru

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	core "github.com/mhsanaei/3x-ui/v3/internal/mieru"
	"github.com/mhsanaei/3x-ui/v3/internal/web/runtime/driver"
)

type fakeRunner struct {
	commands []string
	err      error
}

func (r *fakeRunner) Run(_ context.Context, name string, args ...string) (core.RunnerResult, error) {
	r.commands = append(r.commands, name+" "+strings.Join(args, " "))
	if r.err != nil {
		return core.RunnerResult{}, r.err
	}
	if len(args) > 0 && args[0] == "status" {
		return core.RunnerResult{Output: "RUNNING"}, nil
	}
	return core.RunnerResult{Output: "mita version v1.0.0"}, nil
}

type fakeFS struct {
	files map[string][]byte
	n     int
}

func (fs *fakeFS) ReadFile(path string) ([]byte, error) { return fs.files[path], nil }
func (fs *fakeFS) WriteFile(path string, data []byte, _ uint32) error {
	if fs.files == nil {
		fs.files = map[string][]byte{}
	}
	fs.files[path] = append([]byte(nil), data...)
	return nil
}

func (fs *fakeFS) Rename(oldPath, newPath string) error {
	if fs.files == nil {
		fs.files = map[string][]byte{}
	}
	fs.files[newPath] = fs.files[oldPath]
	delete(fs.files, oldPath)
	return nil
}
func (fs *fakeFS) Remove(path string) error { delete(fs.files, path); return nil }
func (fs *fakeFS) Exists(path string) bool  { _, ok := fs.files[path]; return ok }
func (fs *fakeFS) TempFile(dir, _ string) (string, error) {
	fs.n++
	return dir + "/tmp-" + string(rune('0'+fs.n)), nil
}

func TestMieruDriverContract(t *testing.T) {
	runner := &fakeRunner{}
	fs := &fakeFS{files: map[string][]byte{}}
	d := New(core.NewTrustedLocalRuntime(runner, fs, "/tmp/mita.json"))
	cfg := core.ServerConfig{PortBindings: []core.PortBinding{{Port: 2999, Protocol: core.TransportTCP}}, Users: []core.User{{Name: "alice", Password: "secret"}}, MTU: 1280}
	raw, _ := json.Marshal(cfg)
	inbound := &model.Inbound{Id: 1, Tag: "mieru", Port: 2999, Protocol: "mieru", Enable: true, Settings: string(raw)}

	if _, err := d.Create(context.Background(), inbound); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, ok := fs.files["/tmp/mita.json"]; !ok {
		t.Fatal("Create did not write durable config")
	}
	if st, err := d.Status(context.Background(), inbound); err != nil || st.Status != model.EndpointActive {
		t.Fatalf("Status = %+v, %v", st, err)
	}
	if h, err := d.Health(context.Background(), inbound); err != nil || h.Status != model.EndpointActive {
		t.Fatalf("Health = %+v, %v", h, err)
	}
	if _, err := d.Clients().Create(context.Background(), inbound, model.Client{Email: "a@example.test"}); !errors.Is(err, driver.ErrUnsupportedOperation) {
		t.Fatalf("client create error = %v", err)
	}
}
