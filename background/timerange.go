package main

import (
	"context"

	"github.com/bitmark-inc/spring-app-api/store"
	"github.com/gocraft/work"
	log "github.com/sirupsen/logrus"
)

func (b *BackgroundContext) extractTimeMetadata(job *work.Job) (err error) {
	defer jobEndCollectiveMetric(err, job)
	logEntry := log.WithField("prefix", job.Name+"/"+job.ID)
	accountNumber := job.ArgString("account_number")
	if err := job.ArgError(); err != nil {
		return err
	}

	ctx := context.Background()

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
