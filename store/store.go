package store

import (
	"context"
	"time"
)

// Store an interface to determine what to store in server
type Store interface {
	// Operation

	// Ping to ping the existing db for liveness proof
	Ping(ctx context.Context) error

	// Close to close the current db connection
	Close(ctx context.Context) error

	// Account

	// InsertAccount insert an account to the db with account number, alias and stripe's customer id
	// returns an Account object.
	InsertAccount(ctx context.Context, accountNumber string, encPubKey []byte) (*Account, error)

	// QueryAccount query for an account with condition from account number OR alias
	// leaves one of the condition empty to ignore.
	QueryAccount(ctx context.Context, params *AccountQueryParam) (*Account, error)

	// UpdateAccount to update account information
	UpdateAccount(ctx context.Context, a *Account) (bool, error)

	// AddToken to add a random token represent to an account for validating something
	AddToken(ctx context.Context, accountNumber string, info map[string]interface{}, expire time.Duration) (*Token, error)

	// UseToken to consume a token and
	UseToken(ctx context.Context, token string) (*Account, map[string]interface{}, error)

	// AddFBArchive to add an archive record from an account
	AddFBArchive(ctx context.Context, accountNumber string, starting, ending time.Time) (*FBArchive, error)

	// UpdateFBArchiveStatus to update status for a particular fb archive record with s3 key
	UpdateFBArchiveStatus(ctx context.Context, params *FBArchiveQueryParam, values *FBArchiveQueryParam) ([]FBArchive, error)

	// GetFBArchives to fetch all fb archives
	GetFBArchives(ctx context.Context, params *FBArchiveQueryParam) ([]FBArchive, error)
}

// AccountQueryParam params for querying an account
type AccountQueryParam struct {
	AccountNumber *string
}

// FBArchiveQueryParam params for querying a fb archive
type FBArchiveQueryParam struct {
	ID            *int64
	AccountNumber *string
	S3Key         *string
	Status        *string
}

var (
	// FB archive statuses:

	// FBArchiveStatusSubmitted when an archive is submitted from clients
	FBArchiveStatusSubmitted = "submitted"

	// FBArchiveStatusStored when an archive is successfully
	// stored in amazon s3 and also extracted
	FBArchiveStatusStored = "stored"

	// FBArchiveStatusProcessed when an archive is processed
	// and its stats is avaialbe to query via API
	FBArchiveStatusProcessed = "processed"

	// FBArchiveStatusInvalid when an archive is either failed to download
	// or there are errors while processing
	FBArchiveStatusInvalid = "invalid"
)
