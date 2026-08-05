package service

import (
	"encoding/json"
	"strings"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/util/common"
)

// NodeView is the browser/API read contract for nodes. Credentials are
// write-only: responses expose only whether a node has a token configured.
type NodeView struct {
	Id                  int      `json:"id" example:"1"`
	Name                string   `json:"name" example:"edge-1"`
	Remark              string   `json:"remark" example:"Primary edge"`
	Scheme              string   `json:"scheme" example:"https"`
	Address             string   `json:"address" example:"node.example.com"`
	Port                int      `json:"port" example:"2053"`
	BasePath            string   `json:"basePath" example:"/"`
	HasApiToken         bool     `json:"hasApiToken" example:"true"`
	Enable              bool     `json:"enable" example:"true"`
	AllowPrivateAddress bool     `json:"allowPrivateAddress" example:"false"`
	TlsVerifyMode       string   `json:"tlsVerifyMode" example:"verify"`
	PinnedCertSha256    string   `json:"pinnedCertSha256" example:""`
	InboundSyncMode     string   `json:"inboundSyncMode" example:"all"`
	InboundTags         []string `json:"inboundTags" example:"[\"in-443-tcp\"]"`
	OutboundTag         string   `json:"outboundTag" example:"direct"`
	Guid                string   `json:"guid" example:"node-guid"`
	RuntimeCapabilities []string `json:"runtimeCapabilities"`
	Status              string   `json:"status" example:"online"`
	LastHeartbeat       int64    `json:"lastHeartbeat" example:"1700000000"`
	LatencyMs           int      `json:"latencyMs" example:"42"`
	XrayVersion         string   `json:"xrayVersion" example:"25.10.31"`
	PanelVersion        string   `json:"panelVersion" example:"v0.0.1"`
	CpuPct              float64  `json:"cpuPct" example:"12.5"`
	CpuCores            int      `json:"cpuCores" example:"4"`
	LogicalPro          int      `json:"logicalPro" example:"8"`
	CpuSpeedMhz         float64  `json:"cpuSpeedMhz" example:"2396.4"`
	MemCurrent          uint64   `json:"memCurrent" example:"1073741824"`
	MemTotal            uint64   `json:"memTotal" example:"2147483648"`
	MemPct              float64  `json:"memPct" example:"45.2"`
	SwapCurrent         uint64   `json:"swapCurrent" example:"0"`
	SwapTotal           uint64   `json:"swapTotal" example:"0"`
	DiskCurrent         uint64   `json:"diskCurrent" example:"10737418240"`
	DiskTotal           uint64   `json:"diskTotal" example:"21474836480"`
	UptimeSecs          uint64   `json:"uptimeSecs" example:"86400"`
	NetUp               uint64   `json:"netUp" example:"2097152"`
	NetDown             uint64   `json:"netDown" example:"1048576"`
	NetTrafficSent      uint64   `json:"netTrafficSent" example:"104857600"`
	NetTrafficRecv      uint64   `json:"netTrafficRecv" example:"209715200"`
	TcpCount            int      `json:"tcpCount" example:"128"`
	UdpCount            int      `json:"udpCount" example:"16"`
	AppStatsMem         uint64   `json:"appStatsMem" example:"67108864"`
	AppStatsThreads     int      `json:"appStatsThreads" example:"12"`
	AppStatsUptime      uint64   `json:"appStatsUptime" example:"3600"`
	PublicIPV4          string   `json:"publicIpV4" example:"203.0.113.10"`
	PublicIPV6          string   `json:"publicIpV6" example:"2001:db8::10"`
	LastError           string   `json:"lastError" example:""`
	XrayState           string   `json:"xrayState" example:"running"`
	XrayError           string   `json:"xrayError" example:""`
	ConfigDirty         bool     `json:"configDirty" example:"false"`
	ConfigDirtyAt       int64    `json:"configDirtyAt" example:"0"`
	InboundCount        int      `json:"inboundCount" example:"3"`
	ClientCount         int      `json:"clientCount" example:"25"`
	OnlineCount         int      `json:"onlineCount" example:"5"`
	ActiveCount         int      `json:"activeCount" example:"20"`
	DisabledCount       int      `json:"disabledCount" example:"2"`
	DepletedCount       int      `json:"depletedCount" example:"1"`
	ParentGuid          string   `json:"parentGuid,omitempty" example:""`
	Transitive          bool     `json:"transitive,omitempty" example:"false"`
	CreatedAt           int64    `json:"createdAt" example:"1700000000"`
	UpdatedAt           int64    `json:"updatedAt" example:"1700003600"`
}

func toNodeView(n *model.Node) *NodeView {
	if n == nil {
		return nil
	}
	return &NodeView{
		Id:                  n.Id,
		Name:                n.Name,
		Remark:              n.Remark,
		Scheme:              n.Scheme,
		Address:             n.Address,
		Port:                n.Port,
		BasePath:            n.BasePath,
		HasApiToken:         n.ApiToken != "",
		Enable:              n.Enable,
		AllowPrivateAddress: n.AllowPrivateAddress,
		TlsVerifyMode:       n.TlsVerifyMode,
		PinnedCertSha256:    n.PinnedCertSha256,
		InboundSyncMode:     n.InboundSyncMode,
		InboundTags:         n.InboundTags,
		OutboundTag:         n.OutboundTag,
		Guid:                n.Guid,
		RuntimeCapabilities: parseNodeRuntimeCapabilities(n.RuntimeCapabilities),
		Status:              n.Status,
		LastHeartbeat:       n.LastHeartbeat,
		LatencyMs:           n.LatencyMs,
		XrayVersion:         n.XrayVersion,
		PanelVersion:        n.PanelVersion,
		CpuPct:              n.CpuPct,
		CpuCores:            n.CpuCores,
		LogicalPro:          n.LogicalPro,
		CpuSpeedMhz:         n.CpuSpeedMhz,
		MemCurrent:          n.MemCurrent,
		MemTotal:            n.MemTotal,
		MemPct:              n.MemPct,
		SwapCurrent:         n.SwapCurrent,
		SwapTotal:           n.SwapTotal,
		DiskCurrent:         n.DiskCurrent,
		DiskTotal:           n.DiskTotal,
		UptimeSecs:          n.UptimeSecs,
		NetUp:               n.NetUp,
		NetDown:             n.NetDown,
		NetTrafficSent:      n.NetTrafficSent,
		NetTrafficRecv:      n.NetTrafficRecv,
		TcpCount:            n.TcpCount,
		UdpCount:            n.UdpCount,
		AppStatsMem:         n.AppStatsMem,
		AppStatsThreads:     n.AppStatsThreads,
		AppStatsUptime:      n.AppStatsUptime,
		PublicIPV4:          n.PublicIPV4,
		PublicIPV6:          n.PublicIPV6,
		LastError:           n.LastError,
		XrayState:           n.XrayState,
		XrayError:           n.XrayError,
		ConfigDirty:         n.ConfigDirty,
		ConfigDirtyAt:       n.ConfigDirtyAt,
		InboundCount:        n.InboundCount,
		ClientCount:         n.ClientCount,
		OnlineCount:         n.OnlineCount,
		ActiveCount:         n.ActiveCount,
		DisabledCount:       n.DisabledCount,
		DepletedCount:       n.DepletedCount,
		ParentGuid:          n.ParentGuid,
		Transitive:          n.Transitive,
		CreatedAt:           n.CreatedAt,
		UpdatedAt:           n.UpdatedAt,
	}
}

func toNodeViews(nodes []*model.Node) []*NodeView {
	views := make([]*NodeView, 0, len(nodes))
	for _, node := range nodes {
		views = append(views, toNodeView(node))
	}
	return views
}

// NodeMutationRequest is the node write/probe contract. ApiToken is accepted
// only as input. On update, nil means keep the stored token; replacement and
// clearing are explicit and mutually exclusive.
type NodeMutationRequest struct {
	Id                  int      `json:"id" form:"id"`
	Name                string   `json:"name" form:"name" validate:"required"`
	Remark              string   `json:"remark" form:"remark"`
	Scheme              string   `json:"scheme" form:"scheme" validate:"omitempty,oneof=http https"`
	Address             string   `json:"address" form:"address" validate:"required"`
	Port                int      `json:"port" form:"port" validate:"gte=1,lte=65535"`
	BasePath            string   `json:"basePath" form:"basePath"`
	ApiToken            *string  `json:"apiToken,omitempty" form:"apiToken"`
	ClearApiToken       bool     `json:"clearApiToken,omitempty" form:"clearApiToken"`
	Enable              bool     `json:"enable" form:"enable"`
	AllowPrivateAddress bool     `json:"allowPrivateAddress" form:"allowPrivateAddress"`
	TlsVerifyMode       string   `json:"tlsVerifyMode" form:"tlsVerifyMode" validate:"omitempty,oneof=verify skip pin mtls"`
	PinnedCertSha256    string   `json:"pinnedCertSha256" form:"pinnedCertSha256"`
	InboundSyncMode     string   `json:"inboundSyncMode" form:"inboundSyncMode" validate:"omitempty,oneof=all selected"`
	InboundTags         []string `json:"inboundTags" form:"inboundTags"`
	OutboundTag         string   `json:"outboundTag" form:"outboundTag"`
}

func (r *NodeMutationRequest) validateCredentials(create bool) error {
	if r == nil {
		return common.NewError("node request is required")
	}
	if r.ApiToken != nil && r.ClearApiToken {
		return common.NewError("apiToken and clearApiToken are mutually exclusive")
	}
	if r.ApiToken != nil {
		*r.ApiToken = strings.TrimSpace(*r.ApiToken)
		if *r.ApiToken == "" {
			if create {
				return common.NewError("apiToken is required unless mtls is enabled")
			}
			r.ApiToken = nil
		}
	}
	if create {
		if r.ClearApiToken {
			return common.NewError("credentials cannot be cleared while creating a node")
		}
		if r.ApiToken == nil && r.TlsVerifyMode != "mtls" {
			return common.NewError("apiToken is required unless mtls is enabled")
		}
	}
	if r.ClearApiToken && r.Enable && r.TlsVerifyMode != "mtls" {
		return common.NewError("disable the node or enable mtls before clearing its apiToken")
	}
	return nil
}

func (r *NodeMutationRequest) toNode() *model.Node {
	n := &model.Node{
		Id:                  r.Id,
		Name:                r.Name,
		Remark:              r.Remark,
		Scheme:              r.Scheme,
		Address:             r.Address,
		Port:                r.Port,
		BasePath:            r.BasePath,
		Enable:              r.Enable,
		AllowPrivateAddress: r.AllowPrivateAddress,
		TlsVerifyMode:       r.TlsVerifyMode,
		PinnedCertSha256:    r.PinnedCertSha256,
		InboundSyncMode:     r.InboundSyncMode,
		InboundTags:         r.InboundTags,
		OutboundTag:         r.OutboundTag,
	}
	if r.ApiToken != nil {
		n.ApiToken = *r.ApiToken
	}
	return n
}

func parseNodeRuntimeCapabilities(raw string) []string {
	var values []string
	if json.Unmarshal([]byte(raw), &values) != nil {
		return nil
	}
	allowed := map[string]bool{"amneziawg": true, "mieru": true, "naiveproxy": true}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if allowed[value] {
			out = append(out, value)
		}
	}
	return out
}
