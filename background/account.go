package main

import (
	"context"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/gocraft/work"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

func (b *BackgroundContext) deleteUserData(job *work.Job) (err error) {
	defer jobEndCollectiveMetric(err, job)
	logEntity := log.WithField("prefix", job.Name+"/"+job.ID)
	accountNumber := job.ArgString("account_number")
	if err := job.ArgError(); err != nil {
		return err
	}

	ctx := context.Background()

	// Delete parser's data_owner
	logEntity.Info("Remove data owner on bitsocial")
	job.Checkin("Remove data owner on bitsocial")
	if err := b.bitSocialClient.DeleteDataOwner(ctx, accountNumber); err != nil {
		return err
	}

	// Delete data on dynamodb
	logEntity.Info("Remove post week stat")
	job.Checkin("Remove post week stat")
	if err := b.fbDataStore.RemoveFBStat(ctx, accountNumber+"/post-week-stat"); err != nil {
		return err
	}

	logEntity.Info("Remove post year stat")
	job.Checkin("Remove post year stat")
	if err := b.fbDataStore.RemoveFBStat(ctx, accountNumber+"/post-year-stat"); err != nil {
		return err
	}

	logEntity.Info("Remove post decade stat")
	job.Checkin("Remove post decade stat")
	if err := b.fbDataStore.RemoveFBStat(ctx, accountNumber+"/post-decade-stat"); err != nil {
		return err
	}

	logEntity.Info("Remove reaction week stat")
	job.Checkin("Remove reaction week stat")
	if err := b.fbDataStore.RemoveFBStat(ctx, accountNumber+"/reaction-week-stat"); err != nil {
		return err
	}

	logEntity.Info("Remove reaction year stat")
	job.Checkin("Remove reaction year stat")
	if err := b.fbDataStore.RemoveFBStat(ctx, accountNumber+"/reaction-year-stat"); err != nil {
		return err
	}

	logEntity.Info("Remove reaction decade stat")
	job.Checkin("Remove reaction decade stat")
	if err := b.fbDataStore.RemoveFBStat(ctx, accountNumber+"/reaction-decade-stat"); err != nil {
		return err
	}

	logEntity.Info("Remove posts")
	job.Checkin("Remove posts")
	if err := b.fbDataStore.RemoveFBStat(ctx, accountNumber+"/post"); err != nil {
		return err
	}

	logEntity.Info("Remove reactions")
	job.Checkin("Remove reactions")
	if err := b.fbDataStore.RemoveFBStat(ctx, accountNumber+"/reaction"); err != nil {
		return err
	}

	// Delete on s3
	logEntity.Info("Remove s3 archive")
	job.Checkin("Remove s3 archive")
	sess := session.New(b.awsConf)
	svc := s3.New(sess)

	iter := s3manager.NewDeleteListIterator(svc, &s3.ListObjectsInput{
		Bucket: aws.String(viper.GetString("aws.s3.bucket")),
		Prefix: aws.String("archives/" + accountNumber + "/"),
	})

	if err := s3manager.NewBatchDeleteWithClient(svc).Delete(ctx, iter); err != nil {
		return err
	}

	// Delete on postgres db
	logEntity.Info("Remove postgres db")
	job.Checkin("Remove postgres db")
	if err := b.store.DeleteAccount(ctx, accountNumber); err != nil {
		return err
	}
	logEntity.Info("Finish")

	return nil
}
