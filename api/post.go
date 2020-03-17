package api

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/bitmark-inc/spring-app-api/protomodel"
	"github.com/bitmark-inc/spring-app-api/s3util"
	"github.com/bitmark-inc/spring-app-api/store"
	"github.com/gin-gonic/gin"
	"github.com/golang/protobuf/proto"
	log "github.com/sirupsen/logrus"
)

func (s *Server) getAllPosts(c *gin.Context) {
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

	data, err := s.fbDataStore.GetFBStat(c, accountNumber+"/post", params.StartedAt, params.EndedAt, params.Limit)
	if shouldInterupt(err, c) {
		return
	}

	posts := make([]*protomodel.Post, 0)
	for _, d := range data {
		var post protomodel.Post
		err := proto.Unmarshal(d, &post)
		if shouldInterupt(err, c) {
			return
		}

		posts = append(posts, &post)
	}

	responseWithEncoding(c, http.StatusOK, &protomodel.PostsResponse{
		Result: posts,
	})
}

func (s *Server) getPostStats(c *gin.Context) {
	accountNumber := c.GetString("requester")
	period := c.Param("period")
	startedAt, err := strconv.ParseInt(c.Query("started_at"), 10, 64)

	if err != nil {
		log.Debug(err)
		abortWithEncoding(c, http.StatusBadRequest, errorInvalidParameters)
		return
	}

	results := make([]*protomodel.Usage, 0)

	// For post
	postStatData, err := s.fbDataStore.GetExactFBStat(c, fmt.Sprintf("%s/post-%s-stat", accountNumber, period), startedAt)
	if shouldInterupt(err, c) {
		return
	}

	if postStatData != nil {
		var postStat protomodel.Usage
		err := proto.Unmarshal(postStatData, &postStat)
		if shouldInterupt(err, c) {
			return
		}
		results = append(results, &postStat)
	}

	// For reaction
	reactionStatData, err := s.fbDataStore.GetExactFBStat(c, fmt.Sprintf("%s/reaction-%s-stat", accountNumber, period), startedAt)
	if shouldInterupt(err, c) {
		return
	}

	if reactionStatData != nil {
		var reactionStat protomodel.Usage
		err := proto.Unmarshal(reactionStatData, &reactionStat)
		if shouldInterupt(err, c) {
			return
		}
		results = append(results, &reactionStat)
	}

	// For sentiment
	sentimentStatData, err := s.fbDataStore.GetExactFBStat(c, fmt.Sprintf("%s/sentiment-%s-stat", accountNumber, period), startedAt)
	if shouldInterupt(err, c) {
		return
	}

	if sentimentStatData != nil {
		var sentimentStat protomodel.Usage
		err := proto.Unmarshal(sentimentStatData, &sentimentStat)
		if shouldInterupt(err, c) {
			return
		}
		results = append(results, &sentimentStat)
	}

	responseWithEncoding(c, http.StatusOK, &protomodel.UsageResponse{
		Result: results,
	})
}

func (s *Server) getAllPostMedia(c *gin.Context) {
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

	results := make([]*struct {
		Id                string `json:"id"`
		MediaURI          string `json:"source"`
		ThumbnailURI      string `json:"thumbnail"`
		FilenameExtension string `json:"extension"`
		Timestamp         int64  `json:"timestamp"`
	}, 0)

	if err := s.ormDB.Table("facebook_postmedia").
		Joins("LEFT OUTER JOIN facebook_post ON facebook_postmedia.post_id = facebook_post.id").
		Select("facebook_postmedia.id, facebook_postmedia.media_uri, facebook_postmedia.thumbnail_uri, facebook_postmedia.filename_extension, facebook_post.timestamp").
		Where("facebook_postmedia.data_owner_id = ?", accountNumber).
		Where("facebook_post.timestamp > ?", params.StartedAt).
		Where("facebook_post.timestamp < ?", params.EndedAt).
		Order("facebook_post.timestamp desc").
		Limit(params.Limit).Scan(&results).Error; err != nil {
		abortWithEncoding(c, http.StatusInternalServerError, errorInternalServer)
		return
	}

	sess := session.New(s.awsConf)

	for _, r := range results {
		if url, err := s3util.GetMediaPresignedURL(sess, r.MediaURI, 10*time.Minute); err != nil {
			abortWithEncoding(c, http.StatusInternalServerError, errorInternalServer)
			return
		} else {
			r.MediaURI = url
		}

		if url, err := s3util.GetMediaPresignedURL(sess, r.ThumbnailURI, 10*time.Minute); err != nil {
			abortWithEncoding(c, http.StatusInternalServerError, errorInternalServer)
			return
		} else {
			r.ThumbnailURI = url
		}
	}

	c.JSON(http.StatusOK, gin.H{"result": results})
}

func (s *Server) getPostMediaURI(c *gin.Context) {
	key := c.Query("key")
	s3Key, err := url.QueryUnescape(key)
	if key == "" {
		abortWithEncoding(c, http.StatusBadRequest, errorInvalidParameters)
		return
	}

	sess := session.New(s.awsConf)
	url, err := s3util.GetMediaPresignedURL(sess, s3Key, 5*time.Minute)
	if shouldInterupt(err, c) {
		return
	}

	c.Redirect(http.StatusSeeOther, url)
}

func (s *Server) postsCountStats(c *gin.Context) {
	var params struct {
		From time.Time `form:"started_at" time_format:"unix"`
		To   time.Time `form:"ended_at" time_format:"unix"`
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
	log.Debug(params.From, params.To)

	account := c.MustGet("account").(*store.Account)

	allStatsRows, err := s.ormDB.Raw(`SELECT post_type, count(post_type) FROM (
		SELECT (CASE WHEN media_attached IS TRUE THEN 'media'
			  		 WHEN (external_context_url IS NOT NULL AND external_context_url <> '') THEN 'link'
			         WHEN post is not null AND post <> '' THEN 'update'
			  	     ELSE 'undefined' END) AS post_type FROM facebook_post WHERE timestamp > ? AND timestamp < ?
		) AS t GROUP BY post_type`, params.From.Unix(), params.To.Unix()).Rows()
	if err != nil {
		log.Debug(err)
		abortWithEncoding(c, http.StatusBadRequest, errorInternalServer)
		return
	}
	defer allStatsRows.Close()

	accountStatsRows, err := s.ormDB.Raw(`SELECT post_type, count(post_type) FROM (
		SELECT (CASE WHEN media_attached IS TRUE THEN 'media'
			  		 WHEN (external_context_url IS NOT NULL AND external_context_url <> '') THEN 'link'
			         WHEN post is not null AND post <> '' THEN 'update'
			  	     ELSE 'undefined' END) AS post_type FROM facebook_post WHERE timestamp > ? AND timestamp < ? AND data_owner_id = ?
		) AS t GROUP BY post_type;`, params.From.Unix(), params.To.Unix(), account.AccountNumber).Rows()
	if err != nil {
		log.Debug(err)
		abortWithEncoding(c, http.StatusInternalServerError, errorInternalServer)
		return
	}
	defer accountStatsRows.Close()

	postTypes := []string{"link", "media", "undefined", "update"}
	stats := map[string]map[string]int64{}
	for _, r := range postTypes {
		stats[r] = map[string]int64{
			"sys_avg": 0,
			"count":   0,
		}
	}

	for allStatsRows.Next() {
		var t string
		var count int64
		if err := allStatsRows.Scan(&t, &count); err != nil {
			abortWithEncoding(c, http.StatusInternalServerError, errorInternalServer)
			return
		}
		if _, ok := stats[t]; ok {
			stats[t]["sys_avg"] = count
		}
	}

	for accountStatsRows.Next() {
		var t string
		var count int64
		if err := accountStatsRows.Scan(&t, &count); err != nil {
			abortWithEncoding(c, http.StatusInternalServerError, errorInternalServer)
			return
		}
		if _, ok := stats[t]; ok {
			stats[t]["count"] = count
		}
	}

	c.JSON(http.StatusOK, gin.H{"result": stats})
}

func (s *Server) reactionsCountStats(c *gin.Context) {
	var params struct {
		From time.Time `form:"started_at" time_format:"unix"`
		To   time.Time `form:"ended_at" time_format:"unix"`
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

	account := c.MustGet("account").(*store.Account)

	allStatsRows, err := s.ormDB.Raw(`SELECT reaction, count(reaction)
		FROM facebook_reaction WHERE timestamp > ? AND timestamp < ?
		GROUP BY reaction`, params.From.Unix(), params.To.Unix()).Rows()
	if err != nil {
		log.Debug(err)
		abortWithEncoding(c, http.StatusInternalServerError, errorInternalServer)
		return
	}
	defer allStatsRows.Close()

	accountStatsRows, err := s.ormDB.Raw(`SELECT reaction, count(reaction)
		FROM facebook_reaction WHERE timestamp > ? AND timestamp < ? AND data_owner_id = ?
		GROUP BY reaction`, params.From.Unix(), params.To.Unix(), account.AccountNumber).Rows()
	if err != nil {
		log.Debug(err)
		abortWithEncoding(c, http.StatusInternalServerError, errorInternalServer)
		return
	}
	defer accountStatsRows.Close()

	reactionTypes := []string{"ANGER", "HAHA", "LIKE", "LOVE", "SORRY", "WOW"}
	stats := map[string]map[string]int64{}
	for _, r := range reactionTypes {
		stats[r] = map[string]int64{
			"sys_avg": 0,
			"count":   0,
		}
	}

	for allStatsRows.Next() {
		var t string
		var count int64
		if err := allStatsRows.Scan(&t, &count); err != nil {
			abortWithEncoding(c, http.StatusInternalServerError, errorInternalServer)
			return
		}

		if _, ok := stats[t]; ok {
			stats[t]["sys_avg"] = count
		}
	}

	for accountStatsRows.Next() {
		var t string
		var count int64
		if err := accountStatsRows.Scan(&t, &count); err != nil {
			abortWithEncoding(c, http.StatusInternalServerError, errorInternalServer)
			return
		}

		if _, ok := stats[t]; ok {
			stats[t]["count"] = count
		}
	}

	c.JSON(http.StatusOK, gin.H{"result": stats})
}
