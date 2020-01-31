package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/bitmark-inc/spring-app-api/store"
	"github.com/stretchr/testify/assert"
)

var (
	testAccountNumber1 = "test_bitmark_account_1"
	testAccountNumber2 = "test_bitmark_account_2"
	testAccountNumber3 = "test_bitmark_account_3"
	metadataData       = map[string]interface{}{
		"test": "test",
	}
)

func Test_InsertAccount(t *testing.T) {
	loadTestConfig()

	ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)
	s, err := NewPGStore(ctx)
	assert.Nil(t, err)
	assert.NotNil(t, s)

	// Insert account with null public account number and metadata
	account, err := s.InsertAccount(ctx, testAccountNumber1, nil, nil)
	assert.Nil(t, err)
	assert.NotNil(t, account)

	// Insert with the same account
	account, err = s.InsertAccount(ctx, testAccountNumber1, nil, nil)
	assert.NotNil(t, err)

	// Insert with metadata
	account, err = s.InsertAccount(ctx, testAccountNumber2, nil, metadataData)
	assert.Nil(t, err)
	assert.NotNil(t, account)

	// Query account again
	account2, err := s.QueryAccount(ctx, &store.AccountQueryParam{
		AccountNumber: &testAccountNumber2,
	})

	assert.Equal(t, testAccountNumber2, account2.AccountNumber)
	assert.Equal(t, metadataData, account2.Metadata)
}

func Test_UpdateAccountMetadata(t *testing.T) {
	loadTestConfig()

	ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)
	s, err := NewPGStore(ctx)
	assert.Nil(t, err)
	assert.NotNil(t, s)

	// Insert account with null public account number and metadata
	account, err := s.InsertAccount(ctx, testAccountNumber3, nil, nil)
	assert.Nil(t, err)
	assert.NotNil(t, account)

	account2, err := s.UpdateAccountMetadata(ctx, &store.AccountQueryParam{
		AccountNumber: &testAccountNumber3,
	}, metadataData)

	assert.Equal(t, metadataData, account2.Metadata)

	// Query account again to see
	account3, err := s.QueryAccount(ctx, &store.AccountQueryParam{
		AccountNumber: &testAccountNumber3,
	})

	assert.Equal(t, metadataData, account3.Metadata)

}
