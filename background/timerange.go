package main

import (
	"context"

	log "github.com/sirupsen/logrus"

	"github.com/bitmark-inc/spring-app-api/schema/facebook"
	"github.com/bitmark-inc/spring-app-api/store"
)

func (b *BackgroundContext) extractTimeMetadata(ctx context.Context, accountNumber string) error {
	logEntry := log.WithField("prefix", "extract_time_metadata")

	var lastPostTimestamp int64 = 0
	var lastReactionTimestamp int64 = 0

	// Get last post and reaction time.
	var lastPost facebook.PostORM
	if err := b.ormDB.Order("timestamp DESC").First(&lastPost).Error; err != nil {
		return err
	}

	if lastPost.Timestamp > 0 {
		lastPostTimestamp = lastPost.Timestamp
	}

	var lastReaction facebook.ReactionORM
	if err := b.ormDB.Order("timestamp DESC").First(&lastReaction).Error; err != nil {
		return err
	}

	if lastReaction.Timestamp > 0 {
		lastReactionTimestamp = lastReaction.Timestamp
	}

	latestActivityTimestamp := lastPostTimestamp
	if latestActivityTimestamp < lastReactionTimestamp {
		latestActivityTimestamp = lastReactionTimestamp
	}

	if _, err := b.store.UpdateAccountMetadata(ctx, &store.AccountQueryParam{
		AccountNumber: &accountNumber,
	}, map[string]interface{}{
		"last_post_timestamp":       lastPostTimestamp,
		"last_reaction_timestamp":   lastReactionTimestamp,
		"last_activity_timestamp":   latestActivityTimestamp,
		"latest_activity_timestamp": latestActivityTimestamp,
	}); err != nil {
		return err
	}

	logEntry.Info("Finish parsing time metadata")

	return nil
}
