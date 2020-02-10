package main

import (
	"context"

	"github.com/bitmark-inc/spring-app-api/store"
	log "github.com/sirupsen/logrus"
)

func (b *BackgroundContext) extractTimeMetadata(ctx context.Context, accountNumber string) error {
	logEntry := log.WithField("prefix", "extract_time_metadata")

	var lastPostTimestamp int64 = 0
	var lastReactionTimestamp int64 = 0

	// Get last post and reaction time.
	lastPost, err := b.bitSocialClient.GetLastPost(ctx, accountNumber)
	if err != nil {
		return err
	}

	if lastPost != nil {
		lastPostTimestamp = lastPost.Timestamp
	}

	lastReaction, err := b.bitSocialClient.GetLastReaction(ctx, accountNumber)
	if err != nil {
		return err
	}

	if lastReaction != nil {
		lastPostTimestamp = lastReaction.Timestamp
	}

	lastActivityTimestamp := lastPostTimestamp
	if lastActivityTimestamp < lastReactionTimestamp {
		lastActivityTimestamp = lastReactionTimestamp
	}

	if _, err := b.store.UpdateAccountMetadata(ctx, &store.AccountQueryParam{
		AccountNumber: &accountNumber,
	}, map[string]interface{}{
		"last_post_timestamp":     lastPostTimestamp,
		"last_reaction_timestamp": lastReactionTimestamp,
		"last_activity_timestamp": lastActivityTimestamp,
	}); err != nil {
		return err
	}

	logEntry.Info("Finish parsing time metadata")

	return nil
}
