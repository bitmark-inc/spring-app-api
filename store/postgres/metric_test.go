package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/bitmark-inc/spring-app-api/store"
	"github.com/stretchr/testify/assert"
)

func Test_AccountMetric(t *testing.T) {
	loadTestConfig()

	ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)
	s, err := NewPGStore(ctx)
	assert.NoError(t, err)
	assert.NotNil(t, s)

	metricMap, err := s.CountAccountCreation(ctx, time.Now().Add(0-time.Hour), time.Now())
	assert.NoError(t, err)
	assert.Equal(t, 0, metricMap["ios"])
	assert.Equal(t, 0, metricMap["android"])

	iosAccountNumber := "ios_account"
	androidAccountNumber := "android_account"

	// Insert account with null public account number and metadata
	account, err := s.InsertAccount(ctx, iosAccountNumber, nil, map[string]interface{}{
		"platform": "ios",
	})
	assert.NoError(t, err)
	assert.NotNil(t, account)

	account, err = s.InsertAccount(ctx, androidAccountNumber, nil, map[string]interface{}{
		"platform": "android",
	})
	assert.NoError(t, err)
	assert.NotNil(t, account)

	// Insert fb archives
	archive, err := s.AddFBArchive(ctx, iosAccountNumber, time.Now(), time.Now())
	assert.NoError(t, err)
	assert.NotNil(t, archive)

	archives, err := s.UpdateFBArchiveStatus(ctx, &store.FBArchiveQueryParam{
		AccountNumber: &iosAccountNumber,
	}, &store.FBArchiveQueryParam{
		Status: &store.FBArchiveStatusProcessed,
	})
	assert.NoError(t, err)
	assert.Len(t, archives, 1)

	metricMap, err = s.CountAccountCreation(ctx, time.Now().Add(0-time.Hour), time.Now())
	assert.NoError(t, err)
	assert.Equal(t, 1, metricMap["ios"])
	assert.Equal(t, 0, metricMap["android"])
}
