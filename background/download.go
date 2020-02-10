package main

import (
	"context"
	"encoding/hex"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/http/httputil"
	"strconv"

	"github.com/RichardKnop/machinery/v1/tasks"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/bitmark-inc/spring-app-api/store"
	"github.com/getsentry/sentry-go"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"golang.org/x/crypto/sha3"
)

func (b *BackgroundContext) downloadArchive(ctx context.Context, fileURL, rawCookie, accountNumber string, archiveid int64) error {
	logEntity := log.WithField("prefix", "download_archive")

	req, err := http.NewRequestWithContext(ctx, "GET", fileURL, nil)
	if err != nil {
		logEntity.Error(err)
		return err
	}

	req.Header.Set("Cookie", rawCookie)

	reqDump, err := httputil.DumpRequest(req, true)
	if err != nil {
		logEntity.Error(err)
	}
	logEntity.WithField("dump", string(reqDump)).Info("Request dump")

	resp, err := b.httpClient.Do(req)
	if err != nil {
		logEntity.Error(err)
		return err
	}

	// Print out the response in console log
	dumpBytes, err := httputil.DumpResponse(resp, false)
	if err != nil {
		logEntity.Error(err)
	}
	dump := string(dumpBytes)
	logEntity.Info("response: ", dump)

	if resp.StatusCode > 300 {
		logEntity.Error("Request failed")
		sentry.CaptureException(errors.New("Request failed"))
		return nil
	}

	sess := session.New(b.awsConf)
	svc := s3manager.NewUploader(sess)

	_, p, err := mime.ParseMediaType(resp.Header.Get("Content-Disposition"))
	if err != nil {
		logEntity.Error(err)
		sentry.CaptureException(err)
		return nil
	}
	filename := p["filename"]

	h := sha3.New512()
	teeReader := io.TeeReader(resp.Body, h)

	defer resp.Body.Close()

	logEntity.Info("Start uploading to S3")

	s3key := "archives/" + accountNumber + "/" + filename
	_, err = svc.Upload(&s3manager.UploadInput{
		Bucket: aws.String(viper.GetString("aws.s3.bucket")),
		Key:    aws.String(s3key),
		Body:   teeReader,
		Metadata: map[string]*string{
			"url":        aws.String(fileURL),
			"archive_id": aws.String(strconv.FormatInt(archiveid, 10)),
		},
	})

	if err != nil {
		logEntity.Error(err)
		return err
	}

	// Get fingerprint
	fingerprintBytes := h.Sum(nil)
	fingerprint := hex.EncodeToString(fingerprintBytes)

	_, err = b.store.UpdateFBArchiveStatus(ctx, &store.FBArchiveQueryParam{
		ID: &archiveid,
	}, &store.FBArchiveQueryParam{
		S3Key:       &s3key,
		Status:      &store.FBArchiveStatusStored,
		ContentHash: &fingerprint,
	})
	if err != nil {
		logEntity.Error(err)
		return err
	}

	server.SendTask(&tasks.Signature{
		Name: jobUploadArchive,
		Args: []tasks.Arg{
			{
				Type:  "string",
				Value: s3key,
			},
			{
				Type:  "string",
				Value: accountNumber,
			},
			{
				Type:  "int64",
				Value: archiveid,
			},
		},
	})

	logEntity.Info("Finish...")

	return nil
}
