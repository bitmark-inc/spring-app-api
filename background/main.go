package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/RichardKnop/machinery/v1"
	backendsiface "github.com/RichardKnop/machinery/v1/backends/iface"
	brokersiface "github.com/RichardKnop/machinery/v1/brokers/iface"
	machinerycnf "github.com/RichardKnop/machinery/v1/config"
	machinerylog "github.com/RichardKnop/machinery/v1/log"
	"github.com/RichardKnop/machinery/v1/tasks"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/getsentry/sentry-go"
	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/postgres"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	prefixed "github.com/x-cray/logrus-prefixed-formatter"
	"golang.org/x/sync/errgroup"

	bitmarksdk "github.com/bitmark-inc/bitmark-sdk-go"
	"github.com/bitmark-inc/spring-app-api/external/fbarchive"
	"github.com/bitmark-inc/spring-app-api/external/geoservice"
	"github.com/bitmark-inc/spring-app-api/external/onesignal"
	"github.com/bitmark-inc/spring-app-api/logmodule"
	"github.com/bitmark-inc/spring-app-api/schema/spring"
	"github.com/bitmark-inc/spring-app-api/store"
	"github.com/bitmark-inc/spring-app-api/store/dynamodb"
	"github.com/bitmark-inc/spring-app-api/store/postgres"
)

var (
	server  *machinery.Server
	broker  brokersiface.Broker
	backend backendsiface.Backend

	g errgroup.Group
)

const (
	jobDownloadArchive      = "download_archive"
	jobParseArchive         = "parse_archive"
	jobExtract              = "extract_zip"
	jobUploadArchive        = "upload_archive"
	jobPeriodicArchiveCheck = "periodic_archive_check"
	jobAnalyzePosts         = "analyze_posts"
	jobAnalyzeReactions     = "analyze_reactions"
	jobAnalyzeSentiments    = "analyze_sentiments"
	jobNotificationFinish   = "notification_finish_parsing"
	jobExtractTimeMetadata  = "extract_time_metadata"
	jobGenerateHashContent  = "generate_hash_content"
	jobPrepareDataExport    = "prepare_data_export"
	jobDeleteUserData       = "delete_user_data"
)

type BackgroundContext struct {
	// Stores
	store       store.Store
	fbDataStore store.FBDataStore

	ormDB *gorm.DB

	// AWS Config
	awsConf *aws.Config

	// http client
	httpClient *http.Client

	// External services
	oneSignalClient  *onesignal.OneSignalClient
	bitSocialClient  *fbarchive.Client
	geoServiceClient *geoservice.Client
}

func initLog() {
	// Log
	logLevel, err := log.ParseLevel(viper.GetString("log.level"))
	if err != nil {
		log.SetLevel(log.DebugLevel)
	} else {
		log.SetLevel(logLevel)
	}

	log.SetOutput(os.Stdout)

	log.SetFormatter(&prefixed.TextFormatter{
		ForceFormatting: true,
		FullTimestamp:   true,
	})
}

func loadConfig(file string) {
	// Config from file
	viper.SetConfigType("yaml")
	if file != "" {
		viper.SetConfigFile(file)
	}

	viper.AddConfigPath("/.config/")
	viper.AddConfigPath(".")
	err := viper.ReadInConfig()
	if err != nil {
		fmt.Println("No config file. Read config from env.")
		viper.AllowEmptyEnv(false)
	}

	// Config from env if possible
	viper.AutomaticEnv()
	viper.SetEnvPrefix("fbm")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
}

// CodeError defines a interface of error tha includes an error code in string
type CodeError interface {
	Code() string
	Error() string
}

// ArchiveJobError is an error for machinery that helps determine further error handling
// in the callback function. ArchiveJobError includes an archive ID so that we can set the
// error to corresponded its archive in DB
type ArchiveJobError struct {
	ID        int64
	CodeError CodeError
	JobError  error
}

func NewArchiveJobError(id int64, codeError CodeError) func(err error) error {
	a := &ArchiveJobError{
		ID:        id,
		CodeError: codeError,
	}

	return func(err error) error {
		a.JobError = err
		return a
	}
}

func (a *ArchiveJobError) Error() string {
	return fmt.Sprintf("%s(%s) archive_id: %d", a.CodeError.Code(), a.JobError.Error(), a.ID)
}

func main() {
	var configFile string

	flag.StringVar(&configFile, "c", "./config.yaml", "[optional] path of configuration file")
	flag.Parse()

	loadConfig(configFile)

	initLog()

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	httpClient := &http.Client{Transport: tr}

	// Sentry
	if err := sentry.Init(sentry.ClientOptions{
		Dsn:              viper.GetString("sentry.dsn"),
		AttachStacktrace: true,
		Environment:      viper.GetString("sentry.environment"),
	}); err != nil {
		log.Error(err)
	}

	awsConf := &aws.Config{
		Region:     aws.String(viper.GetString("aws.region")),
		Logger:     &logmodule.AWSLog{},
		HTTPClient: httpClient,
	}

	dynamodbStore, err := dynamodb.NewDynamoDBStore(awsConf, viper.GetString("aws.dynamodb.table"))
	if err != nil {
		log.Panic(err)
	}

	oneSignalClient := onesignal.NewClient(httpClient)
	bitSocialClient := fbarchive.NewClient(httpClient)
	geoServiceClient := geoservice.NewClient(httpClient)

	// Init Bitmark SDK
	bitmarksdk.Init(&bitmarksdk.Config{
		Network:    bitmarksdk.Network(viper.GetString("bitmarksdk.network")),
		APIToken:   viper.GetString("bitmarksdk.token"),
		HTTPClient: httpClient,
	})

	// Init db
	pgstore, err := postgres.NewPGStore(context.Background())
	if err != nil {
		log.Panic(err)
	}

	ormDB, err := gorm.Open("postgres", viper.GetString("orm.conn"))
	if err != nil {
		log.Panic(err)
	}

	b := &BackgroundContext{
		fbDataStore:      dynamodbStore,
		store:            pgstore,
		ormDB:            ormDB,
		awsConf:          awsConf,
		httpClient:       httpClient,
		oneSignalClient:  oneSignalClient,
		bitSocialClient:  bitSocialClient,
		geoServiceClient: geoServiceClient,
	}

	// Register metrics
	if err := registerMetrics(); err != nil {
		log.Fatal(err)
	}
	maxProcessingGaugeVec.WithLabelValues().Set(float64(viper.GetUint("worker.concurrency")))

	var cnf = &machinerycnf.Config{
		Broker:        viper.GetString("redis.conn"),
		DefaultQueue:  "fbm_background",
		NoUnixSignals: false,
		ResultBackend: viper.GetString("redis.conn"),
	}
	s, err := machinery.NewServer(cnf)
	if err != nil {
		log.Panic(err)
	}
	server = s
	machinerylog.Set(&logmodule.MachineryLogger{Prefix: "machinery"})

	server.RegisterTask(jobDownloadArchive, b.downloadArchive)
	server.RegisterTask(jobParseArchive, b.parseArchive)
	server.RegisterTask(jobAnalyzePosts, b.extractPost)
	server.RegisterTask(jobAnalyzeReactions, b.extractReaction)
	server.RegisterTask(jobAnalyzeSentiments, b.extractSentiment)
	server.RegisterTask(jobNotificationFinish, b.notifyAnalyzingDone)
	server.RegisterTask(jobExtractTimeMetadata, b.extractTimeMetadata)
	server.RegisterTask(jobGenerateHashContent, b.generateHashContent)
	server.RegisterTask(jobPrepareDataExport, b.prepareUserExportData)
	server.RegisterTask(jobDeleteUserData, b.deleteUserData)

	workerName, err := os.Hostname()
	if err != nil {
		log.Fatal(err)
	}
	worker := server.NewWorker(workerName, viper.GetInt("worker.concurrency"))
	// Map the name of jobs to handler functions
	worker.SetPreTaskHandler(b.jobStartCollectiveMetric)
	worker.SetPostTaskHandler(b.jobProcessedHandler)
	worker.SetErrorHandler(b.jobErrorHandler)

	if err := worker.Launch(); err != nil {
		log.Fatal(err)
	}

	// Start processing jobs

	// Wait for a signal to quit:

	// Create a new mux server
	serverMux := http.NewServeMux()
	serverMux.Handle("/metrics", promhttp.Handler())
	httpServer := http.Server{
		Addr:    viper.GetString("worker_serveraddr"),
		Handler: serverMux,
	}

	c := make(chan os.Signal, 2)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		log.Info("Preparing to shutdown")
		ctx, cancelFunc := context.WithTimeout(context.Background(), time.Second*10)
		defer cancelFunc()

		log.Info("close idle connections")
		httpClient.CloseIdleConnections()

		log.Info("close postgres connection")
		pgstore.Close(ctx)

		log.Info("shutdown metric server")
		httpServer.Shutdown(ctx)

		log.Info("quit woker")
		worker.Quit()

		log.Info("flush sentry")
		sentry.Flush(time.Second * 5)

		os.Exit(1)
	}()

	g.Go(func() error {
		if err := worker.Launch(); err != nil && err != errors.New("Worker quit gracefully") {
			return err
		}

		return nil
	})
	g.Go(func() error {
		return httpServer.ListenAndServe()
	})

	log.Panic(g.Wait())
}

// For metric
func (b *BackgroundContext) jobStartCollectiveMetric(signature *tasks.Signature) {
	currentProcessingGaugeVec.WithLabelValues(signature.Name).Inc()
}

func (b *BackgroundContext) jobProcessedHandler(signature *tasks.Signature) {
	logEntry := log.WithField("uuid", signature.UUID)
	logEntry.Debug("job done")

	switch signature.Name {
	case jobPrepareDataExport:
		// Update spring archive job state when it is run
		state, err := server.GetBackend().GetState(signature.UUID)
		if err != nil {
			logEntry.WithError(err).Error("fail to get task state")
			sentry.CaptureException(err)
		}

		logEntry.WithField("state", state.State).Info("update archive task state")
		if err := b.ormDB.Model(&spring.ArchiveORM{}).
			Where("job_id = ?", signature.UUID).
			Update("status", state.State).
			Error; err != nil {
			logEntry.WithError(err).Error("fail to update archive state")
			sentry.CaptureException(err)
		}
	default:
		totalProcessedCounterVec.WithLabelValues(signature.Name).Inc()
		currentProcessingGaugeVec.WithLabelValues(signature.Name).Dec()
	}
}

func (b *BackgroundContext) jobErrorHandler(err error) {
	switch err := err.(type) {
	case *ArchiveJobError:
		archiveID := err.ID
		if err := b.store.InvalidFBArchive(context.Background(), &store.FBArchiveQueryParam{
			ID:    &archiveID,
			Error: &err.CodeError,
		}); err != nil {
			log.WithField("prefix", "job_error").WithField("archvie_id", archiveID).WithField("action", "InvalidFBArchive").Warn(err.Error())
		}
	}

	log.Error(err)
	sentry.CaptureException(err)
}
