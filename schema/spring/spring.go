package spring

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type AccountORM struct {
	AccountNumber string `gorm:"primary_key"`
	Metadata      json.RawMessage
	Deleting      bool
}

func (AccountORM) TableName() string {
	return "account"
}

// Spring app total archive
type ArchiveORM struct {
	ID            uuid.UUID  `gorm:"type:uuid;primary_key" sql:"default:uuid_generate_v4()" json:"id"`
	JobID         *uuid.UUID `gorm:"type:uuid" json:"-"`
	Status        string     `json:"status"`
	FileKey       string     `json:"file_key"`
	FileSize      int64      `json:"file_size"`
	AccountNumber string     `json:"-"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

func (ArchiveORM) TableName() string {
	return "archive"
}

// Facebook Archive
type FBArchiveORM struct {
	ID               int `gorm:"primary_key"`
	AccountNumber    string
	FileKey          string
	ProcessingStatus string
	ProcessingError  json.RawMessage
	CreatedAt        time.Time
}

func (FBArchiveORM) TableName() string {
	return "fbarchive"
}
