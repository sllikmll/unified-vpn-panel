package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/util/common"
)

type ConfigProfileService struct{}

var secretProfileKeys = map[string]struct{}{
	"password":     {},
	"pass":         {},
	"secret":       {},
	"privatekey":   {},
	"presharedkey": {},
	"token":        {},
	"apitoken":     {},
	"key":          {},
	"credential":   {},
}

type profileInbound struct {
	Listen         string          `json:"listen"`
	Port           int             `json:"port"`
	Protocol       model.Protocol  `json:"protocol"`
	Settings       json.RawMessage `json:"settings"`
	StreamSettings json.RawMessage `json:"streamSettings"`
	NodeID         *int            `json:"nodeId"`
}

type configProfileDocument struct {
	Inbounds []profileInbound `json:"inbounds"`
}

func (s *ConfigProfileService) GetAll() ([]*model.ConfigProfile, error) {
	var profiles []*model.ConfigProfile
	err := database.GetDB().Order("name asc, id asc").Find(&profiles).Error
	return profiles, err
}

func (s *ConfigProfileService) Get(id int) (*model.ConfigProfile, error) {
	var profile model.ConfigProfile
	if err := database.GetDB().First(&profile, id).Error; err != nil {
		if database.IsNotFound(err) {
			return nil, common.NewError("config profile not found")
		}
		return nil, err
	}
	return &profile, nil
}

func (s *ConfigProfileService) Create(profile *model.ConfigProfile) (*model.ConfigProfile, error) {
	if err := s.prepare(profile); err != nil {
		return nil, err
	}
	if err := database.GetDB().Create(profile).Error; err != nil {
		return nil, err
	}
	return profile, nil
}

func (s *ConfigProfileService) Update(id int, req *model.ConfigProfile) (*model.ConfigProfile, error) {
	existing, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	existing.Name = req.Name
	existing.Description = req.Description
	existing.Enabled = req.Enabled
	existing.Version = req.Version
	existing.Profile = req.Profile
	if err := s.prepare(existing); err != nil {
		return nil, err
	}
	if err := database.GetDB().Save(existing).Error; err != nil {
		return nil, err
	}
	return existing, nil
}

func (s *ConfigProfileService) Clone(id int, name string) (*model.ConfigProfile, error) {
	source, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	cloneName := strings.TrimSpace(name)
	if cloneName == "" {
		cloneName = source.Name + " copy"
	}
	return s.Create(&model.ConfigProfile{
		Name:        cloneName,
		Description: source.Description,
		Enabled:     source.Enabled,
		Version:     source.Version,
		Profile:     source.Profile,
	})
}

func (s *ConfigProfileService) Delete(id int) error {
	res := database.GetDB().Delete(&model.ConfigProfile{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return common.NewError("config profile not found")
	}
	return nil
}

func (s *ConfigProfileService) SetEnabled(id int, enabled bool) error {
	res := database.GetDB().Model(&model.ConfigProfile{}).Where("id = ?", id).Update("enabled", enabled)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return common.NewError("config profile not found")
	}
	return nil
}

func (s *ConfigProfileService) prepare(profile *model.ConfigProfile) error {
	profile.Name = strings.TrimSpace(profile.Name)
	if profile.Name == "" {
		return common.NewError("profile name is required")
	}
	if profile.Version <= 0 {
		profile.Version = 1
	}
	canonical, doc, err := canonicalProfileJSON(profile.Profile)
	if err != nil {
		return err
	}
	if err := rejectSecretKeys(doc); err != nil {
		return err
	}
	if err := s.checkProfilePortConflicts(canonical); err != nil {
		return err
	}
	profile.Profile = canonical
	return nil
}

func canonicalProfileJSON(raw string) (string, any, error) {
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.UseNumber()
	var doc any
	if err := dec.Decode(&doc); err != nil {
		return "", nil, common.NewError("profile must be valid JSON:", err)
	}
	var trailing any
	if err := dec.Decode(&trailing); err == nil {
		return "", nil, common.NewError("profile must contain one JSON document")
	} else if err != io.EOF {
		return "", nil, common.NewError("profile must be valid JSON:", err)
	}
	buf := &bytes.Buffer{}
	enc := json.NewEncoder(buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(doc); err != nil {
		return "", nil, err
	}
	return strings.TrimSpace(buf.String()), doc, nil
}

func rejectSecretKeys(v any) error {
	switch x := v.(type) {
	case map[string]any:
		for key, child := range x {
			normalized := strings.ToLower(strings.ReplaceAll(key, "_", ""))
			normalized = strings.ReplaceAll(normalized, "-", "")
			if _, ok := secretProfileKeys[normalized]; ok {
				return common.NewError("profile templates must not include credential or secret fields:", key)
			}
			if err := rejectSecretKeys(child); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range x {
			if err := rejectSecretKeys(child); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *ConfigProfileService) checkProfilePortConflicts(canonical string) error {
	var doc configProfileDocument
	if err := json.Unmarshal([]byte(canonical), &doc); err != nil {
		return err
	}
	inboundSvc := InboundService{}
	seen := map[string]*model.Inbound{}
	for i, in := range doc.Inbounds {
		if in.Port == 0 {
			continue
		}
		if in.Port < 0 || in.Port > 65535 {
			return common.NewError(fmt.Sprintf("profile inbound %d port must be between 0 and 65535", i))
		}
		settings := string(in.Settings)
		streamSettings := string(in.StreamSettings)
		candidate := &model.Inbound{
			Listen:         in.Listen,
			Port:           in.Port,
			Protocol:       in.Protocol,
			Settings:       settings,
			StreamSettings: streamSettings,
			NodeID:         in.NodeID,
		}
		if candidate.Protocol == "" {
			return common.NewError(fmt.Sprintf("profile inbound %d protocol is required", i))
		}
		if !isConfigProfileProtocol(candidate.Protocol) {
			return common.NewError(fmt.Sprintf("profile inbound %d protocol is not supported: %s", i, candidate.Protocol))
		}
		key := profilePortKey(candidate)
		if prev := seen[key]; prev != nil {
			return common.NewError(fmt.Sprintf("profile inbound %d conflicts with another profile inbound on port %d", i, prev.Port))
		}
		seen[key] = candidate
		conflict, err := inboundSvc.checkPortConflict(candidate, 0)
		if err != nil {
			return err
		}
		if conflict != nil {
			return common.NewError(conflict.String())
		}
	}
	return nil
}

func isConfigProfileProtocol(protocol model.Protocol) bool {
	switch protocol {
	case model.VMESS, model.VLESS, model.Tunnel, model.HTTP, model.Trojan,
		model.Shadowsocks, model.Mixed, model.WireGuard, model.Hysteria, model.MTProto:
		return true
	}
	return false
}

func profilePortKey(inbound *model.Inbound) string {
	listen := inbound.Listen
	if isAnyListen(listen) {
		listen = "*"
	}
	node := "local"
	if inbound.NodeID != nil {
		node = fmt.Sprintf("node-%d", *inbound.NodeID)
	}
	return fmt.Sprintf("%s/%s/%d/%d", node, listen, inbound.Port, inboundTransports(inbound.Protocol, inbound.StreamSettings, inbound.Settings))
}
