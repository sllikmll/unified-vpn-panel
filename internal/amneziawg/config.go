package amneziawg

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/netip"
	"strconv"
	"strings"
)

type BackendKind string

const (
	BackendNone   BackendKind = ""
	BackendDocker BackendKind = "docker-amnezia-awg2"
	BackendNative BackendKind = "native-awg"
)

const (
	DockerContainerName      = "amnezia-awg2"
	DockerHostStateDir       = "/opt/amnezia/state/amnezia-awg2"
	DockerContainerConfigDir = "/opt/amnezia/awg"
	NativeConfigDir          = "/etc/amnezia/amneziawg"
)

type BackendProfile struct {
	Kind               BackendKind
	ContainerName      string
	HostConfigDir      string
	ContainerConfigDir string
}

func DockerBackendProfile() BackendProfile {
	return BackendProfile{
		Kind:               BackendDocker,
		ContainerName:      DockerContainerName,
		HostConfigDir:      DockerHostStateDir,
		ContainerConfigDir: DockerContainerConfigDir,
	}
}

func NativeBackendProfile() BackendProfile {
	return BackendProfile{Kind: BackendNative, HostConfigDir: NativeConfigDir}
}

type State string

const (
	StateUnknown State = ""
	StateRunning State = "running"
	StateStopped State = "stopped"
	StateApplied State = "applied"
	StateDeleted State = "deleted"
)

type Obfuscation20 struct {
	Jc   int    `json:"jc"`
	Jmin int    `json:"jmin"`
	Jmax int    `json:"jmax"`
	S1   int    `json:"s1"`
	S2   int    `json:"s2"`
	S3   int    `json:"s3"`
	S4   int    `json:"s4"`
	H1   string `json:"h1"`
	H2   string `json:"h2"`
	H3   string `json:"h3"`
	H4   string `json:"h4"`
	I1   string `json:"i1"`
}

type Server struct {
	Enable        bool   `json:"enable"`
	InterfaceName string `json:"interfaceName"`
	ListenPort    int    `json:"listenPort"`
	MTU           int    `json:"mtu"`
	PrivateKey    string `json:"privateKey"`
	PublicKey     string `json:"publicKey"`
	IPv4Address   string `json:"ipv4Address"`
	IPv4Pool      string `json:"ipv4Pool"`
	IPv6Enabled   bool   `json:"ipv6Enabled"`
	DNS           string `json:"dns"`
	Endpoint      string `json:"endpoint"`
	Obfuscation20
}

type Client struct {
	ID                  string `json:"id"`
	Email               string `json:"email"`
	PrivateKey          string `json:"privateKey"`
	PublicKey           string `json:"publicKey"`
	PresharedKey        string `json:"presharedKey"`
	IPv4Address         string `json:"ipv4Address"`
	AllowedIPs          string `json:"allowedIPs"`
	ClientAllowedIPs    string `json:"clientAllowedIPs"`
	PersistentKeepalive int    `json:"persistentKeepalive"`
	Enable              bool   `json:"enable"`
}

type DesiredConfig struct {
	Server         Server         `json:"server"`
	ClientDefaults ClientDefaults `json:"clientDefaults,omitempty"`
	Clients        []Client       `json:"clients"`
}

type ClientDefaults struct {
	AllowedIPs          string `json:"allowedIPs,omitempty"`
	PersistentKeepalive int    `json:"persistentKeepalive,omitempty"`
}

type SafeStatus struct {
	Backend   BackendKind  `json:"backend"`
	Available bool         `json:"available"`
	State     State        `json:"state"`
	Peers     []PeerStatus `json:"peers,omitempty"`
}

type PeerStatus struct {
	ClientID          string `json:"clientId"`
	PublicKey         string `json:"-"`
	Enabled           bool   `json:"enabled"`
	LastHandshakeUnix int64  `json:"lastHandshakeUnix,omitempty"`
	RxBytes           int64  `json:"rxBytes,omitempty"`
	TxBytes           int64  `json:"txBytes,omitempty"`
}

const (
	awgHMaxGenerated int64  = 2147483647
	awgHMaxValid     uint64 = 4294967295
	hMinWidth               = 1000
)

func DefaultServer(iface string, port int) Server {
	if iface == "" {
		iface = "awg0"
	}
	if port == 0 {
		port = 51820
	}
	obf, _ := GenerateObfuscation20("default")
	return Server{
		Enable:        true,
		InterfaceName: iface,
		ListenPort:    port,
		MTU:           1420,
		IPv4Address:   "10.66.66.1/24",
		IPv4Pool:      "10.66.66.0/24",
		DNS:           "1.1.1.1",
		Obfuscation20: obf,
	}
}

func GenerateObfuscation20(preset string) (Obfuscation20, error) {
	var o Obfuscation20
	var err error
	if preset == "mobile" {
		o.Jc = 3
		if o.Jmin, err = randInt(30, 50); err != nil {
			return o, err
		}
		delta, err := randInt(20, 80)
		if err != nil {
			return o, err
		}
		o.Jmax = o.Jmin + delta
	} else {
		if o.Jc, err = randInt(3, 6); err != nil {
			return o, err
		}
		if o.Jmin, err = randInt(40, 89); err != nil {
			return o, err
		}
		delta, err := randInt(50, 250)
		if err != nil {
			return o, err
		}
		o.Jmax = o.Jmin + delta
	}
	if o.S1, err = randInt(15, 150); err != nil {
		return o, err
	}
	for {
		if o.S2, err = randInt(15, 150); err != nil {
			return o, err
		}
		if o.S1+56 != o.S2 {
			break
		}
	}
	if o.S3, err = randInt(8, 55); err != nil {
		return o, err
	}
	if o.S4, err = randInt(4, 27); err != nil {
		return o, err
	}
	h, err := generateHRanges()
	if err != nil {
		return o, err
	}
	o.H1, o.H2, o.H3, o.H4 = h[0], h[1], h[2], h[3]
	i1, err := randInt(32, 256)
	if err != nil {
		return o, err
	}
	o.I1 = fmt.Sprintf("<r %d>", i1)
	return o, nil
}

func GenerateKey() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b[:]), nil
}

func randInt(min, max int) (int, error) {
	if max < min {
		return 0, fmt.Errorf("invalid random range %d..%d", min, max)
	}
	n, err := rand.Int(rand.Reader, big.NewInt(int64(max-min)+1))
	if err != nil {
		return 0, err
	}
	return min + int(n.Int64()), nil
}

func generateHRanges() ([4]string, error) {
	const lo int64 = 5
	bandSize := (awgHMaxGenerated - lo + 1) / 4
	var out [4]string
	for i := 0; i < 4; i++ {
		bandLo := lo + int64(i)*bandSize
		bandHi := bandLo + bandSize - 1
		start, err := randInt(int(bandLo), int(bandHi)-hMinWidth-1)
		if err != nil {
			return out, err
		}
		end, err := randInt(start+hMinWidth, int(bandHi)-1)
		if err != nil {
			return out, err
		}
		out[i] = fmt.Sprintf("%d-%d", start, end)
	}
	return out, nil
}

func ValidateServer(server Server) error {
	if strings.TrimSpace(server.InterfaceName) == "" || strings.ContainsAny(server.InterfaceName, "/\\\x00\r\n\t ") {
		return fmt.Errorf("invalid interfaceName")
	}
	if server.ListenPort <= 0 || server.ListenPort > 65535 {
		return fmt.Errorf("invalid listenPort")
	}
	if server.MTU < 0 || server.MTU > 9000 {
		return fmt.Errorf("invalid mtu")
	}
	if server.PrivateKey == "" || server.PublicKey == "" {
		return fmt.Errorf("missing key material")
	}
	if server.IPv6Enabled {
		return fmt.Errorf("ipv6 is disabled for amneziawg2")
	}
	if err := validateIPv4CIDR(server.IPv4Address); err != nil {
		return fmt.Errorf("invalid ipv4Address: %w", err)
	}
	if err := validateIPv4CIDR(server.IPv4Pool); err != nil {
		return fmt.Errorf("invalid ipv4Pool: %w", err)
	}
	return ValidateObfuscation20(server.Obfuscation20)
}

func ValidateObfuscation20(o Obfuscation20) error {
	if o.Jc < 1 || o.Jc > 128 {
		return fmt.Errorf("invalid Jc")
	}
	if o.Jmin < 0 || o.Jmax < 0 || o.Jmin > o.Jmax {
		return fmt.Errorf("invalid Jmin/Jmax")
	}
	if o.S1 < 0 || o.S2 < 0 || o.S1 > 1500 || o.S2 > 1500 {
		return fmt.Errorf("invalid S1/S2")
	}
	if o.S1+56 == o.S2 {
		return fmt.Errorf("invalid S1/S2 equality")
	}
	if o.S3 < 0 || o.S3 > 64 {
		return fmt.Errorf("invalid S3")
	}
	if o.S4 < 0 || o.S4 > 32 {
		return fmt.Errorf("invalid S4")
	}
	for i, h := range []string{o.H1, o.H2, o.H3, o.H4} {
		if err := validateHValue(h); err != nil {
			return fmt.Errorf("invalid H%d: %w", i+1, err)
		}
	}
	if o.I1 != "" {
		if !strings.HasPrefix(o.I1, "<r ") || !strings.HasSuffix(o.I1, ">") {
			return fmt.Errorf("invalid I1")
		}
		n, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(o.I1, "<r "), ">"))
		if err != nil || n < 0 || n > 1024 {
			return fmt.Errorf("invalid I1")
		}
	}
	return nil
}

func validateHValue(v string) error {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	if lo, hi, ok := strings.Cut(v, "-"); ok {
		l, err1 := parseAWGHValue(strings.TrimSpace(lo))
		h, err2 := parseAWGHValue(strings.TrimSpace(hi))
		if err1 != nil || err2 != nil || l > h {
			return fmt.Errorf("range must satisfy 0 <= low <= high <= %d", awgHMaxValid)
		}
		return nil
	}
	if _, err := parseAWGHValue(v); err != nil {
		return fmt.Errorf("value must be integer in 0..%d", awgHMaxValid)
	}
	return nil
}

func parseAWGHValue(v string) (uint64, error) {
	n, err := strconv.ParseUint(v, 10, 64)
	if err != nil || n > awgHMaxValid {
		return 0, fmt.Errorf("invalid H value")
	}
	return n, nil
}

func validateIPv4CIDR(value string) error {
	prefix, err := netip.ParsePrefix(value)
	if err != nil {
		return err
	}
	if !prefix.Addr().Is4() {
		return fmt.Errorf("not ipv4")
	}
	return nil
}

func RenderServerConfig(server Server, clients []Client) (string, error) {
	if err := ValidateServer(server); err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "[Interface]\nPrivateKey = %s\nAddress = %s\nListenPort = %d\n", server.PrivateKey, server.IPv4Address, server.ListenPort)
	if server.MTU > 0 {
		fmt.Fprintf(&b, "MTU = %d\n", server.MTU)
	}
	fmt.Fprintf(&b, "PostUp = iptables -C FORWARD -i %%i -j ACCEPT || iptables -I FORWARD 1 -i %%i -j ACCEPT; iptables -C FORWARD -o %%i -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT || iptables -I FORWARD 1 -o %%i -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT; iptables -t nat -C POSTROUTING -s %s -j MASQUERADE || iptables -t nat -A POSTROUTING -s %s -j MASQUERADE\n", server.IPv4Pool, server.IPv4Pool)
	fmt.Fprintf(&b, "PostDown = iptables -D FORWARD -i %%i -j ACCEPT || true; iptables -D FORWARD -o %%i -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT || true; iptables -t nat -D POSTROUTING -s %s -j MASQUERADE || true\n", server.IPv4Pool)
	writeObfuscation(&b, server.Obfuscation20)
	for _, c := range clients {
		if !c.Enable {
			continue
		}
		if err := ValidateClient(c); err != nil {
			return "", err
		}
		fmt.Fprintf(&b, "\n[Peer]\n# %s\nPublicKey = %s\n", c.ID, c.PublicKey)
		if c.PresharedKey != "" {
			fmt.Fprintf(&b, "PresharedKey = %s\n", c.PresharedKey)
		}
		fmt.Fprintf(&b, "AllowedIPs = %s\n", c.AllowedIPs)
	}
	return b.String(), nil
}

func RenderClientConfig(server Server, client Client) (string, error) {
	if err := ValidateServer(server); err != nil {
		return "", err
	}
	if err := ValidateClient(client); err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "[Interface]\nPrivateKey = %s\nAddress = %s\n", client.PrivateKey, client.IPv4Address)
	if server.DNS != "" {
		fmt.Fprintf(&b, "DNS = %s\n", server.DNS)
	}
	if server.MTU > 0 {
		fmt.Fprintf(&b, "MTU = %d\n", server.MTU)
	}
	writeObfuscation(&b, server.Obfuscation20)
	fmt.Fprintf(&b, "\n[Peer]\nPublicKey = %s\n", server.PublicKey)
	if client.PresharedKey != "" {
		fmt.Fprintf(&b, "PresharedKey = %s\n", client.PresharedKey)
	}
	if server.Endpoint != "" {
		endpoint := server.Endpoint
		if !strings.Contains(endpoint, ":") {
			endpoint = fmt.Sprintf("%s:%d", endpoint, server.ListenPort)
		}
		fmt.Fprintf(&b, "Endpoint = %s\n", endpoint)
	}
	allowed := client.ClientAllowedIPs
	if allowed == "" {
		allowed = "0.0.0.0/0"
	}
	fmt.Fprintf(&b, "AllowedIPs = %s\n", allowed)
	if client.PersistentKeepalive > 0 {
		fmt.Fprintf(&b, "PersistentKeepalive = %d\n", client.PersistentKeepalive)
	}
	return b.String(), nil
}

func ValidateClient(c Client) error {
	if c.ID == "" || strings.ContainsAny(c.ID, "/\\\x00\r\n\t ") {
		return fmt.Errorf("invalid client id")
	}
	if c.PrivateKey == "" || c.PublicKey == "" {
		return fmt.Errorf("missing client key material")
	}
	if err := validateIPv4CIDR(c.IPv4Address); err != nil {
		return fmt.Errorf("invalid client ipv4Address: %w", err)
	}
	if c.AllowedIPs == "" {
		return fmt.Errorf("missing allowedIPs")
	}
	for _, part := range strings.Split(c.AllowedIPs, ",") {
		if err := validateIPv4CIDR(strings.TrimSpace(part)); err != nil {
			return fmt.Errorf("invalid allowedIPs: %w", err)
		}
	}
	if strings.Contains(c.ClientAllowedIPs, "::") {
		return fmt.Errorf("ipv6 client allowed ips disabled")
	}
	return nil
}

func writeObfuscation(b *strings.Builder, o Obfuscation20) {
	fmt.Fprintf(b, "Jc = %d\nJmin = %d\nJmax = %d\nS1 = %d\nS2 = %d\n", o.Jc, o.Jmin, o.Jmax, o.S1, o.S2)
	if o.S3 > 0 {
		fmt.Fprintf(b, "S3 = %d\n", o.S3)
	}
	if o.S4 > 0 {
		fmt.Fprintf(b, "S4 = %d\n", o.S4)
	}
	fmt.Fprintf(b, "H1 = %s\nH2 = %s\nH3 = %s\nH4 = %s\n", hOrDefault(o.H1, "1"), hOrDefault(o.H2, "2"), hOrDefault(o.H3, "3"), hOrDefault(o.H4, "4"))
	if o.I1 != "" {
		fmt.Fprintf(b, "I1 = %s\n", o.I1)
	}
}

func hOrDefault(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

func MarshalSafeStatus(status SafeStatus) ([]byte, error) {
	return json.Marshal(status)
}
