package nodecommand_test

import (
	"reflect"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/web/runtime/nodecommand"
)

func TestAuthenticatedSessionIsOpaqueOutsidePackage(t *testing.T) {
	sessionType := reflect.TypeOf(nodecommand.AuthenticatedSession{})
	for i := 0; i < sessionType.NumField(); i++ {
		field := sessionType.Field(i)
		if field.IsExported() {
			t.Fatalf("AuthenticatedSession field %s is exported", field.Name)
		}
	}

	var session nodecommand.AuthenticatedSession
	if session.NodeID() != 0 || session.TargetGUID() != "" || session.Principal() != "" || session.ChannelID() != "" {
		t.Fatalf("zero session accessors should expose only empty bindings")
	}
	if !session.AuthenticatedAt().IsZero() || !session.ExpiresAt().IsZero() {
		t.Fatalf("zero session time accessors should expose zero times")
	}
}

func TestTransportAndExecutorInterfacesAreOpaqueOutsidePackage(t *testing.T) {
	var _ nodecommand.Transport = nodecommand.TransportFunc(nil)
	var _ nodecommand.Executor = nodecommand.ExecutorFunc(nil)

	transportType := reflect.TypeOf((*nodecommand.Transport)(nil)).Elem()
	executorType := reflect.TypeOf((*nodecommand.Executor)(nil)).Elem()
	if method, ok := transportType.MethodByName("nodeCommandTransport"); !ok || method.PkgPath == "" {
		t.Fatalf("transport marker is not sealed: method=%#v ok=%v", method, ok)
	}
	if method, ok := executorType.MethodByName("nodeCommandExecutor"); !ok || method.PkgPath == "" {
		t.Fatalf("executor marker is not sealed: method=%#v ok=%v", method, ok)
	}
}
