package api

import (
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"github.com/RichardKnop/machinery/v1/tasks"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"

	"github.com/bitmark-inc/spring-app-api/s3util"
	"github.com/bitmark-inc/spring-app-api/schema/spring"
	"github.com/bitmark-inc/spring-app-api/store"
)

func (s *Server) accountRegister(c *gin.Context) {
	accountNumber := c.GetString("requester")

	var params struct {
		EncPubKey string                 `json:"enc_pub_key"`
		Metadata  map[string]interface{} `json:"metadata"`
	}

	if err := c.BindJSON(&params); err != nil {
		log.Debug(err)
		abortWithEncoding(c, http.StatusBadRequest, errorInvalidParameters)
		return
	}

	account, err := s.store.QueryAccount(c, &store.AccountQueryParam{
		AccountNumber: &accountNumber,
	})
	if shouldInterupt(err, c) {
		return
	}

	if account != nil {
		if account.Deleting {
			abortWithEncoding(c, http.StatusBadRequest, errorAccountDeleting)
			return
		}
		abortWithEncoding(c, http.StatusForbidden, errorAccountTaken)
		return
	}

	// Save to db
	encPubKey, err := hex.DecodeString(params.EncPubKey)
	if err != nil {
		log.Debug(err)
		abortWithEncoding(c, http.StatusBadRequest, errorInvalidParameters)
		return
	}

	clientType := strings.ToLower(c.GetHeader("Client-Type"))
	if params.Metadata == nil {
		params.Metadata = make(map[string]interface{})
	}

	params.Metadata["platform"] = clientType
	account, err = s.store.InsertAccount(c, accountNumber, encPubKey, params.Metadata)
	if shouldInterupt(err, c) {
		return
	}

	c.JSON(http.StatusCreated, gin.H{"result": account})
}

func (s *Server) accountDetail(c *gin.Context) {
	accountNumber := c.GetString("account_number")

	log.Debug("Check data owner")

	// Check if the account status
	status, err := s.bitSocialClient.GetDataOwnerStatus(c, accountNumber)
	if err != nil {
		log.Debug(err)
		abortWithEncoding(c, http.StatusBadGateway, errorInternalServer)
		return
	}

	if status == "DELETING" {
		abortWithEncoding(c, http.StatusBadRequest, errorAccountDeleting)
		return
	}

	account, err := s.store.QueryAccount(c, &store.AccountQueryParam{
		AccountNumber: &accountNumber,
	})
	if shouldInterupt(err, c) {
		return
	}

	if account == nil {
		abortWithEncoding(c, http.StatusUnauthorized, errorAccountNotFound)
		return
	}

	c.JSON(http.StatusOK, gin.H{"result": account})
}

func (s *Server) meRoute(meAlias string) gin.HandlerFunc {
	return func(c *gin.Context) {
		accountNumber := c.Param("account_number")
		if accountNumber == meAlias {
			accountNumber = c.GetString("requester")
			c.Set("me", true)
		}
		c.Set("account_number", accountNumber)
	}
}

func (s *Server) accountUpdateMetadata(c *gin.Context) {
	var params struct {
		Metadata map[string]interface{} `json:"metadata"`
	}

	if err := c.BindJSON(&params); err != nil {
		log.Debug(err)
		abortWithEncoding(c, http.StatusBadRequest, errorInvalidParameters)
		return
	}

	account := c.MustGet("account").(*store.Account)

	account, err := s.store.UpdateAccountMetadata(c, &store.AccountQueryParam{
		AccountNumber: &account.AccountNumber,
	}, params.Metadata)
	if shouldInterupt(err, c) {
		return
	}

	c.JSON(http.StatusOK, gin.H{"result": account})
}

func (s *Server) accountPrepareExport(c *gin.Context) {
	accountNumber := c.GetString("account_number")
	jobID := uuid.New()

	a := &spring.ArchiveORM{
		JobID:         &jobID,
		Status:        "PENDING",
		AccountNumber: accountNumber,
	}
	if err := s.ormDB.Create(a).Error; err != nil {
		log.Debug(err)
		abortWithEncoding(c, http.StatusInternalServerError, errorInternalServer)
		return
	}

	job, err := s.backgroundEnqueuer.SendTask(&tasks.Signature{
		UUID: jobID.String(),
		Name: "prepare_data_export",
		Args: []tasks.Arg{
			{
				Type:  "string",
				Value: accountNumber,
			},
			{
				Type:  "string",
				Value: a.ID.String(),
			},
		},
	})
	if err != nil {
		log.Debug(err)
		abortWithEncoding(c, http.StatusInternalServerError, errorInternalServer)
		return
	}

	log.Info("Enqueued job with id:", job.Signature.UUID)

	// Return success
	c.JSON(http.StatusOK, gin.H{"result": "OK"})
}

func (s *Server) accountExportStatus(c *gin.Context) {
	accountNumber := c.GetString("account_number")

	var a spring.ArchiveORM

	if err := s.ormDB.Where("account_number = ?", accountNumber).Order("created_at desc").First(&a).Error; err != nil {
		log.Debug(err)
		abortWithEncoding(c, http.StatusInternalServerError, errorInternalServer)
		return
	}

	c.JSON(http.StatusOK, gin.H{"result": a})
}

func (s *Server) accountDownloadExport(c *gin.Context) {
	accountNumber := c.GetString("account_number")

	var a spring.ArchiveORM

	if err := s.ormDB.Where("account_number = ?", accountNumber).Order("created_at desc").First(&a).Error; err != nil {
		log.Debug(err)
		abortWithEncoding(c, http.StatusInternalServerError, errorInternalServer)
		return
	}

	sess, err := session.NewSession(s.awsConf)
	if err != nil {
		log.Debug(err)
		abortWithEncoding(c, http.StatusInternalServerError, errorInternalServer)
		return
	}

	url, err := s3util.GetMediaPresignedURL(sess, a.FileKey, 5*time.Minute)
	if err != nil {
		log.Debug(err)
		abortWithEncoding(c, http.StatusInternalServerError, errorInternalServer)
		return
	}

	c.Redirect(http.StatusSeeOther, url)
}

func (s *Server) accountDelete(c *gin.Context) {
	account := c.MustGet("account").(*store.Account)

	if err := s.ormDB.Model(&spring.AccountORM{
		AccountNumber: account.AccountNumber,
	}).Update("deleting", true).Error; err != nil {
		log.Debug(err)
		abortWithEncoding(c, http.StatusInternalServerError, errorInternalServer)
		return
	}

	job, err := s.backgroundEnqueuer.SendTask(&tasks.Signature{
		Name: "delete_user_data",
		Args: []tasks.Arg{
			{
				Type:  "string",
				Value: account.AccountNumber,
			},
		},
	})
	if err != nil {
		log.Debug(err)
		abortWithEncoding(c, http.StatusInternalServerError, errorInternalServer)
		return
	}
	log.Info("Enqueued job with id:", job.Signature.UUID)

	// Return success
	c.JSON(http.StatusOK, gin.H{"result": "OK"})
}

func (s *Server) adminAccountDelete(c *gin.Context) {
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
		job, err := s.backgroundEnqueuer.SendTask(&tasks.Signature{
			Name: "delete_user_data",
			Args: []tasks.Arg{
				{
					Type:  "string",
					Value: accountNumber,
				},
			},
		})
		if err != nil {
			log.Debug(err)
			abortWithEncoding(c, http.StatusBadRequest, errorInvalidParameters)
			return
		}
		log.Info("Enqueued job with id:", job.Signature.UUID)
		result[job.Signature.UUID] = accountNumber
	}

	// Return success
	c.JSON(http.StatusOK, gin.H{"result": result})
}
