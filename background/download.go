package main

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/RichardKnop/machinery/v1/tasks"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/bitmark-inc/spring-app-api/archives/facebook"
	"github.com/bitmark-inc/spring-app-api/s3util"
	"github.com/bitmark-inc/spring-app-api/store"
	"github.com/getsentry/sentry-go"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"golang.org/x/crypto/sha3"
)

func downloadFromLink(ctx context.Context, httpClient *http.Client, link, rawCookie string) (*http.Response, error) {
	logEntity := log.WithField("prefix", "download_archive")

	var isGoogleSharing bool // This varaible is to determine whethere to follow up google sharing link
	transformedLink := link

	u, err := url.Parse(link)
	if err != nil {
		return nil, err
	}

	switch u.Host {
	case "drive.google.com":
		isGoogleSharing = true
		var fileID string
		switch u.Path {
		case "/open":
			if id, ok := u.Query()["id"]; ok {
				if len(id) > 0 {
					fileID = id[0]
				}
			}
		default:
			paths := strings.Split(u.Path, "/")
			if len(paths) >= 4 {
				fileID = paths[3]
			}
		}

		if fileID == "" {
			return nil, fmt.Errorf("unrecognized link of google sharing")
		}

		transformedLink = fmt.Sprintf("https://drive.google.com/u/0/uc?id=%s&export=download", fileID)

	case "www.dropbox.com":
		u.Host = "dl.dropboxusercontent.com"
		transformedLink = u.String()
	}

	req, err := http.NewRequestWithContext(ctx, "GET", transformedLink, nil)
	if err != nil {
		logEntity.Error(err)
		return nil, err
	}

	if rawCookie != "" {
		req.Header.Set("Cookie", rawCookie)
	}

	reqDump, err := httputil.DumpRequest(req, true)
	if err != nil {
		logEntity.Error(err)
	}
	logEntity.WithField("dump", string(reqDump)).Debug("Request first download")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	contentType := resp.Header.Get("Content-Type")
	// Assume the archive is a zip file
	if contentType == "application/zip" {
		return resp, nil
	} else {
		// This is an additional process for google drive data because of the large file virus scanning
		if isGoogleSharing && strings.Index(contentType, "text/html") != -1 {
			for _, cookie := range resp.Cookies() {
				if strings.Index(cookie.Name, "download_warning") != -1 {
					req, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s&confirm=%s", transformedLink, cookie.Value), nil)
					if err != nil {
						logEntity.Error(err)
						return nil, err
					}

					req.Header.Set("Cookie", cookie.String())

					reqDump, err := httputil.DumpRequest(req, true)
					if err != nil {
						logEntity.Error(err)
					}
					logEntity.WithField("dump", string(reqDump)).Debug("Request second download for Google Drive")
					return httpClient.Do(req)
				}
			}
		}
		return nil, fmt.Errorf("invalid content of downloaded data")
	}
}

func (b *BackgroundContext) downloadArchive(ctx context.Context, fileURL, archiveType, rawCookie, accountNumber string, archiveid int64) error {
	jobError := NewArchiveJobError(archiveid, facebook.ErrFailToDownloadArchive)
	logEntity := log.WithField("prefix", "download_archive")

	resp, err := downloadFromLink(ctx, b.httpClient, fileURL, rawCookie)
	if err != nil {
		logEntity.Error(err)
		return jobError(err)
	}
	defer resp.Body.Close()

	// Print out the response in console log

	if resp.StatusCode > 300 {
		dumpBytes, err := httputil.DumpResponse(resp, true)
		if err != nil {
			logEntity.Error(err)
		}
		logEntity.WithField("dump", string(dumpBytes)).Error("Request failed")
		sentry.CaptureException(errors.New("Request failed"))
		return nil
	} else {
		dumpBytes, err := httputil.DumpResponse(resp, false)
		if err != nil {
			logEntity.Error(err)
		}
		logEntity.WithField("dump", string(dumpBytes)).Debug("Response for downloaded archive file")
	}

	tmpfile, err := ioutil.TempFile(viper.GetString("archive.workdir"), fmt.Sprintf("%s-background-download-", accountNumber))
	if err != nil {
		return jobError(err)
	}
	defer os.Remove(tmpfile.Name())
	defer tmpfile.Close()

	if _, err := io.Copy(tmpfile, resp.Body); err != nil {
		return jobError(err)
	}

	if err := tmpfile.Sync(); err != nil {
		return jobError(err)
	}

	if !facebook.IsValidArchiveFile(tmpfile.Name()) {
		jobError := NewArchiveJobError(archiveid, facebook.ErrInvalidArchive)
		return jobError(fmt.Errorf("invalid archive file"))
	}

	sess := session.New(b.awsConf)

	logEntity.Info("Start uploading to S3")
	if _, err := tmpfile.Seek(0, 0); err != nil {
		logEntity.Error(err)
		return jobError(err)
	}

	h := sha3.New512()
	teeReader := io.TeeReader(tmpfile, h)

	s3key, err := s3util.UploadArchive(sess, teeReader, accountNumber, archiveType, archiveid, map[string]*string{
		"url":          aws.String(fileURL),
		"archive_type": aws.String(archiveType),
		"archive_id":   aws.String(strconv.FormatInt(archiveid, 10)),
	})

	if err != nil {
		logEntity.Error(err)
		return jobError(err)
	}

	// Get fingerprint
	fingerprintBytes := h.Sum(nil)
	fingerprint := hex.EncodeToString(fingerprintBytes)

	_, err = b.store.UpdateFBArchiveStatus(ctx, &store.FBArchiveQueryParam{
		ID: &archiveid,
	}, &store.FBArchiveQueryParam{
		S3Key:       &s3key,
		Status:      &store.FBArchiveStatusSubmitted,
		ContentHash: &fingerprint,
	})
	if err != nil {
		logEntity.Error(err)
		return jobError(err)
	}

	server.SendTask(&tasks.Signature{
		Name: jobParseArchive,
		Args: []tasks.Arg{
			{
				Type:  "string",
				Value: archiveType,
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
