package model

type ManagedEndpointClientTraffic struct {
	Id              int    `json:"id" gorm:"primaryKey;autoIncrement"`
	EndpointId      int    `json:"endpointId" gorm:"uniqueIndex:idx_endpoint_client_traffic,priority:1;index"`
	Email           string `json:"email" gorm:"uniqueIndex:idx_endpoint_client_traffic,priority:2;index"`
	NodeGuid        string `json:"nodeGuid" gorm:"uniqueIndex:idx_endpoint_client_traffic,priority:3;index"`
	Up              int64  `json:"up"`
	Down            int64  `json:"down"`
	LastUpCounter   int64  `json:"lastUpCounter"`
	LastDownCounter int64  `json:"lastDownCounter"`
	LatestHandshake int64  `json:"latestHandshake"`
	LastOnline      int64  `json:"lastOnline"`
	Endpoint        string `json:"endpoint,omitempty"`
	UpdatedAt       int64  `json:"updatedAt" gorm:"autoUpdateTime"`
}
