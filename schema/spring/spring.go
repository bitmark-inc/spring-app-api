package spring

type AccountORM struct {
	AccountNumber string `gorm:"primary_key"`
}

func (AccountORM) TableName() string {
	return "account"
}

type ArchiveORM struct {
	ID               int `gorm:"primary_key"`
	AccountNumber    string
	FileKey          string
	ProcessingStatus string
	ProcessingError  map[string]interface{}
}

func (ArchiveORM) TableName() string {
	return "fbarchive"
}
