package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/RichardKnop/machinery/v1/tasks"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"

	"github.com/bitmark-inc/spring-app-api/archives/facebook"
	"github.com/bitmark-inc/spring-app-api/s3util"
	"github.com/bitmark-inc/spring-app-api/store"
)

// uploadArchive allows users to upload data archives
func (s *Server) uploadArchive(c *gin.Context) {
	var params struct {
		ArchiveType string `form:"type" binding:"required"`
		ArchiveSize int64  `form:"size" binding:"required"`
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

	sess := session.New(s.awsConf)

	uploadInfo, err := s3util.NewArchiveUpload(sess, account.AccountNumber, params.ArchiveType, params.ArchiveSize, archiveRecord.ID)
	if err != nil {
		if err := s.store.InvalidFBArchive(c, &store.FBArchiveQueryParam{
			ID:    &archiveRecord.ID,
			Error: &facebook.ErrFailToCreateArchive,
		}); err != nil {
			log.WithField("archvie_id", archiveRecord.ID).WithField("action", "InvalidFBArchive").Warn(err.Error())
			abortWithEncoding(c, http.StatusBadRequest, errorInternalServer)
			return
		}

		log.WithError(err).Debug("fail to create a upload link")
		abortWithEncoding(c, http.StatusBadRequest, errorInternalServer)
		return
	}

	c.JSON(http.StatusOK, gin.H{"result": uploadInfo})
}

// uploadArchiveByURL allows users to upload data archives using a given url
func (s *Server) uploadArchiveByURL(c *gin.Context) {
	var params struct {
		FileURL     string `json:"file_url"`
		ArchiveType string `json:"archive_type"`
		RawCookie   string `json:"raw_cookie"` // FIXME: this is for facebook automated downloading
		StartedAt   int64  `json:"started_at"`
		EndedAt     int64  `json:"ended_at"`
	}

	if err := c.BindJSON(&params); err != nil {
		log.Debug(err)
		abortWithEncoding(c, http.StatusBadRequest, errorInvalidParameters)
		return
	}

	// FIXME: this is hardcoded for facebook automate downloading
	archiveType := params.ArchiveType
	if params.RawCookie != "" {
		archiveType = "facebook"
	}

	account := c.MustGet("account").(*store.Account)

	archiveRecord, err := s.store.AddFBArchive(c, account.AccountNumber, time.Unix(params.StartedAt, 0), time.Now())
	if shouldInterupt(err, c) {
		return
	}

	_, err = s.store.UpdateFBArchiveStatus(c, &store.FBArchiveQueryParam{
		ID: &archiveRecord.ID,
	}, &store.FBArchiveQueryParam{
		Status: &store.FBArchiveStatusSubmitted,
	})
	if shouldInterupt(err, c) {
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
				Value: archiveType,
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
		abortWithEncoding(c, http.StatusBadRequest, errorInternalServer)
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

// uploadArchive allows users to upload data archives
func (s *Server) adminAckArchiveUploaded(c *gin.Context) {
	var params struct {
		FileKey  string `json:"file_key"`
		FileHash string `json:"file_hash"`
	}

	if err := c.BindJSON(&params); err != nil {
		log.Debug(err)
		abortWithEncoding(c, http.StatusBadRequest, errorInvalidParameters)
		return
	}

	keys := strings.Split(params.FileKey, "/")
	if len(keys) < 5 || keys[4] != "archive.zip" {
		log.Debug(fmt.Errorf("invalid archive file key"))
		abortWithEncoding(c, http.StatusBadRequest, errorInternalServer)
		return
	}
	// %s/%s/archives/%d/%s, accountNumber, archiveType, archiveID, "archive.zip"

	archiveID, err := strconv.ParseInt(keys[3], 10, 64)
	if err != nil {
		log.Debug(err)
		abortWithEncoding(c, http.StatusBadRequest, errorInternalServer)
		return
	}

	_, err = s.store.UpdateFBArchiveStatus(c, &store.FBArchiveQueryParam{
		ID: &archiveID,
	}, &store.FBArchiveQueryParam{
		S3Key:       &params.FileKey,
		Status:      &store.FBArchiveStatusSubmitted,
		ContentHash: &params.FileHash, //  FIXME: calculate later
	})

	if err != nil {
		log.Debug(err)
		abortWithEncoding(c, http.StatusBadRequest, errorInternalServer)
		return
	}

	// FIXME: this cause additional download the archive file
	if _, err := s.backgroundEnqueuer.SendTask(&tasks.Signature{
		Name: "generate_hash_content",
		Args: []tasks.Arg{
			{
				Type:  "string",
				Value: params.FileKey,
			},
			{
				Type:  "int64",
				Value: archiveID,
			},
		},
	}); err != nil {
		panic(err)
	}

	job, err := s.backgroundEnqueuer.SendTask(&tasks.Signature{
		Name: "parse_archive",
		Args: []tasks.Arg{
			{
				Type:  "string",
				Value: keys[1],
			},
			{
				Type:  "string",
				Value: keys[0],
			},
			{
				Type:  "int64",
				Value: archiveID,
			},
		},
	})

	if err != nil {
		log.Debug(err)
		abortWithEncoding(c, http.StatusBadRequest, errorInternalServer)
		return
	}
	log.Info("Enqueued job with id:", job.Signature.UUID)

	c.JSON(http.StatusOK, gin.H{"result": ""})
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
