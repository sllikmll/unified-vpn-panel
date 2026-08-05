package naiveproxy

import (
	"strings"
	"testing"
)

func validServer() Server {
	return Server{
		Endpoint: Endpoint{Domain: "Example.COM.", ListenIP: "203.0.113.10", Port: 443, ACMEEmail: "ops@example.com"},
		Users: []User{
			{ID: "2", Username: "beta", Password: "beta-password-123", Enabled: false},
			{ID: "1", Username: "alpha", Password: "alpha-password-123", Enabled: true},
			{ID: "3", Username: "zeta.user", Password: "zeta password 123", Enabled: true},
		},
	}
}

func TestValidationRejectsInvalidEndpointAndUsers(t *testing.T) {
	cases := []Server{
		{Endpoint: Endpoint{Domain: "localhost", ListenIP: "203.0.113.10", Port: 443}, Users: []User{{ID: "u", Username: "user", Password: "long-password", Enabled: true}}},
		{Endpoint: Endpoint{Domain: "example.com", ListenIP: "::1", Port: 443}, Users: []User{{ID: "u", Username: "user", Password: "long-password", Enabled: true}}},
		{Endpoint: Endpoint{Domain: "example.com", ListenIP: "0.0.0.0", Port: 443}, Users: []User{{ID: "u", Username: "user", Password: "long-password", Enabled: true}}},
		{Endpoint: Endpoint{Domain: "example.com", ListenIP: "203.0.113.10", Port: 0}, Users: []User{{ID: "u", Username: "user", Password: "long-password", Enabled: true}}},
		{Endpoint: Endpoint{Domain: "example.com", ListenIP: "203.0.113.10", Port: 443}, Users: []User{{ID: "u", Username: "bad user", Password: "long-password", Enabled: true}}},
		{Endpoint: Endpoint{Domain: "example.com", ListenIP: "203.0.113.10", Port: 443}, Users: []User{{ID: "u", Username: "user", Password: "short", Enabled: true}}},
		{Endpoint: Endpoint{Domain: "example.com", ListenIP: "203.0.113.10", Port: 443}, Users: []User{{ID: "u", Username: "user", Password: "long-password", Enabled: false}}},
	}
	for i, tc := range cases {
		if err := tc.Validate(); err == nil {
			t.Fatalf("case %d: expected validation error", i)
		}
	}
}

func TestValidationRejectsDuplicateProtocolUsers(t *testing.T) {
	s := validServer()
	s.Users = append(s.Users, User{ID: "4", Username: "ALPHA", Password: "another-password", Enabled: true})
	if err := s.Validate(); err == nil || !strings.Contains(err.Error(), "duplicate username") {
		t.Fatalf("expected duplicate username error, got %v", err)
	}
}

func TestMultiUserCRUDSemantics(t *testing.T) {
	s := validServer()
	if err := s.UpsertUser(User{ID: "4", Username: "new-user", Password: "new-user-password", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertUser(User{ID: "4", Username: "new-user-renamed", Password: "new-user-password-2", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertUser(User{ID: "5", Username: "ALPHA", Password: "another-password", Enabled: true}); err == nil {
		t.Fatal("expected duplicate username rejection")
	}
	if err := s.DeleteUser("4"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteUser("missing"); err == nil {
		t.Fatal("expected missing user error")
	}
	list := s.ListUsers()
	if len(list) != 3 || list[0].Username != "alpha" {
		t.Fatalf("unexpected public user list: %#v", list)
	}
}

func TestExportURIEscapesCredentials(t *testing.T) {
	u := User{ID: "u", Username: "user.name", Password: "p@ss word:/?#123", Enabled: true}
	got, err := u.ExportURI(Endpoint{Domain: "Naive.Example", ListenIP: "203.0.113.10", Port: 8443})
	if err != nil {
		t.Fatal(err)
	}
	want := "naive+https://user.name:p%40ss%20word%3A%2F%3F%23123@naive.example:8443"
	if got != want {
		t.Fatalf("uri mismatch\nwant %s\ngot  %s", want, got)
	}
}
