package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/bitmark-inc/spring-app-api/store"
	"github.com/stretchr/testify/assert"
)

func Test_FBArchive(t *testing.T) {
	loadTestConfig()

	ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)
	s, err := NewPGStore(ctx)
	assert.NoError(t, err)
	assert.NotNil(t, s)

	// Insert an account if it's not exsting
	s.InsertAccount(ctx, testAccountNumber1, nil, nil)

	// Create archive
	_, err = s.AddFBArchive(ctx, "not_existing_account_number", time.Now(), time.Now())
	assert.Error(t, err)

	archive, err := s.AddFBArchive(ctx, testAccountNumber1, time.Now(), time.Now())
	assert.NoError(t, err)
	assert.NotNil(t, archive)

	assert.Equal(t, testAccountNumber1, archive.AccountNumber)

	// Update archive
	wrongStatus := "wrong_status"
	correctStatus := "invalid"
	contentHash := "hash"
	taskID := "task_id"

	_, err = s.UpdateFBArchiveStatus(ctx, &store.FBArchiveQueryParam{
		ID:    &archive.ID,
		S3Key: &archive.S3Key,
	}, &store.FBArchiveQueryParam{
		Status: &wrongStatus,
	})
	assert.Error(t, err)

	correctID := archive.ID

	archives, err := s.UpdateFBArchiveStatus(ctx, &store.FBArchiveQueryParam{
		ID:    &correctID,
		S3Key: &archive.S3Key,
	}, &store.FBArchiveQueryParam{
		Status:      &correctStatus,
		ContentHash: &contentHash,
		AnalyzedID:  &taskID,
	})
	assert.NoError(t, err)
	assert.Len(t, archives, 1)
	assert.Equal(t, correctStatus, archives[0].ProcessingStatus)
	assert.Equal(t, contentHash, archives[0].ContentHash)
	assert.Equal(t, taskID, archives[0].AnalyzedTaskID)

	// Query archives
	wrongID := int64(9234235)
	archives, err = s.GetFBArchives(ctx, &store.FBArchiveQueryParam{
		ID: &wrongID,
	})
	assert.NoError(t, err)
	assert.Len(t, archives, 0)

	archives, err = s.GetFBArchives(ctx, &store.FBArchiveQueryParam{
		ID: &correctID,
	})
	assert.NoError(t, err)
	assert.Len(t, archives, 1)
}
