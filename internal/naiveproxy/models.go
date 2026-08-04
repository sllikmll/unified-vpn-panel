package naiveproxy

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

const (
	ProtocolName = "naiveproxy"
	ClientScheme = "naive+https"
)

var (
	domainLabelRE = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
	usernameRE    = regexp.MustCompile(`^[A-Za-z0-9._~-]{1,64}$`)
)

// Endpoint is the public HTTPS endpoint served by the patched Caddy
// forwardproxy runtime. ListenIP is intentionally IPv4-only for this phase.
type Endpoint struct {
	Domain    string
	ListenIP  string
	Port      int
	ACMEEmail string
}

// User is one protocol credential for NaiveProxy. It is not a panel admin and
// must not be treated as a native-login account.
type User struct {
	ID       string
	Username string
	Password string
	Enabled  bool
}

type UserPublic struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Enabled  bool   `json:"enabled"`
}

type Server struct {
	Endpoint Endpoint
	Users    []User
}

func (s Server) ListUsers() []UserPublic {
	out := make([]UserPublic, 0, len(s.Users))
	for _, u := range s.Users {
		out = append(out, u.Public())
	}
	slices.SortFunc(out, func(a, b UserPublic) int {
		return strings.Compare(strings.ToLower(a.Username), strings.ToLower(b.Username))
	})
	return out
}

func (s *Server) UpsertUser(u User) error {
	if err := u.Validate(); err != nil {
		return err
	}
	for i, existing := range s.Users {
		if existing.ID == u.ID {
			for j, other := range s.Users {
				if i != j && strings.EqualFold(other.Username, u.Username) {
					return fmt.Errorf("duplicate username %q", u.Username)
				}
			}
			s.Users[i] = u
			return s.Validate()
		}
		if strings.EqualFold(existing.Username, u.Username) {
			return fmt.Errorf("duplicate username %q", u.Username)
		}
	}
	s.Users = append(s.Users, u)
	return s.Validate()
}

func (s *Server) DeleteUser(id string) error {
	for i, u := range s.Users {
		if u.ID == id {
			s.Users = append(s.Users[:i], s.Users[i+1:]...)
			return s.Validate()
		}
	}
	return fmt.Errorf("user %q not found", id)
}

func (e Endpoint) Validate() error {
	if err := validateDomain(e.Domain); err != nil {
		return fmt.Errorf("domain: %w", err)
	}
	if err := validatePort(e.Port); err != nil {
		return fmt.Errorf("port: %w", err)
	}
	addr, err := netip.ParseAddr(e.ListenIP)
	if err != nil {
		return fmt.Errorf("listen ip: %w", err)
	}
	if !addr.Is4() || addr.IsUnspecified() {
		return errors.New("listen ip must be a non-unspecified IPv4 address")
	}
	if e.ACMEEmail != "" && !strings.Contains(e.ACMEEmail, "@") {
		return errors.New("acme email must be empty or contain @")
	}
	return nil
}

func (u User) Validate() error {
	if strings.TrimSpace(u.ID) == "" {
		return errors.New("id is required")
	}
	if !usernameRE.MatchString(u.Username) {
		return errors.New("username must be 1-64 chars using A-Z a-z 0-9 . _ ~ -")
	}
	if utf8.RuneCountInString(u.Password) < 12 || utf8.RuneCountInString(u.Password) > 256 {
		return errors.New("password must be 12-256 characters")
	}
	if strings.ContainsAny(u.Password, "\r\n\t") {
		return errors.New("password must not contain control whitespace")
	}
	return nil
}

func (s Server) Validate() error {
	if err := s.Endpoint.Validate(); err != nil {
		return err
	}
	seenID := map[string]struct{}{}
	seenName := map[string]struct{}{}
	active := 0
	for _, u := range s.Users {
		if err := u.Validate(); err != nil {
			return fmt.Errorf("user %q: %w", u.ID, err)
		}
		if _, ok := seenID[u.ID]; ok {
			return fmt.Errorf("duplicate user id %q", u.ID)
		}
		seenID[u.ID] = struct{}{}
		key := strings.ToLower(u.Username)
		if _, ok := seenName[key]; ok {
			return fmt.Errorf("duplicate username %q", u.Username)
		}
		seenName[key] = struct{}{}
		if u.Enabled {
			active++
		}
	}
	if active == 0 {
		return errors.New("at least one enabled protocol user is required")
	}
	return nil
}

func validateDomain(domain string) error {
	domain = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(domain)), ".")
	if domain == "" || len(domain) > 253 {
		return errors.New("must be a non-empty DNS name up to 253 characters")
	}
	if ip := net.ParseIP(domain); ip != nil {
		return errors.New("must be a DNS name, not an IP address")
	}
	labels := strings.Split(domain, ".")
	if len(labels) < 2 {
		return errors.New("must contain at least two labels")
	}
	for _, label := range labels {
		if !domainLabelRE.MatchString(label) {
			return fmt.Errorf("invalid label %q", label)
		}
	}
	return nil
}

func canonicalDomain(domain string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(domain)), ".")
}

func validatePort(port int) error {
	if port < 1 || port > 65535 {
		return errors.New("must be between 1 and 65535")
	}
	return nil
}

func (s Server) activeUsers() []User {
	users := make([]User, 0, len(s.Users))
	for _, u := range s.Users {
		if u.Enabled {
			users = append(users, u)
		}
	}
	slices.SortFunc(users, func(a, b User) int {
		return strings.Compare(strings.ToLower(a.Username), strings.ToLower(b.Username))
	})
	return users
}

func (u User) Public() UserPublic {
	return UserPublic{ID: u.ID, Username: u.Username, Enabled: u.Enabled}
}

func (u User) ExportURI(e Endpoint) (string, error) {
	if err := e.Validate(); err != nil {
		return "", err
	}
	if err := u.Validate(); err != nil {
		return "", err
	}
	if !u.Enabled {
		return "", errors.New("cannot export disabled user")
	}
	host := canonicalDomain(e.Domain)
	if e.Port != 443 {
		host = fmt.Sprintf("%s:%d", host, e.Port)
	}
	v := url.URL{
		Scheme: ClientScheme,
		Host:   host,
		User:   url.UserPassword(u.Username, u.Password),
	}
	return v.String(), nil
}
