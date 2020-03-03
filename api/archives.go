package api

import (
	"archive/zip"
	"bytes"
	"encoding/hex"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/RichardKnop/machinery/v1/tasks"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
	"golang.org/x/crypto/sha3"

	"github.com/bitmark-inc/spring-app-api/s3util"
	"github.com/bitmark-inc/spring-app-api/store"
)

// uploadArchive allows users to upload data archives
func (s *Server) uploadArchive(c *gin.Context) {
	var params struct {
		ArchiveType string `form:"type" binding:"required"`
	}

	if err := c.BindQuery(&params); err != nil {
		log.Debug(err)
		abortWithEncoding(c, http.StatusBadRequest, errorInvalidParameters)
		return
	}

	account := c.MustGet("account").(*store.Account)
	archiveRecord, err := s.store.AddFBArchive(c, account.AccountNumber, time.Unix(0, 0), time.Now())
	if shouldInterupt(err, c) {
		return
	}

	fileBytes, err := c.GetRawData()
	if shouldInterupt(err, c) {
		return
	}

	switch http.DetectContentType(fileBytes) {
	case "application/zip":
		requiredDir := map[string]struct{}{
			"photos_and_videos/": {},
			"posts/":             {},
			"friends/":           {},
		}
		z, err := zip.NewReader(bytes.NewReader(fileBytes), int64(len(fileBytes)))
		if shouldInterupt(err, c) {
			return
		}

		for _, f := range z.File {
			if f.Mode().IsDir() {
				if _, ok := requiredDir[f.Name]; ok {
					delete(requiredDir, f.Name)
				}
			}
		}

		if len(requiredDir) != 0 {
			abortWithEncoding(c, http.StatusBadRequest, errorInvalidParameters)
			return
		}

	default:
		abortWithEncoding(c, http.StatusBadRequest, errorInvalidParameters)
		return
	}

	h := sha3.New512()
	teeReader := io.TeeReader(bytes.NewBuffer(fileBytes), h)

	sess := session.New(s.awsConf)
	s3key, err := s3util.UploadArchive(sess, teeReader, account.AccountNumber, "archive.zip", params.ArchiveType, archiveRecord.ID, map[string]*string{
		"archive_type": aws.String(params.ArchiveType),
		"archive_id":   aws.String(strconv.FormatInt(archiveRecord.ID, 10)),
	})
	if err != nil {
		log.Debug(err)
		abortWithEncoding(c, http.StatusInternalServerError, errorInternalServer)
		return
	}

	fingerprintBytes := h.Sum(nil)
	fingerprint := hex.EncodeToString(fingerprintBytes)

	_, err = s.store.UpdateFBArchiveStatus(c, &store.FBArchiveQueryParam{
		ID: &archiveRecord.ID,
	}, &store.FBArchiveQueryParam{
		S3Key:       &s3key,
		Status:      &store.FBArchiveStatusStored,
		ContentHash: &fingerprint,
	})
	if err != nil {
		log.Debug(err)
		abortWithEncoding(c, http.StatusBadRequest, errorInternalServer)
		return
	}

	job, err := s.backgroundEnqueuer.SendTask(&tasks.Signature{
		Name: "parse_archive",
		Args: []tasks.Arg{
			{
				Type:  "string",
				Value: params.ArchiveType,
			},
			{
				Type:  "string",
				Value: account.AccountNumber,
			},
			{
				Type:  "int64",
				Value: archiveRecord.ID,
			},
		},
	})
	if err != nil {
		log.Debug(err)
		abortWithEncoding(c, http.StatusBadRequest, errorInvalidParameters)
		return
	}
	log.Info("Enqueued job with id:", job.Signature.UUID)

	c.JSON(http.StatusAccepted, gin.H{"result": "ok"})
}

// uploadArchiveByURL allows users to upload data archives using a given url
func (s *Server) uploadArchiveByURL(c *gin.Context) {
	var params struct {
		FileURL     string `json:"file_url"`
		ArchiveType string `json:"archive_type"`
		StartedAt   int64  `json:"started_at"`
		EndedAt     int64  `json:"ended_at"`
	}

	if err := c.BindJSON(&params); err != nil {
		log.Debug(err)
		abortWithEncoding(c, http.StatusBadRequest, errorInvalidParameters)
		return
	}

	account := c.MustGet("account").(*store.Account)
	archiveRecord, err := s.store.AddFBArchive(c, account.AccountNumber, time.Unix(params.StartedAt, 0), time.Unix(params.EndedAt, 0))
	shouldInterupt(err, c)

	_, err = s.store.UpdateFBArchiveStatus(c, &store.FBArchiveQueryParam{
		ID: &archiveRecord.ID,
	}, &store.FBArchiveQueryParam{
		Status: &store.FBArchiveStatusSubmitted,
	})
	if err != nil {
		log.Debug(err)
		abortWithEncoding(c, http.StatusBadRequest, errorInternalServer)
		return
	}

	job, err := s.backgroundEnqueuer.SendTask(&tasks.Signature{
		Name: "download_archive",
		Args: []tasks.Arg{
			{
				Type:  "string",
				Value: params.FileURL,
			},
			{
				Type:  "string",
				Value: params.ArchiveType,
			},
			{
				Type:  "string",
				Value: "",
			},
			{
				Type:  "string",
				Value: account.AccountNumber,
			},
			{
				Type:  "int64",
				Value: archiveRecord.ID,
			},
		},
	})
	if err != nil {
		log.Debug(err)
		abortWithEncoding(c, http.StatusBadRequest, errorInvalidParameters)
		return
	}
	log.Info("Enqueued job with id:", job.Signature.UUID)

	c.JSON(http.StatusAccepted, gin.H{"result": "ok"})
}

func (s *Server) downloadFBArchive(c *gin.Context) {
	var params struct {
		Headers   map[string]string `json:"headers"`
		FileURL   string            `json:"file_url"`
		RawCookie string            `json:"raw_cookie"`
		StartedAt int64             `json:"started_at"`
		EndedAt   int64             `json:"ended_at"`
	}

	if err := c.BindJSON(&params); err != nil {
		log.Debug(err)
		abortWithEncoding(c, http.StatusBadRequest, errorInvalidParameters)
		return
	}

	account := c.MustGet("account").(*store.Account)
	archiveRecord, err := s.store.AddFBArchive(c, account.AccountNumber, time.Unix(params.StartedAt, 0), time.Unix(params.EndedAt, 0))
	shouldInterupt(err, c)

	job, err := s.backgroundEnqueuer.SendTask(&tasks.Signature{
		Name: "download_archive",
		Args: []tasks.Arg{
			{
				Type:  "string",
				Value: params.FileURL,
			},
			{
				Type:  "string",
				Value: params.RawCookie,
			},
			{
				Type:  "string",
				Value: account.AccountNumber,
			},
			{
				Type:  "int64",
				Value: archiveRecord.ID,
			},
		},
	})
	if err != nil {
		log.Debug(err)
		abortWithEncoding(c, http.StatusBadRequest, errorInvalidParameters)
		return
	}
	log.Info("Enqueued job with id:", job.Signature.UUID)

	c.JSON(http.StatusAccepted, gin.H{"result": "ok"})
}

func (s *Server) getAllArchives(c *gin.Context) {
	account := c.MustGet("account").(*store.Account)

	archives, err := s.store.GetFBArchives(c, &store.FBArchiveQueryParam{
		AccountNumber: &account.AccountNumber,
	})

	if shouldInterupt(err, c) {
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"result": archives,
	})
}

func (s *Server) adminSubmitArchives(c *gin.Context) {
	var params struct {
		Ids []int64 `json:"ids"`
	}

	if err := c.BindJSON(&params); err != nil {
		log.Debug(err)
		abortWithEncoding(c, http.StatusBadRequest, errorInvalidParameters)
		return
	}

	result := make(map[string]store.FBArchive)
	for _, id := range params.Ids {
		archives, err := s.store.GetFBArchives(c, &store.FBArchiveQueryParam{
			ID: &id,
		})
		if len(archives) != 1 {
			continue
		}

		archive := archives[0]

		job, err := s.backgroundEnqueuer.SendTask(&tasks.Signature{
			Name: "upload_archive",
			Args: []tasks.Arg{
				{
					Type:  "string",
					Value: archive.S3Key,
				},
				{
					Type:  "string",
					Value: archive.AccountNumber,
				},
				{
					Type:  "int64",
					Value: archive.ID,
				},
			},
		})
		if err != nil {
			log.Debug(err)
			abortWithEncoding(c, http.StatusBadRequest, errorInvalidParameters)
			return
		}
		log.Info("Enqueued job with id:", job.Signature.UUID)
		result[job.Signature.UUID] = archive
	}

	c.JSON(http.StatusAccepted, result)
}

func (s *Server) adminForceParseArchive(c *gin.Context) {
	var params struct {
		AccountNumbers []string `json:"account_numbers"`
	}

	if err := c.BindJSON(&params); err != nil {
		log.Debug(err)
		abortWithEncoding(c, http.StatusBadRequest, errorInvalidParameters)
		return
	}

	result := make(map[string]string)
	for _, accountNumber := range params.AccountNumbers {
		archives, err := s.store.GetFBArchives(c, &store.FBArchiveQueryParam{
			AccountNumber: &accountNumber,
		})
		if err != nil {
			log.Debug(err)
			abortWithEncoding(c, http.StatusBadRequest, errorInvalidParameters)
			return
		}
		if len(archives) == 0 {
			continue
		}

		job, err := s.backgroundEnqueuer.SendTask(&tasks.Signature{
			Name: "analyze_posts",
			Args: []tasks.Arg{
				{
					Type:  "string",
					Value: accountNumber,
				},
				{
					Type:  "int64",
					Value: archives[0].ID,
				},
			},
		})
		if err != nil {
			log.Debug(err)
			abortWithEncoding(c, http.StatusBadRequest, errorInvalidParameters)
			return
		}

		if _, err := s.backgroundEnqueuer.SendTask(&tasks.Signature{
			Name: "extract_time_metadata",
			Args: []tasks.Arg{
				{
					Type:  "string",
					Value: accountNumber,
				},
			},
		}); err != nil {
			log.Debug(err)
			abortWithEncoding(c, http.StatusBadRequest, errorInvalidParameters)
			return
		}
		log.Info("Enqueued job with id:", job.Signature.UUID)
		result[job.Signature.UUID] = accountNumber
	}

	c.JSON(http.StatusAccepted, result)
}

func (s *Server) adminGenerateHashContent(c *gin.Context) {
	var params struct {
		Ids []int64 `json:"ids"`
	}

	if err := c.BindJSON(&params); err != nil {
		log.Debug(err)
		abortWithEncoding(c, http.StatusBadRequest, errorInvalidParameters)
		return
	}

	result := make(map[string]store.FBArchive)
	for _, id := range params.Ids {
		archives, err := s.store.GetFBArchives(c, &store.FBArchiveQueryParam{
			ID: &id,
		})
		if len(archives) != 1 {
			continue
		}

		archive := archives[0]
		if archive.S3Key == "" {
			continue
		}

		job, err := s.backgroundEnqueuer.SendTask(&tasks.Signature{
			Name: "generate_hash_content",
			Args: []tasks.Arg{
				{
					Type:  "string",
					Value: archive.S3Key,
				},
				{
					Type:  "int64",
					Value: archives[0].ID,
				},
			},
		})
		if err != nil {
			log.Debug(err)
			abortWithEncoding(c, http.StatusBadRequest, errorInvalidParameters)
			return
		}
		log.Info("Enqueued job with id:", job.Signature.UUID)
		result[job.Signature.UUID] = archive
	}

	c.JSON(http.StatusAccepted, result)
}
