package main

import (
	"context"
	"encoding/hex"
	"io"
	"io/ioutil"
	"os"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/bitmark-inc/spring-app-api/store"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"golang.org/x/crypto/sha3"
)

func (b *BackgroundContext) generateHashContent(ctx context.Context, s3key string, archiveid int64) error {
	logEntity := log.WithField("prefix", "generate_hash_content")

	sess := session.New(b.awsConf)
	downloader := s3manager.NewDownloader(sess)
	h := sha3.New512()

	tmpFile, err := ioutil.TempFile(viper.GetString("archive.workdir"), "fbarchives-*.zip")
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
