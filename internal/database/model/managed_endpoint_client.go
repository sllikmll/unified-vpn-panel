package model

type EndpointClientState string

const (
	EndpointClientPending  EndpointClientState = "pending"
	EndpointClientApplied  EndpointClientState = "applied"
	EndpointClientDisabled EndpointClientState = "disabled"
	EndpointClientFailed   EndpointClientState = "failed"
	EndpointClientDeleting EndpointClientState = "deleting"
	EndpointClientDeleted  EndpointClientState = "deleted"
)

type ManagedEndpointClient struct {
	Id              int                 `json:"id" gorm:"primaryKey;autoIncrement" example:"1"`
	EndpointId      int                 `json:"endpointId" gorm:"uniqueIndex:idx_endpoint_client_identity,priority:1;index"`
	ClientId        int                 `json:"clientId" gorm:"index"`
	SubID           string              `json:"subId" gorm:"-"`
	Email           string              `json:"email" gorm:"uniqueIndex:idx_endpoint_client_identity,priority:2;index"`
	Enable          bool                `json:"enable" gorm:"index"`
	State           EndpointClientState `json:"state" gorm:"index" example:"applied"`
	Status          EndpointClientState `json:"status,omitempty" gorm:"-"`
	TrafficUp       int64               `json:"trafficUp" gorm:"-"`
	TrafficDown     int64               `json:"trafficDown" gorm:"-"`
	LatestHandshake int64               `json:"latestHandshake,omitempty" gorm:"-"`
	LastOnline      int64               `json:"lastOnline,omitempty" gorm:"-"`
	PublicIdentity  string              `json:"publicIdentity,omitempty" gorm:"index"`
	Address         string              `json:"address,omitempty"`
	CredentialRef   string              `json:"-" gorm:"index"`
	ClientConfig    string              `json:"-" gorm:"type:text"`
	ObservedConfig  string              `json:"-" gorm:"type:text"`
	LastAppliedHash string              `json:"-" gorm:"size:64"`
	LastError       string              `json:"-" gorm:"type:text"`
	CreatedAt       int64               `json:"createdAt" gorm:"autoCreateTime"`
	UpdatedAt       int64               `json:"updatedAt" gorm:"autoUpdateTime"`
}
