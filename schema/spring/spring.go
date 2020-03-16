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
	ID            uuid.UUID  `gorm:"type:uuid;primary_key" sql:"default:uuid_generate_v4()"`
	JobID         *uuid.UUID `gorm:"type:uuid;`
	Status        string
	FileKey       string
	AccountNumber string
	CreatedAt     time.Time
	UpdatedAt     time.Time
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
}

func (FBArchiveORM) TableName() string {
	return "fbarchive"
}
