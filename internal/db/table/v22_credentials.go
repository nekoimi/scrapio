package table

import "time"

type V22Credential struct {
	Id                string `xorm:"varchar(36) pk"`
	OwnerId           int64  `xorm:"owner_id"`
	Revision          int
	Name              string
	Kind              string
	Origin            string
	Storage           string
	Header            string
	Prefix            string
	Env               string
	Ciphertext        string
	KeyId             string `xorm:"key_id"`
	MaskSelectors     string `xorm:"jsonb mask_selectors"`
	Fingerprint       string
	CreateFingerprint string     `xorm:"create_fingerprint"`
	ExpiresAt         time.Time  `xorm:"expires_at"`
	RevokedAt         *time.Time `xorm:"revoked_at"`
	CreatedAt         time.Time  `xorm:"created_at"`
	UpdatedAt         time.Time  `xorm:"updated_at"`
}

func (V22Credential) TableName() string { return "v22_credentials" }
