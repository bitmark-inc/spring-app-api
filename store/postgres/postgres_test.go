package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
)

func loadTestConfig() {
	viper.AutomaticEnv()
	viper.SetEnvPrefix("fbm")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	log.SetLevel(log.DebugLevel)
}

func Test_PostgresEngine(t *testing.T) {
	ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)
	loadTestConfig()

	s, err := NewPGStore(ctx)
	assert.NoError(t, err)
	assert.NotNil(t, s)

	err = s.Ping(ctx)
	assert.NoError(t, err)

	err = s.Close(ctx)
	assert.NoError(t, err)
}
