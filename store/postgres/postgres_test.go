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
	loadTestConfig()

	ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)
	s, err := NewPGStore(ctx)
	assert.Nil(t, err)
	assert.NotNil(t, s)

	err = s.Ping(ctx)
	assert.Nil(t, err)

	err = s.Close(ctx)
	assert.Nil(t, err)
}
