package s3util

import (
	"fmt"
	"io"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

// UploadArchive upload archive files to S3
func UploadArchive(sess *session.Session, data io.Reader, accountNumber, filename, archiveType string, archiveID int64, metadata map[string]*string) (string, error) {
	logEntity := log.WithField("prefix", "s3_util")

	svc := s3manager.NewUploader(sess)

	s3key := fmt.Sprintf("%s/%s/archives/%d/%s", accountNumber, archiveType, archiveID, filename)

	logEntity.WithField("key", s3key).Info("Start uploading to S3")
	_, err := svc.Upload(&s3manager.UploadInput{
		Bucket:   aws.String(viper.GetString("aws.s3.bucket")),
		Key:      aws.String(s3key),
		Body:     data,
		Metadata: metadata,
	})
	return s3key, err
}

// GetMediaPresignedURL returns the presigned link from S3 by a specific file path and a specific time
func GetMediaPresignedURL(sess *session.Session, s3Key string, expire time.Duration) (string, error) {
	logEntity := log.WithField("prefix", "s3_util")

	svc := s3.New(sess)
	req, _ := svc.GetObjectRequest(&s3.GetObjectInput{
		Bucket: aws.String(viper.GetString("aws.s3.bucket")),
		Key:    aws.String(s3Key),
	})

	urlStr, err := req.Presign(expire)
	if err != nil {
		log.Println("Failed to sign request", err)
		return "", err
	}

	logEntity.Infoln("The URL is", urlStr)

	return urlStr, err
}
