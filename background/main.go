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
	"github.com/prometheus/client_golang/prometheus/promhttp"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	prefixed "github.com/x-cray/logrus-prefixed-formatter"
	"golang.org/x/sync/errgroup"

	"github.com/RichardKnop/machinery/v1"
	backendsiface "github.com/RichardKnop/machinery/v1/backends/iface"
	brokersiface "github.com/RichardKnop/machinery/v1/brokers/iface"
	machinerycnf "github.com/RichardKnop/machinery/v1/config"
	machinerylog "github.com/RichardKnop/machinery/v1/log"
	"github.com/RichardKnop/machinery/v1/tasks"
)

var (
	server  *machinery.Server
	broker  brokersiface.Broker
	backend backendsiface.Backend

	g errgroup.Group
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
	server.RegisterTask(jobUploadArchive, b.submitArchive)
	server.RegisterTask(jobPeriodicArchiveCheck, b.checkArchive)
	server.RegisterTask(jobAnalyzePosts, b.extractPost)
	server.RegisterTask(jobAnalyzeReactions, b.extractReaction)
	server.RegisterTask(jobAnalyzeSentiments, b.extractSentiment)
	server.RegisterTask(jobNotificationFinish, b.notifyAnalyzingDone)
	server.RegisterTask(jobExtractTimeMetadata, b.extractTimeMetadata)
	server.RegisterTask(jobGenerateHashContent, b.generateHashContent)
	server.RegisterTask(jobDeleteUserData, b.deleteUserData)

	workerName, err := os.Hostname()
	if err != nil {
		log.Fatal(err)
	}
	worker := server.NewWorker(workerName, viper.GetInt("worker.concurrency"))
	if err := worker.Launch(); err != nil {
		log.Fatal(err)
	}

	// Map the name of jobs to handler functions
	worker.SetPreTaskHandler(b.jobStartCollectiveMetric)
	worker.SetPostTaskHandler(b.jobEndCollectiveMetric)
	worker.SetErrorHandler(b.jobErrorHandler)

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

func (b *BackgroundContext) jobEndCollectiveMetric(signature *tasks.Signature) {
	totalProcessedCounterVec.WithLabelValues(signature.Name).Inc()
	currentProcessingGaugeVec.WithLabelValues(signature.Name).Dec()
}

func (b *BackgroundContext) jobErrorHandler(err error) {
	log.Error(err)
	sentry.CaptureException(err)
}
