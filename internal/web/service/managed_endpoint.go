package service

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"

	"gorm.io/gorm"
)

type ManagedEndpointSource string

const (
	ManagedEndpointSourceLegacy  ManagedEndpointSource = "legacy-inbound"
	ManagedEndpointSourceManaged ManagedEndpointSource = "managed-endpoint"
)

type ManagedTrafficView struct {
	Up   int64 `json:"up" example:"1024"`
	Down int64 `json:"down" example:"2048"`
}

type ManagedHealthView struct {
	Status    model.EndpointStatus `json:"status" example:"active"`
	Message   string               `json:"message"`
	CheckedAt int64                `json:"checkedAt"`
}

type ManagedSecretSummary struct {
	HasSecrets bool     `json:"hasSecrets"`
	Fields     []string `json:"fields"`
}

type ManagedEndpointView struct {
	Id            string                `json:"id" example:"legacy-xray-1"`
	Source        ManagedEndpointSource `json:"source" example:"legacy-inbound"`
	NativeId      int                   `json:"nativeId,omitempty" example:"1"`
	InboundId     *int                  `json:"inboundId,omitempty" example:"1"`
	NodeID        *int                  `json:"nodeId,omitempty" example:"2"`
	RuntimeKind   model.RuntimeKind     `json:"runtimeKind" example:"xray"`
	Protocol      model.ManagedProtocol `json:"protocol" example:"vless"`
	Tag           string                `json:"tag" example:"in-443-tcp"`
	Remark        string                `json:"remark" example:"VLESS-443"`
	Listen        string                `json:"listen"`
	Port          int                   `json:"port" example:"443"`
	Enable        bool                  `json:"enable" example:"true"`
	Status        model.EndpointStatus  `json:"status" example:"active"`
	ClientCount   int                   `json:"clientCount" example:"2"`
	Traffic       ManagedTrafficView    `json:"traffic"`
	Health        ManagedHealthView     `json:"health"`
	SecretSummary ManagedSecretSummary  `json:"secretSummary"`
	NodeName      string                `json:"nodeName,omitempty"`
}

type ManagedEndpointCapability struct {
	RuntimeKind     model.RuntimeKind       `json:"runtimeKind" example:"wireguard"`
	Protocols       []model.ManagedProtocol `json:"protocols"`
	ServerLifecycle bool                    `json:"serverLifecycle"`
	ClientCRUD      bool                    `json:"clientCrud"`
	NativeExport    []string                `json:"nativeExport"`
	Subscription    []string                `json:"subscription"`
	Traffic         bool                    `json:"traffic"`
	Detect          bool                    `json:"detect"`
	FirewallPolicy  bool                    `json:"firewallPolicy"`
}

type ManagedEndpointCapabilities struct {
	RuntimeKinds []ManagedEndpointCapability `json:"runtimeKinds"`
}

type ManagedEndpointService struct{}

func (s ManagedEndpointService) List(userId int) ([]ManagedEndpointView, error) {
	db := database.GetDB()
	var native []model.ManagedEndpoint
	if err := db.Where("user_id = ?", userId).Order("id ASC").Find(&native).Error; err != nil {
		return nil, err
	}
	managedInboundIDs := make(map[int]bool)
	for _, row := range native {
		if row.InboundId != nil {
			managedInboundIDs[*row.InboundId] = true
		}
	}

	var inbounds []model.Inbound
	if err := db.Where("user_id = ?", userId).Order("id ASC").Find(&inbounds).Error; err != nil {
		return nil, err
	}
	legacyInboundIDs := make([]int, 0, len(inbounds))
	for _, inbound := range inbounds {
		if !managedInboundIDs[inbound.Id] {
			legacyInboundIDs = append(legacyInboundIDs, inbound.Id)
		}
	}
	legacyCounts, err := batchLegacyClientCounts(db, userId, legacyInboundIDs)
	if err != nil {
		return nil, err
	}
	nativeIDs := make([]int, 0, len(native))
	for _, endpoint := range native {
		nativeIDs = append(nativeIDs, endpoint.Id)
	}
	nativeCounts, nativeTraffic, nativeSecretKinds, err := batchNativeEndpointProjectionData(db, nativeIDs)
	if err != nil {
		return nil, err
	}
	views := make([]ManagedEndpointView, 0, len(inbounds)+len(native))
	for _, inbound := range inbounds {
		if managedInboundIDs[inbound.Id] {
			continue
		}
		views = append(views, legacyInboundView(inbound, legacyCounts[inbound.Id]))
	}
	for _, endpoint := range native {
		views = append(views, nativeEndpointView(endpoint, nativeCounts[endpoint.Id], nativeTraffic[endpoint.Id], nativeSecretKinds[endpoint.Id]))
	}
	return views, nil
}

func (s ManagedEndpointService) Get(userId int, id string) (*ManagedEndpointView, error) {
	list, err := s.List(userId)
	if err != nil {
		return nil, err
	}
	for i := range list {
		if list[i].Id == id {
			return &list[i], nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

func (ManagedEndpointService) Capabilities() ManagedEndpointCapabilities {
	return ManagedEndpointCapabilities{RuntimeKinds: []ManagedEndpointCapability{
		phase0Capability(model.RuntimeXray, model.ManagedProtocol(model.VLESS), model.ManagedProtocol(model.VMESS), model.ManagedProtocol(model.Trojan), model.ManagedProtocol(model.Shadowsocks), model.ManagedProtocol(model.WireGuard), model.ManagedProtocol(model.Hysteria), model.ManagedProtocol(model.HTTP), model.ManagedProtocol(model.Mixed), model.ManagedProtocol(model.Tunnel)),
		phase0Capability(model.RuntimeMTProto, model.ManagedProtocol(model.MTProto)),
		phase0Capability(model.RuntimeWireGuard, "wireguard"),
		phase0Capability(model.RuntimeAmneziaWG, "amneziawg"),
		phase0Capability(model.RuntimeMieru, "mieru"),
		phase0Capability(model.RuntimeNaiveProxy, "naiveproxy"),
	}}
}

func phase0Capability(kind model.RuntimeKind, protocols ...model.ManagedProtocol) ManagedEndpointCapability {
	return ManagedEndpointCapability{
		RuntimeKind:     kind,
		Protocols:       protocols,
		ServerLifecycle: false,
		ClientCRUD:      false,
		NativeExport:    []string{},
		Subscription:    []string{},
		Traffic:         false,
		Detect:          false,
		FirewallPolicy:  false,
	}
}

func legacyInboundView(inbound model.Inbound, clientCount int) ManagedEndpointView {
	runtimeKind := model.RuntimeXray
	if inbound.Protocol == model.MTProto {
		runtimeKind = model.RuntimeMTProto
	}
	status := model.EndpointActive
	if !inbound.Enable {
		status = model.EndpointDisabled
	}
	return ManagedEndpointView{
		Id:          legacyManagedEndpointID(runtimeKind, inbound.Id),
		Source:      ManagedEndpointSourceLegacy,
		InboundId:   &inbound.Id,
		NodeID:      inbound.NodeID,
		RuntimeKind: runtimeKind,
		Protocol:    model.ManagedProtocol(inbound.Protocol),
		Tag:         inbound.Tag,
		Remark:      inbound.Remark,
		Listen:      inbound.Listen,
		Port:        inbound.Port,
		Enable:      inbound.Enable,
		Status:      status,
		ClientCount: clientCount,
		Traffic:     ManagedTrafficView{Up: inbound.Up, Down: inbound.Down},
		Health:      ManagedHealthView{Status: status},
		SecretSummary: ManagedSecretSummary{
			HasSecrets: inbound.Protocol == model.MTProto,
			Fields:     legacySecretFields(inbound.Protocol),
		},
	}
}

func legacyManagedEndpointID(kind model.RuntimeKind, inboundID int) string {
	return "legacy-" + string(kind) + "-" + strconv.Itoa(inboundID)
}

func legacySecretFields(protocol model.Protocol) []string {
	if protocol == model.MTProto {
		return []string{"clients.secret"}
	}
	return []string{}
}

func batchLegacyClientCounts(db *gorm.DB, userId int, inboundIDs []int) (map[int]int, error) {
	counts := make(map[int]int, len(inboundIDs))
	if len(inboundIDs) == 0 {
		return counts, nil
	}
	var rows []struct {
		InboundId int
		Count     int
	}
	err := db.Table("client_inbounds").
		Select("client_inbounds.inbound_id AS inbound_id, COUNT(*) AS count").
		Joins("JOIN inbounds ON inbounds.id = client_inbounds.inbound_id").
		Where("inbounds.user_id = ? AND client_inbounds.inbound_id IN ?", userId, inboundIDs).
		Group("client_inbounds.inbound_id").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		counts[row.InboundId] = row.Count
	}
	return counts, nil
}

func batchNativeEndpointProjectionData(db *gorm.DB, endpointIDs []int) (map[int]int, map[int]ManagedTrafficView, map[int][]string, error) {
	counts := make(map[int]int, len(endpointIDs))
	traffic := make(map[int]ManagedTrafficView, len(endpointIDs))
	secretKinds := make(map[int][]string, len(endpointIDs))
	if len(endpointIDs) == 0 {
		return counts, traffic, secretKinds, nil
	}

	var countRows []struct {
		EndpointId int
		Count      int
	}
	if err := db.Model(&model.ManagedEndpointClient{}).
		Select("endpoint_id, COUNT(*) AS count").
		Where("endpoint_id IN ?", endpointIDs).
		Group("endpoint_id").
		Scan(&countRows).Error; err != nil {
		return nil, nil, nil, err
	}
	for _, row := range countRows {
		counts[row.EndpointId] = row.Count
	}

	var trafficRows []struct {
		EndpointId int
		Up         int64
		Down       int64
	}
	if err := db.Model(&model.ManagedEndpointClientTraffic{}).
		Select("endpoint_id, COALESCE(SUM(up), 0) AS up, COALESCE(SUM(down), 0) AS down").
		Where("endpoint_id IN ?", endpointIDs).
		Group("endpoint_id").
		Scan(&trafficRows).Error; err != nil {
		return nil, nil, nil, err
	}
	for _, row := range trafficRows {
		traffic[row.EndpointId] = ManagedTrafficView{Up: row.Up, Down: row.Down}
	}

	var secretRows []struct {
		OwnerId    int
		SecretKind string
	}
	if err := db.Model(&model.ManagedSecret{}).
		Select("owner_id, secret_kind").
		Where("owner_type = ? AND owner_id IN ?", "managed_endpoint", endpointIDs).
		Order("secret_kind ASC").
		Scan(&secretRows).Error; err != nil {
		return nil, nil, nil, err
	}
	for _, row := range secretRows {
		secretKinds[row.OwnerId] = append(secretKinds[row.OwnerId], row.SecretKind)
	}
	return counts, traffic, secretKinds, nil
}

func nativeEndpointView(endpoint model.ManagedEndpoint, clientCount int, traffic ManagedTrafficView, secretKinds []string) ManagedEndpointView {
	sort.Strings(secretKinds)
	status := endpoint.Status
	if status == "" {
		if endpoint.Enable {
			status = model.EndpointActive
		} else {
			status = model.EndpointDisabled
		}
	}
	return ManagedEndpointView{
		Id:          fmt.Sprintf("managed-%d", endpoint.Id),
		Source:      ManagedEndpointSourceManaged,
		NativeId:    endpoint.Id,
		InboundId:   endpoint.InboundId,
		NodeID:      endpoint.NodeID,
		RuntimeKind: endpoint.RuntimeKind,
		Protocol:    endpoint.Protocol,
		Tag:         endpoint.Tag,
		Remark:      endpoint.Remark,
		Listen:      endpoint.Listen,
		Port:        endpoint.Port,
		Enable:      endpoint.Enable,
		Status:      status,
		ClientCount: clientCount,
		Traffic:     traffic,
		Health:      ManagedHealthView{Status: status, CheckedAt: endpoint.LastHealthAt},
		SecretSummary: ManagedSecretSummary{
			HasSecrets: len(secretKinds) > 0,
			Fields:     secretKinds,
		},
	}
}

func ParseManagedEndpointID(id string) (string, error) {
	if strings.HasPrefix(id, "managed-") {
		if _, err := strconv.Atoi(strings.TrimPrefix(id, "managed-")); err != nil {
			return "", err
		}
		return id, nil
	}
	parts := strings.Split(id, "-")
	if len(parts) != 3 || parts[0] != "legacy" {
		return "", errors.New("invalid managed endpoint id")
	}
	if _, err := strconv.Atoi(parts[2]); err != nil {
		return "", err
	}
	return id, nil
}
