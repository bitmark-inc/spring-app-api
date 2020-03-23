package api

import (
	"net/http"
	"time"

	"github.com/bitmark-inc/spring-app-api/schema/facebook"
	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
)

func (s *Server) getAllEvents(c *gin.Context) {
	accountNumber := c.GetString("requester")
	var params struct {
		StartedAt int64 `form:"started_at"`
		EndedAt   int64 `form:"ended_at"`
		Limit     int64 `form:"limit"`
	}

	if err := c.BindQuery(&params); err != nil {
		log.Debug(err)
		abortWithEncoding(c, http.StatusBadRequest, errorInvalidParameters)
		return
	}

	if params.EndedAt == 0 {
		params.EndedAt = time.Now().Unix()
	}

	if params.StartedAt >= params.EndedAt {
		abortWithEncoding(c, http.StatusBadRequest, errorInvalidParameters)
		return
	}

	if params.Limit > 1000 {
		params.Limit = 1000
	}

	if params.Limit < 1 {
		params.Limit = 100
	}

	var events []facebook.EventORM

	if err := s.ormDB.
		Where("data_owner_id = ?", accountNumber).
		Where("start_timestamp >= ? AND start_timestamp < ?", params.StartedAt, params.EndedAt).
		Order("start_timestamp desc").Limit(params.Limit).
		Find(&events).Error; err != nil {
		abortWithEncoding(c, http.StatusInternalServerError, errorInternalServer)
		return
	}

	c.JSON(http.StatusOK, gin.H{"result": events})
}
