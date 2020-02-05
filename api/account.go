package api

import (
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/bitmark-inc/spring-app-api/store"
	"github.com/gocraft/work"
	log "github.com/sirupsen/logrus"

	"github.com/gin-gonic/gin"
)

func (s *Server) accountRegister(c *gin.Context) {
	accountNumber := c.GetString("requester")

	account, err := s.store.QueryAccount(c, &store.AccountQueryParam{
		AccountNumber: &accountNumber,
	})
	if shouldInterupt(err, c) {
		return
	}

	if account != nil {
		abortWithEncoding(c, http.StatusForbidden, errorAccountTaken)
		return
	}

	var params struct {
		EncPubKey string                 `json:"enc_pub_key"`
		Metadata  map[string]interface{} `json:"metadata"`
	}

	if err := c.BindJSON(&params); err != nil {
		log.Debug(err)
		abortWithEncoding(c, http.StatusBadRequest, errorInvalidParameters)
		return
	}

	// Register data owner
	if err := s.bitSocialClient.NewDataOwner(c, accountNumber); err != nil {
		log.Debug(err)
		abortWithEncoding(c, http.StatusBadGateway, errorInternalServer)
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

func (s *Server) accountDelete(c *gin.Context) {
	account := c.MustGet("account").(*store.Account)

	job, err := s.backgroundEnqueuer.EnqueueUnique("delete_user_data", work.Q{
		"account_number": account.AccountNumber,
	})
	if err != nil {
		log.Debug(err)
		abortWithEncoding(c, http.StatusBadRequest, errorInvalidParameters)
		return
	}
	log.Info("Enqueued job with id:", job.ID)

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
		job, err := s.backgroundEnqueuer.EnqueueUnique("delete_user_data", work.Q{
			"account_number": accountNumber,
		})
		if err != nil {
			log.Debug(err)
			abortWithEncoding(c, http.StatusBadRequest, errorInvalidParameters)
			return
		}
		log.Info("Enqueued job with id:", job.ID)
		result[job.ID] = accountNumber
	}

	// Return success
	c.JSON(http.StatusOK, gin.H{"result": result})
}
