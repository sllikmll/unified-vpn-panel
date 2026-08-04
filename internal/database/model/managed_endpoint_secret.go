package model

type ManagedSecret struct {
	Id          int    `json:"id" gorm:"primaryKey;autoIncrement"`
	OwnerType   string `json:"ownerType" gorm:"uniqueIndex:idx_secret_owner,priority:1"`
	OwnerId     int    `json:"ownerId" gorm:"uniqueIndex:idx_secret_owner,priority:2"`
	SecretKind  string `json:"secretKind" gorm:"uniqueIndex:idx_secret_owner,priority:3"`
	Ciphertext  []byte `json:"-"`
	Fingerprint string `json:"fingerprint" gorm:"size:64;index"`
	CreatedAt   int64  `json:"createdAt" gorm:"autoCreateTime"`
	UpdatedAt   int64  `json:"updatedAt" gorm:"autoUpdateTime"`
}
