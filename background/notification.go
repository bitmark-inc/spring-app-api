package main

import (
	"context"

	log "github.com/sirupsen/logrus"
)

func (b *BackgroundContext) notifyAnalyzingDone(ctx context.Context, accountNumber string) error {
	logEntity := log.WithField("prefix", "notify_analyzing_done")

	if err := b.oneSignalClient.NotifyFBArchiveAvailable(ctx, accountNumber); err != nil {
		logEntity.Error(err)
		return err
	}

	return nil
}
