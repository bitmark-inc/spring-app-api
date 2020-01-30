package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
)

func (s *Server) metricAccountCreation(c *gin.Context) {
	var params struct {
		From time.Time `form:"from" time_format:"unix"`
		To   time.Time `form:"to" time_format:"unix"`
	}

	if err := c.BindQuery(&params); err != nil {
		log.Debug(err)
		abortWithEncoding(c, http.StatusBadRequest, errorInvalidParameters)
		return
	}

	if params.From.After(params.To) {
		abortWithEncoding(c, http.StatusBadRequest, errorInvalidParameters)
		return
	}

	count, err := s.store.CountAccountCreation(c, params.From, params.To)
	if shouldInterupt(err, c) {
		return
	}

	c.JSON(http.StatusOK, gin.H{"result": gin.H{"total": count}})
}
