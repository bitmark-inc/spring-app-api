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

type S3PresignRequest struct {
	URL     string              `json:"url"`
	Headers map[string][]string `json:"headers"`
}

func generateS3ArchiveKey(accountNumber, archiveType string, archiveID int64) string {
	return fmt.Sprintf("%s/%s/archives/%d/%s", accountNumber, archiveType, archiveID, "archive.zip")
}

// UploadArchive upload archive files to S3
func UploadArchive(sess *session.Session, data io.Reader, accountNumber, archiveType string, archiveID int64, metadata map[string]*string) (string, error) {
	logEntity := log.WithField("prefix", "s3_util")

	svc := s3manager.NewUploader(sess)

	s3Key := generateS3ArchiveKey(accountNumber, archiveType, archiveID)

	logEntity.WithField("key", s3Key).Info("Start uploading to S3")
	_, err := svc.Upload(&s3manager.UploadInput{
		Bucket:   aws.String(viper.GetString("aws.s3.bucket")),
		Key:      aws.String(s3Key),
		Body:     data,
		Metadata: metadata,
	})
	return s3Key, err
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

func NewArchiveUpload(sess *session.Session, accountNumber, archiveType string, archiveSize, archiveID int64) (S3PresignRequest, error) {
	logEntity := log.WithField("prefix", "s3_util")
	svc := s3.New(sess)

	s3Key := generateS3ArchiveKey(accountNumber, archiveType, archiveID)

	sdkReq, _ := svc.PutObjectRequest(&s3.PutObjectInput{
		Bucket:        aws.String(viper.GetString("aws.s3.bucket")),
		Key:           aws.String(s3Key),
		ContentLength: aws.Int64(archiveSize),
	})

	logEntity.WithField("key", s3Key).WithField("size", archiveSize).Info("Generate a presigned link for uploading to S3")
	u, headers, err := sdkReq.PresignRequest(15 * time.Minute)
	if err != nil {
		return S3PresignRequest{}, err
	}

	return S3PresignRequest{
		URL:     u,
		Headers: headers,
	}, nil
}
