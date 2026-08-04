package model

type RuntimeKind string

const (
	RuntimeXray       RuntimeKind = "xray"
	RuntimeMTProto    RuntimeKind = "mtproto"
	RuntimeWireGuard  RuntimeKind = "wireguard"
	RuntimeAmneziaWG  RuntimeKind = "amneziawg"
	RuntimeMieru      RuntimeKind = "mieru"
	RuntimeNaiveProxy RuntimeKind = "naiveproxy"
)

type ManagedProtocol string

type EndpointStatus string

const (
	EndpointDraft      EndpointStatus = "draft"
	EndpointApplying   EndpointStatus = "applying"
	EndpointActive     EndpointStatus = "active"
	EndpointDegraded   EndpointStatus = "degraded"
	EndpointDisabled   EndpointStatus = "disabled"
	EndpointFailed     EndpointStatus = "failed"
	EndpointDeleting   EndpointStatus = "deleting"
	EndpointDeleted    EndpointStatus = "deleted"
	EndpointRolledBack EndpointStatus = "rolled_back"
)

type ManagedEndpoint struct {
	Id               int             `json:"id" gorm:"primaryKey;autoIncrement" example:"1"`
	UserId           int             `json:"-" gorm:"uniqueIndex:idx_managed_endpoints_user_tag,priority:1"`
	InboundId        *int            `json:"inboundId,omitempty" gorm:"index"`
	NodeID           *int            `json:"nodeId,omitempty" gorm:"index"`
	RuntimeKind      RuntimeKind     `json:"runtimeKind" gorm:"index;not null" validate:"required" example:"wireguard"`
	Protocol         ManagedProtocol `json:"protocol" gorm:"index;not null" validate:"required" example:"wireguard"`
	Tag              string          `json:"tag" gorm:"uniqueIndex:idx_managed_endpoints_user_tag,priority:2;not null" example:"wg-home"`
	Remark           string          `json:"remark" example:"WireGuard home"`
	Listen           string          `json:"listen"`
	Port             int             `json:"port" validate:"gte=0,lte=65535" example:"51820"`
	Enable           bool            `json:"enable" gorm:"index"`
	Status           EndpointStatus  `json:"status" gorm:"index" example:"active"`
	DesiredConfig    string          `json:"-" gorm:"type:text"`
	ObservedConfig   string          `json:"-" gorm:"type:text"`
	Capabilities     string          `json:"capabilities" gorm:"type:text"`
	LastAppliedHash  string          `json:"lastAppliedHash" gorm:"size:64"`
	LastObservedHash string          `json:"lastObservedHash" gorm:"size:64"`
	LastError        string          `json:"-" gorm:"type:text"`
	LastHealthAt     int64           `json:"lastHealthAt"`
	CreatedAt        int64           `json:"createdAt" gorm:"autoCreateTime"`
	UpdatedAt        int64           `json:"updatedAt" gorm:"autoUpdateTime"`
}
