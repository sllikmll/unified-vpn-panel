package model

type ManagedEndpointApplyLog struct {
	Id             int    `json:"id" gorm:"primaryKey;autoIncrement"`
	IdempotencyKey string `json:"idempotencyKey" gorm:"uniqueIndex;not null"`
	EndpointId     int    `json:"endpointId" gorm:"index"`
	NodeID         *int   `json:"nodeId,omitempty" gorm:"index"`
	Action         string `json:"action" gorm:"index"`
	Status         string `json:"status" gorm:"index"`
	RequestHash    string `json:"requestHash" gorm:"size:64"`
	BeforeHash     string `json:"beforeHash" gorm:"size:64"`
	AfterHash      string `json:"afterHash" gorm:"size:64"`
	RollbackToken  string `json:"-"`
	Error          string `json:"-" gorm:"type:text"`
	CreatedAt      int64  `json:"createdAt" gorm:"autoCreateTime"`
	UpdatedAt      int64  `json:"updatedAt" gorm:"autoUpdateTime"`
}
