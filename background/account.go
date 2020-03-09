package main

import (
	"context"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/getsentry/sentry-go"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

func (b *BackgroundContext) deleteUserData(ctx context.Context, accountNumber string) error {
	logEntity := log.WithField("prefix", "delete_user_data")

	// Delete on postgres db
	logEntity.Info("Remove postgres db")
	if err := b.store.DeleteAccount(ctx, accountNumber); err != nil {
		logEntity.Error(err)
		sentry.CaptureException(err)
	}

	// Delete data on dynamodb
	logEntity.Info("Remove post week stat")
	if err := b.fbDataStore.RemoveFBStat(ctx, accountNumber+"/post-week-stat"); err != nil {
		logEntity.Error(err)
		sentry.CaptureException(err)
	}

	logEntity.Info("Remove post year stat")
	if err := b.fbDataStore.RemoveFBStat(ctx, accountNumber+"/post-year-stat"); err != nil {
		logEntity.Error(err)
		sentry.CaptureException(err)
	}

	logEntity.Info("Remove post decade stat")
	if err := b.fbDataStore.RemoveFBStat(ctx, accountNumber+"/post-decade-stat"); err != nil {
		logEntity.Error(err)
		sentry.CaptureException(err)
	}

	logEntity.Info("Remove reaction week stat")
	if err := b.fbDataStore.RemoveFBStat(ctx, accountNumber+"/reaction-week-stat"); err != nil {
		logEntity.Error(err)
		sentry.CaptureException(err)
	}

	logEntity.Info("Remove reaction year stat")
	if err := b.fbDataStore.RemoveFBStat(ctx, accountNumber+"/reaction-year-stat"); err != nil {
		logEntity.Error(err)
		sentry.CaptureException(err)
	}

	logEntity.Info("Remove reaction decade stat")
	if err := b.fbDataStore.RemoveFBStat(ctx, accountNumber+"/reaction-decade-stat"); err != nil {
		logEntity.Error(err)
		sentry.CaptureException(err)
	}

	logEntity.Info("Remove posts")
	if err := b.fbDataStore.RemoveFBStat(ctx, accountNumber+"/post"); err != nil {
		logEntity.Error(err)
		sentry.CaptureException(err)
	}

	logEntity.Info("Remove reactions")
	if err := b.fbDataStore.RemoveFBStat(ctx, accountNumber+"/reaction"); err != nil {
		logEntity.Error(err)
		sentry.CaptureException(err)
	}

	// Delete on s3
	logEntity.Info("Remove s3 archive")
	sess := session.New(b.awsConf)
	svc := s3.New(sess)

	iter := s3manager.NewDeleteListIterator(svc, &s3.ListObjectsInput{
		Bucket: aws.String(viper.GetString("aws.s3.bucket")),
		Prefix: aws.String(accountNumber + "/"),
	})

	if err := s3manager.NewBatchDeleteWithClient(svc).Delete(ctx, iter); err != nil {
		logEntity.Error(err)
		sentry.CaptureException(err)
	}

	logEntity.Info("Finish")

	return nil
}
