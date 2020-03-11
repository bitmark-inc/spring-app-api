package main

import (
	"context"

	"github.com/jinzhu/gorm"
	log "github.com/sirupsen/logrus"

	"github.com/bitmark-inc/spring-app-api/schema/facebook"
	"github.com/bitmark-inc/spring-app-api/store"
)

func (b *BackgroundContext) extractTimeMetadata(ctx context.Context, accountNumber string) error {
	logEntry := log.WithField("prefix", "extract_time_metadata")

	var firstPostTimestamp, lastPostTimestamp int64
	var firstReactionTimestamp, lastReactionTimestamp int64

	// Get last post and reaction time.
	var firstPost, lastPost facebook.PostORM
	if err := b.ormDB.Where("data_owner_id = ?", accountNumber).Order("timestamp ASC").First(&firstPost).Error; err != nil {
		if err != gorm.ErrRecordNotFound {
			return err
		}
	}
	if err := b.ormDB.Where("data_owner_id = ?", accountNumber).Order("timestamp DESC").First(&lastPost).Error; err != nil {
		if err != gorm.ErrRecordNotFound {
			return err
		}
	}

	if firstPost.Timestamp > 0 {
		firstPostTimestamp = firstPost.Timestamp
	}
	if lastPost.Timestamp > 0 {
		lastPostTimestamp = lastPost.Timestamp
	}

	var firstReaction, lastReaction facebook.ReactionORM
	if err := b.ormDB.Where("data_owner_id = ?", accountNumber).Order("timestamp ASC").First(&firstReaction).Error; err != nil {
		if err != gorm.ErrRecordNotFound {
			return err
		}
	}
	if err := b.ormDB.Where("data_owner_id = ?", accountNumber).Order("timestamp DESC").First(&lastReaction).Error; err != nil {
		if err != gorm.ErrRecordNotFound {
			return err
		}
	}

	if firstReaction.Timestamp > 0 {
		firstReactionTimestamp = firstReaction.Timestamp
	}
	if lastReaction.Timestamp > 0 {
		lastReactionTimestamp = lastReaction.Timestamp
	}

	firstActivityTimestamp := firstPostTimestamp
	if firstActivityTimestamp > firstReactionTimestamp {
		firstActivityTimestamp = firstReactionTimestamp
	}

	lastActivityTimestamp := lastPostTimestamp
	if lastActivityTimestamp < lastReactionTimestamp {
		lastActivityTimestamp = lastReactionTimestamp
	}

	if _, err := b.store.UpdateAccountMetadata(ctx, &store.AccountQueryParam{
		AccountNumber: &accountNumber,
	}, map[string]interface{}{
		"last_post_timestamp":       lastPostTimestamp,
		"last_reaction_timestamp":   lastReactionTimestamp,
		"first_activity_timestamp":  firstActivityTimestamp,
		"last_activity_timestamp":   lastActivityTimestamp,
		"latest_activity_timestamp": lastActivityTimestamp,
	}); err != nil {
		return err
	}

	logEntry.Info("Finish parsing time metadata")

	return nil
}
