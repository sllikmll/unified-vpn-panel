package protocolconnections

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"

	"gorm.io/gorm"
)

const (
	MaxRawBytes = 64 * 1024
	startMark   = "# unified-managed-proxies:start"
	endMark     = "# unified-managed-proxies:end"
)

type ProtocolSpec struct {
	Id              string   `json:"id"`
	Label           string   `json:"label"`
	Schemes         []string `json:"schemes"`
	MihomoSupported bool     `json:"mihomoSupported"`
}

var Protocols = []ProtocolSpec{
	{Id: "wireguard", Label: "WireGuard", Schemes: []string{"wireguard://"}, MihomoSupported: true},
	{Id: "amnezia", Label: "Amnezia", Schemes: []string{"awg://", "amneziawg://"}, MihomoSupported: true},
	{Id: "hysteria2", Label: "Hysteria2", Schemes: []string{"hysteria2://", "hy2://", "hysteria://"}, MihomoSupported: true},
	{Id: "vless", Label: "VLESS", Schemes: []string{"vless://"}, MihomoSupported: true},
	{Id: "trojan", Label: "Trojan", Schemes: []string{"trojan://"}, MihomoSupported: true},
	{Id: "mieru", Label: "Meiru", Schemes: []string{"mieru://", "mierus://"}, MihomoSupported: false},
	{Id: "naiveproxy", Label: "NaiveProxy", Schemes: []string{"naive://", "naive+https://", "https://"}, MihomoSupported: true},
	{Id: "vmess", Label: "VMess", Schemes: []string{"vmess://"}, MihomoSupported: true},
	{Id: "shadowsocks", Label: "Shadowsocks", Schemes: []string{"ss://"}, MihomoSupported: true},
}

var protocolSet = func() map[string]ProtocolSpec {
	out := make(map[string]ProtocolSpec, len(Protocols))
	for _, p := range Protocols {
		out[p.Id] = p
	}
	return out
}()

type ImportRequest struct {
	Protocol  string   `json:"protocol" example:"trojan"`
	Name      string   `json:"name" example:"de-frankfurt"`
	Content   string   `json:"content" example:"trojan://password@example.com:443#de-frankfurt"`
	Link      string   `json:"link,omitempty"`
	Config    string   `json:"config,omitempty"`
	Selectors []string `json:"selectors"`
}

type UpdateRequest struct {
	Name      *string   `json:"name,omitempty"`
	Enabled   *bool     `json:"enabled,omitempty"`
	Selectors *[]string `json:"selectors,omitempty"`
}

type ConnectionView struct {
	model.ProtocolConnection
	HasRaw          bool     `json:"hasRaw"`
	MihomoSupported bool     `json:"mihomoSupported"`
	ProtocolLabel   string   `json:"protocolLabel"`
	UsedBySelectors []string `json:"usedBySelectors"`
}

type ListResponse struct {
	Connections []ConnectionView `json:"connections"`
	Count       int              `json:"count"`
	Protocols   []ProtocolSpec   `json:"protocols"`
}

type Service struct {
	db *gorm.DB
}

func NewService(db *gorm.DB) *Service {
	if db == nil {
		db = database.GetDB()
	}
	return &Service{db: db}
}

func (s *Service) List(protocol string) (*ListResponse, error) {
	var rows []model.ProtocolConnection
	q := s.db.Order("created_at asc, name asc")
	if protocol = strings.TrimSpace(strings.ToLower(protocol)); protocol != "" {
		if !IsAllowedProtocol(protocol) {
			return nil, fmt.Errorf("unsupported protocol")
		}
		q = q.Where("protocol = ?", protocol)
	}
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	views := make([]ConnectionView, 0, len(rows))
	for _, row := range rows {
		views = append(views, publicView(row, false))
	}
	return &ListResponse{Connections: views, Count: len(views), Protocols: Protocols}, nil
}

func (s *Service) Get(id string, reveal bool) (*ConnectionView, error) {
	var row model.ProtocolConnection
	if err := s.db.First(&row, "id = ?", strings.TrimSpace(id)).Error; err != nil {
		return nil, err
	}
	view := publicView(row, reveal)
	return &view, nil
}

func (s *Service) Import(req ImportRequest) (*ConnectionView, bool, error) {
	content := firstNonEmpty(req.Content, req.Link, req.Config)
	conn, err := ParseConnection(req.Protocol, content, req.Name)
	if err != nil {
		return nil, false, err
	}
	conn.Selectors = dedupeStrings(req.Selectors)
	var existing model.ProtocolConnection
	err = s.db.Where("name = ?", conn.Name).First(&existing).Error
	if err == nil && existing.Id != conn.Id {
		return nil, false, fmt.Errorf("duplicate connection name")
	}
	replaced := false
	if err == nil {
		conn.CreatedAt = existing.CreatedAt
		replaced = true
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, err
	}
	if err := s.db.Save(conn).Error; err != nil {
		return nil, false, err
	}
	view := publicView(*conn, false)
	return &view, replaced, nil
}

func (s *Service) Update(id string, req UpdateRequest) (*ConnectionView, error) {
	var row model.ProtocolConnection
	if err := s.db.First(&row, "id = ?", strings.TrimSpace(id)).Error; err != nil {
		return nil, err
	}
	if req.Name != nil {
		name, err := normalizeName(*req.Name)
		if err != nil {
			return nil, err
		}
		if name != row.Name {
			var n int64
			if err := s.db.Model(&model.ProtocolConnection{}).Where("name = ? AND id <> ?", name, row.Id).Count(&n).Error; err != nil {
				return nil, err
			}
			if n > 0 {
				return nil, fmt.Errorf("duplicate connection name")
			}
			row.Name = name
			row.MihomoYAML = replaceYAMLName(row.MihomoYAML, name)
			row.MihomoJSON = replaceJSONName(row.MihomoJSON, name)
		}
	}
	if req.Enabled != nil {
		row.Enabled = *req.Enabled
	}
	if req.Selectors != nil {
		row.Selectors = dedupeStrings(*req.Selectors)
	}
	if err := s.db.Save(&row).Error; err != nil {
		return nil, err
	}
	view := publicView(row, false)
	return &view, nil
}

func (s *Service) Delete(id string) error {
	res := s.db.Delete(&model.ProtocolConnection{}, "id = ?", strings.TrimSpace(id))
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (s *Service) ManagedBlock() (string, error) {
	var rows []model.ProtocolConnection
	if err := s.db.Order("created_at asc, name asc").Find(&rows).Error; err != nil {
		return "", err
	}
	body := strings.Builder{}
	for _, row := range rows {
		if !row.Enabled || !protocolSet[row.Protocol].MihomoSupported || !strings.HasPrefix(strings.TrimSpace(row.MihomoYAML), "- name:") {
			continue
		}
		body.WriteString("  # ")
		body.WriteString(row.Protocol)
		body.WriteString(" / ")
		body.WriteString(row.Id)
		body.WriteByte('\n')
		for _, line := range strings.Split(strings.TrimRight(row.MihomoYAML, "\n"), "\n") {
			body.WriteString("  ")
			body.WriteString(line)
			body.WriteByte('\n')
		}
	}
	text := "  " + startMark + "\n" + body.String() + "  " + endMark + "\n"
	return text, nil
}

func (s *Service) ExportYAML() (string, error) {
	block, err := s.ManagedBlock()
	if err != nil {
		return "", err
	}
	return "proxies:\n" + block, nil
}

func publicView(row model.ProtocolConnection, reveal bool) ConnectionView {
	spec := protocolSet[row.Protocol]
	view := ConnectionView{
		ProtocolConnection: row,
		HasRaw:             row.RawSource != "",
		MihomoSupported:    spec.MihomoSupported,
		ProtocolLabel:      firstNonEmpty(spec.Label, row.Protocol),
		UsedBySelectors:    []string{},
	}
	if !reveal {
		view.RawSource = ""
		view.MihomoJSON = redactJSONSecrets(view.MihomoJSON)
		view.MihomoYAML = Redact(view.MihomoYAML)
	}
	return view
}

func ParseConnection(protocol, raw, customName string) (*model.ProtocolConnection, error) {
	text := strings.TrimSpace(raw)
	if text == "" {
		return nil, fmt.Errorf("empty connection content")
	}
	if len([]byte(text)) > MaxRawBytes {
		return nil, fmt.Errorf("connection content exceeds %d bytes", MaxRawBytes)
	}
	proto := strings.ToLower(strings.TrimSpace(protocol))
	if proto == "" || !IsAllowedProtocol(proto) {
		proto = DetectProtocol(text)
	}
	if !IsAllowedProtocol(proto) {
		return nil, fmt.Errorf("unsupported protocol")
	}
	name, err := normalizeOptionalName(customName)
	if err != nil {
		return nil, err
	}
	proxy, supported, err := parseByProtocol(proto, text, name)
	if err != nil {
		return nil, fmt.Errorf("parse %s failed: %w", proto, err)
	}
	if name != "" {
		proxy["name"] = name
	}
	if proxy["name"] == nil || strings.TrimSpace(fmt.Sprint(proxy["name"])) == "" {
		proxy["name"] = protocolSet[proto].Label
	}
	finalName, err := normalizeName(fmt.Sprint(proxy["name"]))
	if err != nil {
		return nil, err
	}
	proxy["name"] = finalName
	yamlText := ""
	jsonText := ""
	if supported {
		yamlText = yamlProxy(proxy)
		b, err := json.Marshal(proxy)
		if err != nil {
			return nil, err
		}
		jsonText = string(b)
	} else {
		yamlText = fmt.Sprintf("# Mieru connection %s is stored, but current Mihomo injection is disabled.\n", finalName)
	}
	return &model.ProtocolConnection{
		Id:         connectionID(proto, finalName, text),
		Protocol:   proto,
		Name:       finalName,
		Enabled:    true,
		RawSource:  text,
		MihomoJSON: jsonText,
		MihomoYAML: yamlText,
	}, nil
}

func IsAllowedProtocol(protocol string) bool {
	_, ok := protocolSet[strings.ToLower(strings.TrimSpace(protocol))]
	return ok
}

func DetectProtocol(text string) string {
	s := strings.ToLower(strings.TrimSpace(text))
	if strings.Contains(s, "[interface]") && strings.Contains(s, "[peer]") {
		if strings.Contains(s, "jc") || strings.Contains(s, "jmin") || strings.Contains(s, "s1") || strings.Contains(s, "h1") {
			return "amnezia"
		}
		return "wireguard"
	}
	for _, proto := range Protocols {
		for _, scheme := range proto.Schemes {
			if strings.HasPrefix(s, strings.ToLower(scheme)) {
				if proto.Id == "naiveproxy" && scheme == "https://" && !strings.Contains(s, "@") {
					continue
				}
				return proto.Id
			}
		}
	}
	return ""
}

func parseByProtocol(proto, raw, name string) (map[string]any, bool, error) {
	switch proto {
	case "wireguard", "amnezia":
		proxy, err := parseWireGuard(wireguardFromDataURL(raw))
		return proxy, true, err
	case "hysteria2":
		return parseHysteria2(raw, name)
	case "vless":
		return parseVLESS(raw, name)
	case "trojan":
		return parseTrojan(raw, name)
	case "mieru":
		if !strings.HasPrefix(strings.ToLower(raw), "mieru://") && !strings.HasPrefix(strings.ToLower(raw), "mierus://") {
			return nil, false, fmt.Errorf("not a Mieru URI")
		}
		u, _ := url.Parse(raw)
		return map[string]any{"name": firstNonEmpty(name, unescape(u.Fragment), u.Hostname(), "Mieru")}, false, nil
	case "naiveproxy":
		return parseNaive(raw, name)
	case "vmess":
		return parseVMess(raw, name)
	case "shadowsocks":
		return parseShadowsocks(raw, name)
	default:
		return nil, false, fmt.Errorf("unsupported protocol")
	}
}

func parseVLESS(raw, customName string) (map[string]any, bool, error) {
	u, err := url.Parse(raw)
	if err != nil || strings.ToLower(u.Scheme) != "vless" || u.User == nil || u.Hostname() == "" {
		return nil, true, fmt.Errorf("invalid vless link")
	}
	q := u.Query()
	netw := strings.ToLower(firstNonEmpty(q.Get("type"), "tcp"))
	p := baseProxy(customName, u, "vless")
	user := u.User.Username()
	if strings.Contains(user, ":") {
		parts := strings.SplitN(user, ":", 2)
		user = parts[0]
		if q.Get("flow") == "" {
			p["flow"] = normalizeVLESSFlow(parts[1])
		}
	}
	p["uuid"] = unescape(user)
	if flow := normalizeVLESSFlow(q.Get("flow")); flow != "" {
		p["flow"] = flow
	}
	p["encryption"] = firstNonEmpty(unescape(q.Get("encryption")), "")
	p["network"] = netw
	p["udp"] = true
	p["packet-encoding"] = "xudp"
	addTLSOptions(p, q, q.Get("security"), true)
	addTransportOptions(p, netw, q, q.Get("sni"))
	return p, true, nil
}

func parseTrojan(raw, customName string) (map[string]any, bool, error) {
	u, err := url.Parse(raw)
	if err != nil || strings.ToLower(u.Scheme) != "trojan" || u.User == nil || u.Hostname() == "" {
		return nil, true, fmt.Errorf("invalid trojan link")
	}
	q := u.Query()
	netw := strings.ToLower(firstNonEmpty(q.Get("type"), "tcp"))
	if netw == "xhttp" {
		return nil, true, fmt.Errorf("xhttp transport is supported by Mihomo only for VLESS proxies")
	}
	p := baseProxy(customName, u, "trojan")
	p["password"] = unescape(u.User.Username())
	p["udp"] = true
	addTLSOptions(p, q, firstNonEmpty(q.Get("security"), "tls"), false)
	p["network"] = netw
	addTransportOptions(p, netw, q, "")
	return p, true, nil
}

func parseHysteria2(raw, customName string) (map[string]any, bool, error) {
	u, err := url.Parse(raw)
	scheme := ""
	if u != nil {
		scheme = strings.ToLower(u.Scheme)
	}
	if err != nil || (scheme != "hysteria2" && scheme != "hy2" && scheme != "hysteria") || u.User == nil || u.Hostname() == "" {
		return nil, true, fmt.Errorf("invalid hysteria2 link")
	}
	p := baseProxy(customName, u, "hysteria2")
	pass := unescape(u.User.Username())
	if pwd, ok := u.User.Password(); ok && pwd != "" {
		pass += ":" + unescape(pwd)
	}
	if pass == "" {
		return nil, true, fmt.Errorf("invalid hysteria2 link")
	}
	q := u.Query()
	p["password"] = pass
	p["udp"] = true
	p["fast-open"] = true
	p["alpn"] = csvList(firstNonEmpty(q.Get("alpn"), "h3"))
	for _, key := range []string{"up", "down", "obfs"} {
		if v := unescape(q.Get(key)); v != "" {
			p[key] = v
		}
	}
	if v := firstNonEmpty(q.Get("obfs-password"), q.Get("obfs_password"), q.Get("obfsPassword")); v != "" {
		p["obfs-password"] = unescape(v)
	}
	if sni := firstNonEmpty(q.Get("sni"), q.Get("peer")); sni != "" {
		p["sni"] = unescape(sni)
	}
	if queryBool(q, "allowInsecure", "insecure") {
		p["skip-cert-verify"] = true
	}
	return p, true, nil
}

func parseNaive(raw, customName string) (map[string]any, bool, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User == nil {
		return nil, true, fmt.Errorf("invalid NaiveProxy URI")
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "naive" && scheme != "naive+https" && scheme != "https" {
		return nil, true, fmt.Errorf("not a NaiveProxy URI")
	}
	password, _ := u.User.Password()
	p := baseProxy(customName, u, "http")
	p["username"] = unescape(u.User.Username())
	p["password"] = unescape(password)
	p["tls"] = true
	p["sni"] = u.Hostname()
	if p["username"] == "" {
		return nil, true, fmt.Errorf("invalid NaiveProxy URI")
	}
	return p, true, nil
}

func parseVMess(raw, customName string) (map[string]any, bool, error) {
	payload := strings.TrimPrefix(strings.TrimSpace(raw), "vmess://")
	decoded, err := decodeBase64(payload)
	if err != nil {
		return nil, true, fmt.Errorf("invalid vmess payload")
	}
	var data map[string]any
	if err := json.Unmarshal(decoded, &data); err != nil {
		return nil, true, fmt.Errorf("invalid vmess payload")
	}
	server := fmt.Sprint(firstAny(data, "add", "host"))
	u := &url.URL{Host: net.JoinHostPort(server, fmt.Sprint(firstAnyDefault(data, "443", "port"))), Fragment: fmt.Sprint(firstAny(data, "ps"))}
	p := baseProxy(customName, u, "vmess")
	p["uuid"] = fmt.Sprint(data["id"])
	p["alterId"] = intFromAny(data["aid"], 0)
	p["cipher"] = firstNonEmpty(fmt.Sprint(data["scy"]), "auto")
	p["udp"] = true
	netw := strings.ToLower(firstNonEmpty(fmt.Sprint(data["net"]), "tcp"))
	if netw == "xhttp" {
		return nil, true, fmt.Errorf("xhttp transport is supported by Mihomo only for VLESS proxies")
	}
	p["network"] = netw
	q := url.Values{}
	for k, v := range data {
		q.Set(k, fmt.Sprint(v))
	}
	addTLSOptions(p, q, fmt.Sprint(data["tls"]), true)
	addTransportOptions(p, netw, q, "")
	return p, true, nil
}

func parseShadowsocks(raw, customName string) (map[string]any, bool, error) {
	body := strings.TrimPrefix(strings.TrimSpace(raw), "ss://")
	frag := ""
	if i := strings.Index(body, "#"); i >= 0 {
		frag, body = body[i+1:], body[:i]
	}
	if i := strings.Index(body, "?"); i >= 0 {
		body = body[:i]
	}
	method, pass, host, port := "", "", "", 0
	if strings.Contains(body, "@") {
		left, right, _ := strings.Cut(body, "@")
		if !strings.Contains(left, ":") {
			if b, err := decodeBase64(left); err == nil {
				left = string(b)
			}
		}
		method, pass, _ = strings.Cut(left, ":")
		host, port = splitHostPort(right, 0)
	} else if b, err := decodeBase64(body); err == nil {
		creds, hp, _ := strings.Cut(string(b), "@")
		method, pass, _ = strings.Cut(creds, ":")
		host, port = splitHostPort(hp, 0)
	}
	if method == "" || pass == "" || host == "" || port == 0 {
		return nil, true, fmt.Errorf("invalid ss link")
	}
	p := map[string]any{"name": firstNonEmpty(customName, unescape(frag), host), "type": "ss", "server": host, "port": port, "cipher": method, "password": pass, "udp": true}
	return p, true, nil
}

func parseWireGuard(raw string) (map[string]any, error) {
	iface, peer := map[string]string{}, map[string]string{}
	section := ""
	for _, rawLine := range strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.ToLower(strings.Trim(line, "[]"))
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(k), "-", ""))
		val := stripInlineComment(v)
		if section == "interface" {
			iface[key] = val
		} else if section == "peer" {
			peer[key] = val
		}
	}
	if iface["privatekey"] == "" || peer["publickey"] == "" || peer["endpoint"] == "" {
		return nil, fmt.Errorf("invalid WireGuard config: missing mandatory keys")
	}
	host, port := splitHostPort(peer["endpoint"], 0)
	p := map[string]any{"name": firstNonEmpty(peer["name"], iface["name"], host), "type": "wireguard", "server": host, "port": port, "private-key": iface["privatekey"], "public-key": peer["publickey"], "udp": true}
	if ip4, ip6 := splitWGAddresses(iface["address"]); ip4 != "" || ip6 != "" {
		if ip4 != "" {
			p["ip"] = ip4
		}
		if ip6 != "" {
			p["ipv6"] = ip6
		}
	}
	if v := firstNonEmpty(peer["presharedkey"], peer["pre-shared-key"]); v != "" {
		p["pre-shared-key"] = v
	}
	if v := firstNonEmpty(peer["reserved"], peer["clientid"], peer["client-id"]); v != "" {
		p["reserved"] = v
	}
	if v := iface["dns"]; v != "" {
		p["dns"] = csvList(v)
		p["remote-dns-resolve"] = true
	}
	if v := iface["mtu"]; v != "" {
		p["mtu"] = intString(v)
	}
	if v := peer["persistentkeepalive"]; v != "" {
		p["persistent-keepalive"] = intString(v)
	}
	if v := firstNonEmpty(peer["allowedips"], "0.0.0.0/0, ::/0"); v != "" {
		p["allowed-ips"] = csvList(v)
	}
	amz := map[string]any{}
	for _, k := range []string{"jc", "jmin", "jmax", "s1", "s2", "s3", "s4", "h1", "h2", "h3", "h4", "i1", "i2", "i3", "i4", "i5", "j1", "j2", "j3", "itime"} {
		if v := firstNonEmpty(peer[k], iface[k]); v != "" {
			amz[k] = intString(strings.Trim(v, `"'`))
		}
	}
	if len(amz) > 0 {
		p["amnezia-wg-option"] = amz
	}
	return p, nil
}

func baseProxy(customName string, u *url.URL, typ string) map[string]any {
	host, port := u.Hostname(), 443
	if u.Port() != "" {
		port, _ = strconv.Atoi(u.Port())
	}
	return map[string]any{"name": firstNonEmpty(customName, unescape(u.Fragment), host, typ), "type": typ, "server": host, "port": port}
}

func addTLSOptions(p map[string]any, q url.Values, security string, servername bool) {
	sec := strings.ToLower(security)
	if sec != "tls" && sec != "reality" {
		return
	}
	p["tls"] = true
	p["tfo"] = true
	if sni := firstNonEmpty(q.Get("sni"), q.Get("peer"), q.Get("host")); sni != "" {
		if servername {
			p["servername"] = unescape(sni)
		} else {
			p["sni"] = unescape(sni)
		}
	}
	if alpn := csvList(q.Get("alpn")); len(alpn) > 0 {
		p["alpn"] = alpn
	}
	if queryBool(q, "allowInsecure", "insecure") {
		p["skip-cert-verify"] = true
	}
	if sec == "reality" {
		ro := map[string]any{}
		if v := firstNonEmpty(q.Get("pbk"), q.Get("publicKey"), q.Get("public-key"), q.Get("public_key")); v != "" {
			ro["public-key"] = unescape(v)
		}
		if v := firstNonEmpty(q.Get("sid"), q.Get("shortId"), q.Get("short-id"), q.Get("short_id"), q.Get("shortid")); v != "" {
			ro["short-id"] = unescape(v)
		}
		if queryBool(q, "support-x25519mlkem768", "supportX25519MLKEM768", "support_x25519mlkem768") {
			ro["support-x25519mlkem768"] = true
		}
		if v := q.Get("spx"); v != "" {
			ro["spider-x"] = unescape(v)
		}
		p["reality-opts"] = ro
	}
	p["client-fingerprint"] = firstNonEmpty(q.Get("fp"), "chrome")
}

func addTransportOptions(p map[string]any, netw string, q url.Values, fallbackHost string) {
	switch netw {
	case "xhttp":
		opts := map[string]any{"path": firstNonEmpty(unescape(q.Get("path")), "/")}
		if h := firstNonEmpty(unescape(q.Get("host")), fallbackHost); h != "" {
			opts["host"] = h
		}
		if m := unescape(q.Get("mode")); m != "" {
			opts["mode"] = m
		}
		p["xhttp-opts"] = opts
	case "ws":
		opts := map[string]any{"path": firstNonEmpty(unescape(q.Get("path")), "/")}
		if h := unescape(q.Get("host")); h != "" {
			opts["headers"] = map[string]any{"Host": h}
		}
		p["ws-opts"] = opts
	case "grpc":
		if s := firstNonEmpty(q.Get("serviceName"), q.Get("service_name"), q.Get("path")); s != "" {
			p["grpc-opts"] = map[string]any{"grpc-service-name": unescape(s)}
		}
	case "httpupgrade":
		opts := map[string]any{"path": firstNonEmpty(unescape(q.Get("path")), "/")}
		if h := unescape(q.Get("host")); h != "" {
			opts["headers"] = map[string]any{"Host": h}
		}
		p["http-upgrade-opts"] = opts
	}
}

func yamlProxy(p map[string]any) string {
	order := []string{"name", "type", "server", "port", "uuid", "alterId", "cipher", "password", "username", "flow", "encryption", "network", "udp", "packet-encoding", "tls", "tfo", "servername", "sni", "alpn", "client-fingerprint", "skip-cert-verify", "reality-opts", "xhttp-opts", "ws-opts", "grpc-opts", "http-upgrade-opts", "fast-open", "up", "down", "obfs", "obfs-password", "ip", "ipv6", "private-key", "public-key", "pre-shared-key", "reserved", "dns", "remote-dns-resolve", "mtu", "persistent-keepalive", "allowed-ips", "amnezia-wg-option"}
	var b strings.Builder
	for i, k := range order {
		v, ok := p[k]
		if !ok || emptyValue(v) {
			continue
		}
		if i == 0 {
			b.WriteString("- ")
			writeYAMLKV(&b, 0, k, v)
		} else {
			writeYAMLKV(&b, 2, k, v)
		}
	}
	return b.String()
}

func writeYAMLKV(b *strings.Builder, indent int, key string, v any) {
	pad := strings.Repeat(" ", indent)
	switch val := v.(type) {
	case bool:
		fmt.Fprintf(b, "%s%s: %v\n", pad, key, val)
	case int:
		fmt.Fprintf(b, "%s%s: %d\n", pad, key, val)
	case []string:
		fmt.Fprintf(b, "%s%s:\n", pad, key)
		for _, item := range val {
			fmt.Fprintf(b, "%s  - %s\n", pad, yamlStr(item))
		}
	case map[string]any:
		fmt.Fprintf(b, "%s%s:\n", pad, key)
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			writeYAMLKV(b, indent+2, k, val[k])
		}
	default:
		fmt.Fprintf(b, "%s%s: %s\n", pad, key, yamlStr(fmt.Sprint(val)))
	}
}

var yamlNeedsQuote = regexp.MustCompile(`[\s:#\[\]{}&,*>!%` + "`" + `"'|@?]`)

func yamlStr(v string) string {
	s := strings.ReplaceAll(strings.ReplaceAll(v, "\r", ""), "\n", " ")
	low := strings.ToLower(strings.TrimSpace(s))
	if s == "" || yamlNeedsQuote.MatchString(s) || strings.Contains("-?:&*", s[:min(1, len(s))]) || isYAMLAmbiguous(low) {
		return "'" + strings.ReplaceAll(s, "'", "''") + "'"
	}
	if _, err := strconv.ParseFloat(s, 64); err == nil {
		return "'" + strings.ReplaceAll(s, "'", "''") + "'"
	}
	return s
}

func isYAMLAmbiguous(s string) bool {
	switch s {
	case "null", "~", "true", "false", "yes", "no", "on", "off":
		return true
	}
	return false
}

func normalizeName(name string) (string, error) {
	n := strings.TrimSpace(name)
	if n == "" {
		return "", fmt.Errorf("name is required")
	}
	if len([]rune(n)) > 128 {
		return "", fmt.Errorf("name is too long")
	}
	if strings.ContainsAny(n, "\r\n\t") {
		return "", fmt.Errorf("name contains invalid control characters")
	}
	return n, nil
}

func normalizeOptionalName(name string) (string, error) {
	if strings.TrimSpace(name) == "" {
		return "", nil
	}
	return normalizeName(name)
}

func connectionID(protocol, name, raw string) string {
	sum := sha256.Sum256([]byte(protocol + "\n" + name + "\n" + raw))
	return slug(protocol) + "-" + slug(name) + "-" + hex.EncodeToString(sum[:])[:12]
}

func slug(s string) string {
	re := regexp.MustCompile(`[^0-9A-Za-z._-]+`)
	out := strings.Trim(re.ReplaceAllString(s, "-"), "-._")
	if out == "" {
		return "proxy"
	}
	if len(out) > 64 {
		return out[:64]
	}
	return out
}

func Redact(text string) string {
	out := regexp.MustCompile(`(?im)^(\s*(?:password|private-key|presharedkey|pre-shared-key|uuid|token|secret)\s*[:=]\s*)(.+)$`).ReplaceAllString(text, `${1}<redacted>`)
	out = regexp.MustCompile(`(?i)(://)[^/@\s]+@`).ReplaceAllString(out, `${1}<redacted>@`)
	return out
}

func redactJSONSecrets(raw string) string {
	var m map[string]any
	if json.Unmarshal([]byte(raw), &m) != nil {
		return Redact(raw)
	}
	for _, key := range []string{"password", "private-key", "pre-shared-key", "uuid", "token", "secret", "username"} {
		if _, ok := m[key]; ok {
			m[key] = "<redacted>"
		}
	}
	if nested, ok := m["reality-opts"].(map[string]any); ok {
		if _, ok := nested["public-key"]; ok {
			nested["public-key"] = "<redacted>"
		}
	}
	b, _ := json.Marshal(m)
	return string(b)
}

func wireguardFromDataURL(link string) string {
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(link)), "wireguard://") {
		return link
	}
	payload := strings.TrimPrefix(strings.TrimSpace(link), "wireguard://")
	payload = strings.Split(strings.Split(payload, "#")[0], "?")[0]
	if decoded, err := decodeBase64(payload); err == nil {
		return string(decoded)
	}
	return link
}

func decodeBase64(s string) ([]byte, error) {
	clean := strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(s, "\n", ""), "\r", ""))
	if m := len(clean) % 4; m != 0 {
		clean += strings.Repeat("=", 4-m)
	}
	if b, err := base64.URLEncoding.DecodeString(clean); err == nil {
		return b, nil
	}
	return base64.StdEncoding.DecodeString(clean)
}

func splitHostPort(value string, fallback int) (string, int) {
	host, portText, err := net.SplitHostPort(value)
	if err != nil {
		idx := strings.LastIndex(value, ":")
		if idx < 0 {
			return strings.Trim(value, "[]"), fallback
		}
		host, portText = value[:idx], value[idx+1:]
	}
	port, _ := strconv.Atoi(portText)
	if port == 0 {
		port = fallback
	}
	return strings.Trim(host, "[]"), port
}

func splitWGAddresses(value string) (string, string) {
	var ip4, ip6 string
	for _, part := range strings.Split(value, ",") {
		addr := strings.TrimSpace(strings.Split(part, "/")[0])
		if addr == "" {
			continue
		}
		if strings.Contains(addr, ":") {
			ip6 = addr
		} else {
			ip4 = addr
		}
	}
	return ip4, ip6
}

func csvList(value string) []string {
	var out []string
	for _, item := range strings.Split(unescape(value), ",") {
		if v := strings.TrimSpace(item); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func dedupeStrings(values []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, value := range values {
		item := strings.TrimSpace(value)
		if item != "" && !seen[item] {
			out = append(out, item)
			seen[item] = true
		}
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func firstAny(data map[string]any, keys ...string) any {
	for _, k := range keys {
		if v, ok := data[k]; ok && fmt.Sprint(v) != "" {
			return v
		}
	}
	return ""
}

func firstAnyDefault(data map[string]any, def string, keys ...string) any {
	if v := firstAny(data, keys...); fmt.Sprint(v) != "" {
		return v
	}
	return def
}

func unescape(s string) string {
	v, err := url.QueryUnescape(s)
	if err != nil {
		return s
	}
	return v
}

func intFromAny(v any, def int) int {
	i, err := strconv.Atoi(fmt.Sprint(v))
	if err != nil {
		return def
	}
	return i
}

func intString(v string) any {
	i, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return strings.TrimSpace(v)
	}
	return i
}

func stripInlineComment(v string) string {
	return regexp.MustCompile(`\s+[;#].*$`).ReplaceAllString(strings.TrimSpace(v), "")
}

func normalizeVLESSFlow(v string) string {
	flow := strings.TrimSpace(unescape(v))
	if flow == "xtls-rprx-vision" || strings.HasPrefix(flow, "xtls-rprx-vision-") {
		return "xtls-rprx-vision"
	}
	return flow
}

func queryBool(q url.Values, keys ...string) bool {
	for _, k := range keys {
		switch strings.ToLower(strings.TrimSpace(q.Get(k))) {
		case "1", "true", "yes", "on":
			return true
		}
	}
	return false
}

func emptyValue(v any) bool {
	switch val := v.(type) {
	case nil:
		return true
	case string:
		return val == ""
	case []string:
		return len(val) == 0
	case map[string]any:
		return len(val) == 0
	}
	return false
}

func replaceYAMLName(yml, name string) string {
	re := regexp.MustCompile(`(?m)^-\s+name:\s*.*$`)
	return re.ReplaceAllString(yml, "- name: "+yamlStr(name))
}

func replaceJSONName(raw, name string) string {
	var m map[string]any
	if json.Unmarshal([]byte(raw), &m) != nil {
		return raw
	}
	m["name"] = name
	b, _ := json.Marshal(m)
	return string(b)
}
