package api

import (
	"context"
	"crypto/rsa"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/RichardKnop/machinery/v1"
	"github.com/aws/aws-sdk-go/aws"
	sentrygin "github.com/getsentry/sentry-go/gin"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/gogo/protobuf/proto"
	"github.com/jinzhu/gorm"
	"github.com/spf13/viper"

	"github.com/bitmark-inc/bitmark-sdk-go/account"
	"github.com/bitmark-inc/spring-app-api/external/fbarchive"
	"github.com/bitmark-inc/spring-app-api/external/onesignal"
	"github.com/bitmark-inc/spring-app-api/logmodule"
	"github.com/bitmark-inc/spring-app-api/store"
)

// Server to run a http server instance
type Server struct {
	// Server instance
	server *http.Server

	// Stores
	store       store.Store
	fbDataStore store.FBDataStore

	// JWT private key
	jwtPrivateKey *rsa.PrivateKey

	// AWS Config
	awsConf *aws.Config

	ormDB *gorm.DB

	// External services
	oneSignalClient *onesignal.OneSignalClient
	bitSocialClient *fbarchive.Client

	// account
	bitmarkAccount *account.AccountV2

	// http client for calling external services
	httpClient *http.Client

	// job pool enqueuer
	backgroundEnqueuer *machinery.Server

	// country continent list
	countryContinentMap map[string]string
	areaFBIncomeMap     *areaFBIncomeMap
}

// NewServer new instance of server
func NewServer(store store.Store,
	fbDataStore store.FBDataStore,
	ormDB *gorm.DB,
	jwtKey *rsa.PrivateKey,
	awsConf *aws.Config,
	bitmarkAccount *account.AccountV2,
	backgroundEnqueuer *machinery.Server) *Server {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}

	httpClient := &http.Client{
		Timeout:   5 * time.Minute,
		Transport: tr,
	}
	return &Server{
		store:              store,
		fbDataStore:        fbDataStore,
		ormDB:              ormDB,
		jwtPrivateKey:      jwtKey,
		awsConf:            awsConf,
		httpClient:         httpClient,
		bitmarkAccount:     bitmarkAccount,
		oneSignalClient:    onesignal.NewClient(httpClient),
		bitSocialClient:    fbarchive.NewClient(httpClient),
		backgroundEnqueuer: backgroundEnqueuer,
	}
}

// Run to run the server
func (s *Server) Run(addr string) error {
	c, err := loadCountryContinentMap()
	if err != nil {
		return err
	}
	s.countryContinentMap = c

	incomeMap, err := loadFBIncomeMap()
	if err != nil {
		return err
	}
	s.areaFBIncomeMap = incomeMap

	s.server = &http.Server{
		Addr:    addr,
		Handler: s.setupRouter(),
	}

	return s.server.ListenAndServe()
}

func (s *Server) setupRouter() *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(sentrygin.New(sentrygin.Options{
		Repanic:         true,
		WaitForDelivery: false,
		Timeout:         10 * time.Second,
	}))

	webhookRoute := r.Group("/webhook")
	webhookRoute.Use(logmodule.Ginrus("Webhook"))
	{
	}

	apiRoute := r.Group("/api")
	apiRoute.Use(logmodule.Ginrus("API"))
	apiRoute.GET("/information", s.information)
	apiRoute.Use(s.clientVersionGateway())

	apiRoute.POST("/auth", s.requestJWT)

	accountRoute := apiRoute.Group("/accounts")
	accountRoute.Use(s.authMiddleware())
	{
		accountRoute.POST("", s.accountRegister)
	}

	accountRoute.Use(s.recognizeAccountMiddleware())
	{
		accountRoute.GET("/me", s.accountDetail)

		accountRoute.PATCH("/me", s.accountUpdateMetadata)
		accountRoute.DELETE("/me", s.accountDelete)

		accountRoute.POST("/me/export", s.accountPrepareExport)
		accountRoute.GET("/me/export", s.accountExportStatus)
		accountRoute.GET("/me/export/download", s.accountDownloadExport)
	}

	archivesRoute := apiRoute.Group("/archives")
	archivesRoute.Use(s.authMiddleware())
	archivesRoute.Use(s.recognizeAccountMiddleware())
	{
		archivesRoute.POST("", s.uploadArchive)
		archivesRoute.POST("url", s.uploadArchiveByURL)
		archivesRoute.GET("", s.getAllArchives)
	}

	postRoute := apiRoute.Group("/posts")
	postRoute.Use(s.authMiddleware())
	postRoute.Use(s.fakeCredential())
	{
		postRoute.GET("", s.getAllPosts)
	}

	photoAndMediaRoute := apiRoute.Group("/photos_and_videos")
	photoAndMediaRoute.Use(s.authMiddleware())
	photoAndMediaRoute.Use(s.fakeCredential())
	{
		photoAndMediaRoute.GET("", s.getAllPostMedia)
	}

	mediaRoute := apiRoute.Group("/media")
	mediaRoute.Use(s.authMiddleware())
	mediaRoute.Use(s.fakeCredential())
	{
		mediaRoute.GET("", s.getPostMediaURI)
	}

	reactionRoute := apiRoute.Group("/reactions")
	reactionRoute.Use(s.authMiddleware())
	reactionRoute.Use(s.fakeCredential())
	{
		reactionRoute.GET("", s.getAllReactions)
	}

	eventRoute := apiRoute.Group("/events")
	eventRoute.Use(s.authMiddleware())
	eventRoute.Use(s.fakeCredential())
	{
		eventRoute.GET("", s.getAllEvents)
	}

	usageRoute := apiRoute.Group("/usage")
	usageRoute.Use(s.authMiddleware())
	usageRoute.Use(s.fakeCredential())
	{
		usageRoute.GET("/:period", s.getPostStats)
	}

	statsRoute := apiRoute.Group("/stats")
	statsRoute.Use(s.authMiddleware())
	statsRoute.Use(s.recognizeAccountMiddleware())
	{
		statsRoute.GET("/posts", s.postsCountStats)
		statsRoute.GET("/reactions", s.reactionsCountStats)
	}

	insightRoute := apiRoute.Group("/insight")
	insightRoute.Use(s.authMiddleware())
	insightRoute.Use(s.fakeCredential())
	insightRoute.Use(s.recognizeAccountMiddleware())
	{
		insightRoute.GET("", s.getInsight)
	}

	assetRoute := r.Group("/assets")
	assetRoute.Use(logmodule.Ginrus("Asset"))
	{
		assetRoute.Static("", viper.GetString("server.assetdir"))
	}

	secretRoute := r.Group("/secret")
	secretRoute.Use(logmodule.Ginrus("Secret"))
	secretRoute.Use(s.apikeyAuthentication(viper.GetString("server.apikey.admin")))
	{
		secretRoute.POST("/ack-archive-uploaded", s.adminAckArchiveUploaded)
		secretRoute.POST("/submit-archives", s.adminSubmitArchives)
		secretRoute.POST("/parse-archives", s.adminForceParseArchive)
		secretRoute.POST("/generate-hash-content", s.adminGenerateHashContent)
		secretRoute.POST("/delete-accounts", s.adminAccountDelete)
	}

	metricRoute := r.Group("/metrics")
	metricRoute.Use(logmodule.Ginrus("Metric"))
	metricRoute.Use(cors.New(cors.Config{
		AllowMethods:     []string{"GET"},
		AllowHeaders:     []string{"Origin"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
		AllowAllOrigins:  true,
		MaxAge:           12 * time.Hour,
	}))
	metricRoute.Use(s.apikeyAuthentication(viper.GetString("server.apikey.metric")))
	{
		metricRoute.GET("/total-users", s.metricAccountCreation)
	}

	r.GET("/healthz", s.healthz)

	return r
}

func loadCountryContinentMap() (map[string]string, error) {
	var countryContinentMap map[string]string
	data, _ := ioutil.ReadFile(viper.GetString("server.countryContinentMap"))
	err := json.Unmarshal(data, &countryContinentMap)
	return countryContinentMap, err
}

type fbIncomePeriod struct {
	StartedAt     int64   `json:"started_at"`
	EndedAt       int64   `json:"ended_at"`
	QuarterAmount float64 `json:"amount"`
}

type areaFBIncomeMap struct {
	WorldWide   []fbIncomePeriod `json:"world_wide"`
	USCanada    []fbIncomePeriod `json:"us_canada"`
	Europe      []fbIncomePeriod `json:"europe"`
	AsiaPacific []fbIncomePeriod `json:"asia_pacific"`
	Rest        []fbIncomePeriod `json:"rest"`
}

func loadFBIncomeMap() (*areaFBIncomeMap, error) {
	var fbIncomeMap areaFBIncomeMap
	data, _ := ioutil.ReadFile(viper.GetString("server.areaFBIncomeMap"))
	err := json.Unmarshal(data, &fbIncomeMap)
	return &fbIncomeMap, err
}

// Shutdown to shutdown the server
func (s *Server) Shutdown(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

// shouldInterupt sends error message and determine if it should interupt the current flow
func shouldInterupt(err error, c *gin.Context) bool {
	if err == nil {
		return false
	}

	c.Error(err)
	abortWithEncoding(c, http.StatusInternalServerError, errorInternalServer)

	return true
}

func (s *Server) healthz(c *gin.Context) {
	// Ping db
	err := s.store.Ping(c)
	if shouldInterupt(err, c) {
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "OK",
		"version": viper.GetString("server.version"),
	})
}

func (s *Server) information(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"information": map[string]interface{}{
			"server": map[string]interface{}{
				"version":                viper.GetString("server.version"),
				"enc_pub_key":            hex.EncodeToString(s.bitmarkAccount.EncrKey.PublicKeyBytes()),
				"bitmark_account_number": s.bitmarkAccount.AccountNumber(),
			},
			"android":        viper.GetStringMap("clients.android"),
			"ios":            viper.GetStringMap("clients.ios"),
			"system_version": "Spring 0.1",
			"docs":           viper.GetStringMap("docs"),
		},
	})
}

func responseWithEncoding(c *gin.Context, code int, obj proto.Message) {
	acceptEncoding := c.GetHeader("Accept-Encoding")

	switch acceptEncoding {
	case "application/x-protobuf":
		c.ProtoBuf(code, obj)
	default:
		c.JSON(code, obj)
	}
}

func abortWithEncoding(c *gin.Context, code int, obj proto.Message) {
	responseWithEncoding(c, code, obj)
	c.Abort()
}
