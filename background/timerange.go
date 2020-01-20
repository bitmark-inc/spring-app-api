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

	// Get last post and reaction time.
	lastPost, err := b.bitSocialClient.GetLastPost(ctx, accountNumber)
	if err != nil {
		return err
	}

	lastReaction, err := b.bitSocialClient.GetLastReaction(ctx, accountNumber)
	if err != nil {
		return err
	}

	lastActivityTimestamp := lastPost.Timestamp
	if lastActivityTimestamp < lastReaction.Timestamp {
		lastActivityTimestamp = lastReaction.Timestamp
	}

	if _, err := b.store.UpdateAccountMetadata(ctx, &store.AccountQueryParam{
		AccountNumber: &accountNumber,
	}, map[string]interface{}{
		"last_post_timestamp":     lastPost.Timestamp,
		"last_reaction_timestamp": lastReaction.Timestamp,
		"last_activity_timestamp": lastActivityTimestamp,
	}); err != nil {
		return err
	}

	logEntry.Info("Finish parsing time metadata")

	return nil
}
