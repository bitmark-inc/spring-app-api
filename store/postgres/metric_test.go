package postgres

import (
	"context"
	"testing"
	"time"

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

	// Insert account with null public account number and metadata
	account, err := s.InsertAccount(ctx, "ios_account", nil, map[string]interface{}{
		"platform": "ios",
	})
	assert.NoError(t, err)
	assert.NotNil(t, account)

	account, err = s.InsertAccount(ctx, "android_account", nil, map[string]interface{}{
		"platform": "android",
	})
	assert.NoError(t, err)
	assert.NotNil(t, account)

	metricMap, err = s.CountAccountCreation(ctx, time.Now().Add(0-time.Hour), time.Now())
	assert.NoError(t, err)
	assert.Equal(t, 1, metricMap["ios"])
	assert.Equal(t, 1, metricMap["android"])
}
