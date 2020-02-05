package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	bitmarksdk "github.com/bitmark-inc/bitmark-sdk-go"
	"github.com/bitmark-inc/spring-app-api/external/fbarchive"
	"github.com/bitmark-inc/spring-app-api/external/geoservice"
	"github.com/bitmark-inc/spring-app-api/external/onesignal"
	"github.com/bitmark-inc/spring-app-api/logmodule"
	"github.com/bitmark-inc/spring-app-api/store"
	"github.com/bitmark-inc/spring-app-api/store/dynamodb"
	"github.com/bitmark-inc/spring-app-api/store/postgres"
	"github.com/getsentry/sentry-go"
	"github.com/gocraft/work"
	"github.com/gomodule/redigo/redis"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	prefixed "github.com/x-cray/logrus-prefixed-formatter"
	"golang.org/x/sync/errgroup"
)

var (
	enqueuer *work.Enqueuer
	pool     *work.WorkerPool
	g        errgroup.Group
)

const (
	jobDownloadArchive      = "download_archive"
	jobExtract              = "extract_zip"
	jobUploadArchive        = "upload_archive"
	jobPeriodicArchiveCheck = "periodic_archive_check"
	jobAnalyzePosts         = "analyze_posts"
	jobAnalyzeReactions     = "analyze_reactions"
	jobAnalyzeSentiments    = "analyze_sentiments"
	jobNotificationFinish   = "notification_finish_parsing"
	jobExtractTimeMetadata  = "extract_time_metadata"
	jobGenerateHashContent  = "generate_hash_content"
	jobDeleteUserData       = "delete_user_data"
)

type BackgroundContext struct {
	// Stores
	store       store.Store
	fbDataStore store.FBDataStore

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
		Environment:      viper.GetString("bitmarksdk.network"),
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

	// Login to bitsocial server
	go func(bitSocialClient *fbarchive.Client) {
		for {
			ctx := context.Background()
			err := bitSocialClient.Login(ctx, viper.GetString("fbarchive.username"), viper.GetString("fbarchive.password"))
			if err == nil {
				log.Info("Success logged in to bitsocial server")
				return
			}
			log.WithError(err).Error("Cannot connect to bitsocial server")
			time.Sleep(1 * time.Minute)
		}
	}(bitSocialClient)

	// Init db
	pgstore, err := postgres.NewPGStore(context.Background())
	if err != nil {
		log.Panic(err)
	}

	b := &BackgroundContext{
		fbDataStore:      dynamodbStore,
		store:            pgstore,
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

	redisPool := &redis.Pool{
		MaxActive: 5,
		MaxIdle:   5,
		Wait:      true,
		Dial: func() (redis.Conn, error) {
			return redis.Dial("tcp", viper.GetString("redis.conn"), redis.DialPassword(viper.GetString("redis.password")))
		},
	}

	pool = work.NewWorkerPool(*b, viper.GetUint("worker.concurrency"), "fbm", redisPool)
	enqueuer = work.NewEnqueuer("fbm", redisPool)

	// Add middleware for logging for each job
	pool.Middleware(b.log)
	pool.Middleware(b.jobStartCollectiveMetric)

	// Map the name of jobs to handler functions
	pool.JobWithOptions(jobDownloadArchive,
		work.JobOptions{Priority: 10, MaxFails: 1},
		b.downloadArchive)
	pool.JobWithOptions(jobUploadArchive,
		work.JobOptions{Priority: 10, MaxFails: 1},
		b.submitArchive)
	pool.JobWithOptions(jobExtract,
		work.JobOptions{Priority: 10, MaxFails: 1},
		b.extractMedia)
	pool.JobWithOptions(jobPeriodicArchiveCheck,
		work.JobOptions{Priority: 5, MaxFails: 1},
		b.checkArchive)
	pool.JobWithOptions(jobAnalyzePosts,
		work.JobOptions{Priority: 10, MaxFails: 1},
		b.extractPost)
	pool.JobWithOptions(jobAnalyzeReactions,
		work.JobOptions{Priority: 10, MaxFails: 1},
		b.extractReaction)
	pool.JobWithOptions(jobAnalyzeSentiments,
		work.JobOptions{Priority: 10, MaxFails: 1},
		b.extractSentiment)
	pool.JobWithOptions(jobNotificationFinish,
		work.JobOptions{Priority: 10, MaxFails: 1},
		b.notifyAnalyzingDone)
	pool.JobWithOptions(jobExtractTimeMetadata,
		work.JobOptions{Priority: 1, MaxFails: 1},
		b.extractTimeMetadata)
	pool.JobWithOptions(jobGenerateHashContent,
		work.JobOptions{Priority: 10, MaxFails: 1},
		b.generateHashContent)
	pool.JobWithOptions(jobDeleteUserData,
		work.JobOptions{Priority: 10, MaxFails: 1},
		b.deleteUserData)

	signalChan := make(chan os.Signal, 2)
	signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)

	go func(signalChan chan os.Signal) {
		<-signalChan
		log.Info("Preparing to shutdown")

		httpClient.CloseIdleConnections()
		// Stop the pool
		pool.Stop()
		sentry.Flush(time.Second * 5)

		os.Exit(1)
	}(signalChan)

	// Start processing jobs
	pool.Start()

	// Wait for a signal to quit:

	// Create a new mux server
	server := http.NewServeMux()
	server.Handle("/metrics", promhttp.Handler())

	http.ListenAndServe(viper.GetString("worker_serveraddr"), server)
}

func (b *BackgroundContext) log(job *work.Job, next work.NextMiddlewareFunc) error {
	log.WithField("args", job.Args).Info("Starting job: ", job.Name)
	return next()
}

// For metric
func (b *BackgroundContext) jobStartCollectiveMetric(job *work.Job, next work.NextMiddlewareFunc) error {
	currentProcessingGaugeVec.WithLabelValues(job.Name).Inc()
	return next()
}

func jobEndCollectiveMetric(err error, job *work.Job) error {
	totalProcessedCounterVec.WithLabelValues(job.Name).Inc()
	currentProcessingGaugeVec.WithLabelValues(job.Name).Dec()
	if err != nil {
		totalFailedCounterVec.WithLabelValues(job.Name).Inc()
		logEntity := log.WithField("prefix", job.Name+"/"+job.ID)
		logEntity.Error(err)
		sentry.CaptureException(err)
	} else {
		totalSuccessfulCounterVec.WithLabelValues(job.Name).Inc()
	}
	return nil
}
