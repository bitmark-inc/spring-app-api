package main

import (
	"context"
	"encoding/hex"
	"io"
	"io/ioutil"
	"os"
	"time"

	"github.com/RichardKnop/machinery/v1/tasks"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/bitmark-inc/spring-app-api/store"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"golang.org/x/crypto/sha3"
)

func (b *BackgroundContext) submitArchive(ctx context.Context, s3key, accountNumber string, archiveid int64) error {
	logEntity := log.WithField("prefix", "submit_archive")
	// Register data owner
	if err := b.bitSocialClient.NewDataOwner(ctx, accountNumber); err != nil {
		log.Debug(err)
	}

	// Set status to processing
	if _, err := b.store.UpdateFBArchiveStatus(ctx, &store.FBArchiveQueryParam{
		ID: &archiveid,
	}, &store.FBArchiveQueryParam{
		Status: &store.FBArchiveStatusProcessing,
	}); err != nil {
		logEntity.Error(err)
		return err
	}

	sess := session.New(b.awsConf)
	downloader := s3manager.NewDownloader(sess)

	tmpFile, err := ioutil.TempFile(os.TempDir(), "fbarchives-*.zip")
	if err != nil {
		logEntity.Error(err)
		return err
	}

	defer tmpFile.Close()
	// Remember to clean up the file afterwards
	defer os.Remove(tmpFile.Name())

	_, err = downloader.Download(tmpFile,
		&s3.GetObjectInput{
			Bucket: aws.String(viper.GetString("aws.s3.bucket")),
			Key:    aws.String(s3key),
		})

	if err != nil {
		logEntity.Error(err)
		return err
	}

	logEntity.Info("Downloaded zip file. Start submiting")

	archiveID, err := b.bitSocialClient.UploadArchives(ctx, tmpFile, accountNumber)
	if err != nil {
		logEntity.Error(err)
		return err
	}

	logEntity.Info("Trigger parsing")
	taskID, err := b.bitSocialClient.TriggerParsing(ctx, archiveID, accountNumber)
	if err != nil {
		logEntity.Error(err)
		return err
	}

	logEntity.Info("Upload success, update db information")

	if _, err := b.store.UpdateFBArchiveStatus(ctx, &store.FBArchiveQueryParam{
		ID: &archiveid,
	}, &store.FBArchiveQueryParam{
		AnalyzedID: &taskID,
	}); err != nil {
		logEntity.Error(err)
		return err
	}

	logEntity.Info("Finish...")
	eta := time.Now().Add(time.Second * 120)
	server.SendTask(&tasks.Signature{
		Name: jobPeriodicArchiveCheck,
		ETA:  &eta,
		Args: []tasks.Arg{
			{
				Type:  "int64",
				Value: archiveid,
			},
		},
	})
	return nil
}

func (b *BackgroundContext) checkArchive(ctx context.Context, archiveid int64) error {
	logEntity := log.WithField("prefix", "check_archive")

	archives, err := b.store.GetFBArchives(ctx, &store.FBArchiveQueryParam{
		ID: &archiveid,
	})
	if err != nil {
		logEntity.Error(err)
		return err
	}

	if len(archives) != 1 {
		logEntity.Warn("Cannot find archive with ID: ", archiveid)
		return nil
	}

	status, err := b.bitSocialClient.GetArchiveTaskStatus(ctx, archives[0].AnalyzedTaskID)
	if err != nil {
		logEntity.Error(err)
		return err
	}
	log.Info("Receive status: ", status)

	switch status {
	case "FAILED":
		if _, err := b.store.UpdateFBArchiveStatus(ctx, &store.FBArchiveQueryParam{
			ID: &archiveid,
		}, &store.FBArchiveQueryParam{
			Status: &store.FBArchiveStatusInvalid,
		}); err != nil {
			logEntity.Error(err)
			return err
		}
	case "FINISHED":
		server.SendTask(&tasks.Signature{
			Name: jobAnalyzePosts,
			Args: []tasks.Arg{
				{
					Type:  "string",
					Value: archives[0].AccountNumber,
				},
				{
					Type:  "int64",
					Value: archiveid,
				},
			},
		})

		server.SendTask(&tasks.Signature{
			Name: jobExtractTimeMetadata,
			Args: []tasks.Arg{
				{
					Type:  "string",
					Value: archives[0].AccountNumber,
				},
			},
		})
	case "INTERRUPTED":
		logEntity.Warn("Task interrupted")
		return nil
	default:
		// Retry after 10 minutes
		eta := time.Now().Add(time.Minute * 10)
		server.SendTask(&tasks.Signature{
			Name: jobPeriodicArchiveCheck,
			ETA:  &eta,
			Args: []tasks.Arg{
				{
					Type:  "int64",
					Value: archiveid,
				},
			},
		})

		log.Info("Retry after 10 minutes")
	}

	logEntity.Info("Finish...")

	return nil
}

func (b *BackgroundContext) generateHashContent(ctx context.Context, s3key string, archiveid int64) error {
	logEntity := log.WithField("prefix", "generate_hash_content")

	sess := session.New(b.awsConf)
	downloader := s3manager.NewDownloader(sess)
	h := sha3.New512()

	tmpFile, err := ioutil.TempFile(os.TempDir(), "fbarchives-*.zip")
	if err != nil {
		logEntity.Error(err)
		return err
	}

	defer tmpFile.Close()
	// Remember to clean up the file afterwards
	defer os.Remove(tmpFile.Name())

	_, err = downloader.Download(tmpFile,
		&s3.GetObjectInput{
			Bucket: aws.String(viper.GetString("aws.s3.bucket")),
			Key:    aws.String(s3key),
		})

	if err != nil {
		logEntity.Error(err)
		return err
	}

	logEntity.Info("Downloaded zip file. Start computing fingerprint")

	io.Copy(h, tmpFile)

	// Get fingerprint
	fingerprintBytes := h.Sum(nil)
	fingerprint := hex.EncodeToString(fingerprintBytes)

	_, err = b.store.UpdateFBArchiveStatus(ctx, &store.FBArchiveQueryParam{
		ID: &archiveid,
	}, &store.FBArchiveQueryParam{
		ContentHash: &fingerprint,
	})
	if err != nil {
		logEntity.Error(err)
		return err
	}

	logEntity.Info("Finish...")
	return nil
}
