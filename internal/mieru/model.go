package mieru

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	BinaryName          = "mita"
	DefaultConfigPath   = "/etc/mita/server.json"
	DefaultDownloadHost = "github.com"
)

type Transport string

const (
	TransportTCP Transport = "TCP"
	TransportUDP Transport = "UDP"
)

type ServerConfig struct {
	PortBindings []PortBinding `json:"portBindings,omitempty"`
	Users        []User        `json:"users,omitempty"`
	MTU          int           `json:"mtu,omitempty"`
}

type PortBinding struct {
	Port      int       `json:"port,omitempty"`
	Protocol  Transport `json:"protocol,omitempty"`
	PortRange string    `json:"portRange,omitempty"`
}

type User struct {
	Name            string  `json:"name,omitempty"`
	Password        string  `json:"password,omitempty"`
	HashedPassword  string  `json:"hashedPassword,omitempty"`
	Quotas          []Quota `json:"quotas,omitempty"`
	AllowPrivateIP  bool    `json:"allowPrivateIP,omitempty"`
	AllowLoopbackIP bool    `json:"allowLoopbackIP,omitempty"`
}

type Quota struct {
	Days      int `json:"days,omitempty"`
	Megabytes int `json:"megabytes,omitempty"`
}

type Endpoint struct {
	Host        string        `json:"host"`
	PortBinding []PortBinding `json:"portBindings"`
}

type ClientExport struct {
	ProfileName string     `json:"profileName"`
	UserName    string     `json:"userName"`
	Password    string     `json:"password,omitempty"`
	Endpoints   []Endpoint `json:"endpoints"`
	MTU         int        `json:"mtu,omitempty"`
}

type PublicUser struct {
	Name            string  `json:"name"`
	Quotas          []Quota `json:"quotas,omitempty"`
	AllowPrivateIP  bool    `json:"allowPrivateIP"`
	AllowLoopbackIP bool    `json:"allowLoopbackIP"`
	HasCredential   bool    `json:"hasCredential"`
}

type PublicServerConfig struct {
	PortBindings []PortBinding `json:"portBindings,omitempty"`
	Users        []PublicUser  `json:"users,omitempty"`
	MTU          int           `json:"mtu,omitempty"`
}

type StatusState string

const (
	StatusUnknown  StatusState = "unknown"
	StatusMissing  StatusState = "missing"
	StatusIdle     StatusState = "idle"
	StatusStarting StatusState = "starting"
	StatusRunning  StatusState = "running"
	StatusStopping StatusState = "stopping"
	StatusStopped  StatusState = "stopped"
	StatusError    StatusState = "error"
)

type Status struct {
	State         StatusState `json:"state"`
	Installed     bool        `json:"installed"`
	Running       bool        `json:"running"`
	MissingBinary bool        `json:"missingBinary"`
	Version       string      `json:"version,omitempty"`
}

type TrafficCounters struct {
	Supported bool             `json:"supported"`
	Users     map[string]int64 `json:"users,omitempty"`
}

var (
	ErrMissingBinary        = errors.New("mieru mita binary is missing")
	ErrCommandFailed        = errors.New("mieru mita command failed")
	ErrUnsupportedTraffic   = errors.New("mieru traffic counters are unsupported by this backend")
	ErrUnsupportedOperation = errors.New("mieru operation is unsupported by this backend")
	portRangePattern        = regexp.MustCompile(`^(\d+)-(\d+)$`)
	statusPattern           = regexp.MustCompile(`(?i)"?(IDLE|STARTING|RUNNING|STOPPING|STOPPED)"?`)
)

func (c ServerConfig) ValidateFull() error {
	if len(c.PortBindings) == 0 {
		return fmt.Errorf("server port binding is not set")
	}
	return c.ValidatePatch()
}

func (c ServerConfig) ValidatePatch() error {
	if _, err := flatPortBindings(c.PortBindings); err != nil {
		return err
	}
	if c.MTU != 0 && (c.MTU < 1280 || c.MTU > 1500) {
		return fmt.Errorf("MTU value %d is out of range, valid range is [1280, 1500]", c.MTU)
	}
	seen := map[string]struct{}{}
	for _, u := range c.Users {
		if err := u.Validate(); err != nil {
			return err
		}
		if _, ok := seen[u.Name]; ok {
			return fmt.Errorf("duplicate user %q", u.Name)
		}
		seen[u.Name] = struct{}{}
	}
	return nil
}

func (u User) Validate() error {
	if u.Name == "" {
		return fmt.Errorf("user name is not set")
	}
	if len(u.Name) > 64 {
		return fmt.Errorf("user name exceeds 64 bytes")
	}
	if u.Password == "" && u.HashedPassword == "" {
		return fmt.Errorf("user password is not set")
	}
	if u.Password != "" && len(u.Password) > 64 {
		return fmt.Errorf("user password exceeds 64 bytes")
	}
	for _, q := range u.Quotas {
		if q.Days <= 0 {
			return fmt.Errorf("quota: number of days %d is invalid", q.Days)
		}
		if q.Megabytes <= 0 {
			return fmt.Errorf("quota: traffic volume in megabyte %d is invalid", q.Megabytes)
		}
	}
	return nil
}

func CanonicalConfig(c ServerConfig) (ServerConfig, error) {
	if err := c.ValidatePatch(); err != nil {
		return ServerConfig{}, err
	}
	out := c
	bindings, err := flatPortBindings(c.PortBindings)
	if err != nil {
		return ServerConfig{}, err
	}
	out.PortBindings = bindings
	out.Users = append([]User(nil), c.Users...)
	sort.Slice(out.Users, func(i, j int) bool { return out.Users[i].Name < out.Users[j].Name })
	return out, nil
}

func CanonicalJSON(c ServerConfig) ([]byte, error) {
	canonical, err := CanonicalConfig(c)
	if err != nil {
		return nil, err
	}
	return json.MarshalIndent(canonical, "", "    ")
}

func MergeUsers(c ServerConfig, users ...User) (ServerConfig, error) {
	index := map[string]User{}
	for _, u := range c.Users {
		if err := u.Validate(); err != nil {
			return ServerConfig{}, err
		}
		index[u.Name] = u
	}
	for _, u := range users {
		if err := u.Validate(); err != nil {
			return ServerConfig{}, err
		}
		index[u.Name] = u
	}
	names := make([]string, 0, len(index))
	for name := range index {
		names = append(names, name)
	}
	sort.Strings(names)
	c.Users = c.Users[:0]
	for _, name := range names {
		c.Users = append(c.Users, index[name])
	}
	return c, nil
}

func DeleteUsers(c ServerConfig, names ...string) ServerConfig {
	deleteSet := map[string]struct{}{}
	for _, name := range names {
		deleteSet[name] = struct{}{}
	}
	remaining := make([]User, 0, len(c.Users))
	for _, u := range c.Users {
		if _, ok := deleteSet[u.Name]; !ok {
			remaining = append(remaining, u)
		}
	}
	c.Users = remaining
	return c
}

func RedactConfig(c ServerConfig) PublicServerConfig {
	canonical, err := CanonicalConfig(c)
	if err != nil {
		canonical = c
	}
	out := PublicServerConfig{PortBindings: canonical.PortBindings, MTU: canonical.MTU}
	for _, u := range canonical.Users {
		out.Users = append(out.Users, PublicUser{
			Name:            u.Name,
			Quotas:          append([]Quota(nil), u.Quotas...),
			AllowPrivateIP:  u.AllowPrivateIP,
			AllowLoopbackIP: u.AllowLoopbackIP,
			HasCredential:   u.Password != "" || u.HashedPassword != "",
		})
	}
	return out
}

func SimpleLinks(export ClientExport) ([]string, error) {
	if export.ProfileName == "" {
		return nil, fmt.Errorf("profile name is empty")
	}
	if export.UserName == "" {
		return nil, fmt.Errorf("user name is empty")
	}
	if export.Password == "" {
		return nil, fmt.Errorf("password is empty")
	}
	if len(export.Endpoints) == 0 {
		return nil, fmt.Errorf("no endpoints")
	}
	links := make([]string, 0, len(export.Endpoints))
	for _, endpoint := range export.Endpoints {
		if err := validateEndpointHost(endpoint.Host); err != nil {
			return nil, err
		}
		bindings, err := validateExportPortBindings(endpoint.PortBinding)
		if err != nil {
			return nil, err
		}
		u := &url.URL{Scheme: "mierus", Host: endpoint.Host}
		u.User = url.UserPassword(export.UserName, export.Password)
		q := url.Values{}
		q.Add("profile", export.ProfileName)
		if export.MTU != 0 {
			if export.MTU < 1280 || export.MTU > 1500 {
				return nil, fmt.Errorf("MTU value %d is out of range, valid range is [1280, 1500]", export.MTU)
			}
			q.Add("mtu", strconv.Itoa(export.MTU))
		}
		for _, binding := range bindings {
			if binding.PortRange != "" {
				q.Add("port", binding.PortRange)
			} else {
				q.Add("port", strconv.Itoa(binding.Port))
			}
			q.Add("protocol", string(binding.Protocol))
		}
		u.RawQuery = q.Encode()
		links = append(links, u.String())
	}
	return links, nil
}

func validateExportPortBindings(bindings []PortBinding) ([]PortBinding, error) {
	if len(bindings) == 0 {
		return nil, fmt.Errorf("port binding is not set")
	}
	out := make([]PortBinding, 0, len(bindings))
	for _, binding := range bindings {
		if binding.Protocol != TransportTCP && binding.Protocol != TransportUDP {
			return nil, fmt.Errorf("protocol is not set")
		}
		if binding.Port != 0 && binding.PortRange != "" {
			return nil, fmt.Errorf("port and portRange cannot both be set")
		}
		if binding.Port != 0 {
			if binding.Port < 1 || binding.Port > 65535 {
				return nil, fmt.Errorf("port number %d is invalid", binding.Port)
			}
			out = append(out, binding)
			continue
		}
		matches := portRangePattern.FindStringSubmatch(binding.PortRange)
		if len(matches) != 3 {
			return nil, fmt.Errorf("unable to parse port range %q", binding.PortRange)
		}
		begin, _ := strconv.Atoi(matches[1])
		end, _ := strconv.Atoi(matches[2])
		if begin > end {
			return nil, fmt.Errorf("begin of port range %d is bigger than end of port range %d", begin, end)
		}
		if begin < 1 || end > 65535 {
			return nil, fmt.Errorf("port range %q is invalid", binding.PortRange)
		}
		out = append(out, binding)
	}
	return out, nil
}

func MapStatus(raw string, installed bool) Status {
	if !installed {
		return Status{State: StatusMissing, MissingBinary: true}
	}
	state := mapStatusState(raw)
	return Status{State: state, Installed: true, Running: state == StatusRunning}
}

func mapStatusState(raw string) StatusState {
	token := strings.ToUpper(strings.TrimSpace(raw))
	if matches := statusPattern.FindStringSubmatch(raw); len(matches) == 2 {
		token = strings.ToUpper(matches[1])
	}
	switch token {
	case "IDLE":
		return StatusStopped
	case "STARTING":
		return StatusStarting
	case "RUNNING":
		return StatusRunning
	case "STOPPING":
		return StatusStopping
	case "STOPPED":
		return StatusStopped
	default:
		return StatusUnknown
	}
}

func flatPortBindings(bindings []PortBinding) ([]PortBinding, error) {
	tcp := map[int]struct{}{}
	udp := map[int]struct{}{}
	for _, binding := range bindings {
		if binding.Protocol != TransportTCP && binding.Protocol != TransportUDP {
			return nil, fmt.Errorf("protocol is not set")
		}
		add := func(port int) error {
			if port < 1 || port > 65535 {
				return fmt.Errorf("port number %d is invalid", port)
			}
			if binding.Protocol == TransportTCP {
				tcp[port] = struct{}{}
			} else {
				udp[port] = struct{}{}
			}
			return nil
		}
		if binding.Port != 0 && binding.PortRange != "" {
			return nil, fmt.Errorf("port and portRange cannot both be set")
		}
		if binding.Port != 0 {
			if err := add(binding.Port); err != nil {
				return nil, err
			}
			continue
		}
		matches := portRangePattern.FindStringSubmatch(binding.PortRange)
		if len(matches) != 3 {
			return nil, fmt.Errorf("unable to parse port range %q", binding.PortRange)
		}
		begin, _ := strconv.Atoi(matches[1])
		end, _ := strconv.Atoi(matches[2])
		if begin > end {
			return nil, fmt.Errorf("begin of port range %d is bigger than end of port range %d", begin, end)
		}
		for port := begin; port <= end; port++ {
			if err := add(port); err != nil {
				return nil, err
			}
		}
	}
	out := make([]PortBinding, 0, len(tcp)+len(udp))
	addSorted := func(ports map[int]struct{}, protocol Transport) {
		list := make([]int, 0, len(ports))
		for port := range ports {
			list = append(list, port)
		}
		sort.Ints(list)
		for _, port := range list {
			out = append(out, PortBinding{Port: port, Protocol: protocol})
		}
	}
	addSorted(tcp, TransportTCP)
	addSorted(udp, TransportUDP)
	return out, nil
}

func validateEndpointHost(host string) error {
	if host == "" {
		return fmt.Errorf("endpoint host is empty")
	}
	if ip := net.ParseIP(host); ip != nil {
		if ip.To4() == nil {
			return fmt.Errorf("IPv6 endpoint %q is not supported", host)
		}
		return nil
	}
	if strings.Contains(host, ":") {
		return fmt.Errorf("IPv6 endpoint %q is not supported", host)
	}
	return nil
}
