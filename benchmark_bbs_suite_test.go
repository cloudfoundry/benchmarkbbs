package benchmarkbbs_test

import (
	"crypto/rand"
	"crypto/tls"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"runtime"
	"strings"
	"time"

	"code.cloudfoundry.org/bbs"
	"code.cloudfoundry.org/bbs/db"
	etcddb "code.cloudfoundry.org/bbs/db/etcd"
	"code.cloudfoundry.org/bbs/db/sqldb"
	"code.cloudfoundry.org/bbs/encryption"
	"code.cloudfoundry.org/bbs/format"
	"code.cloudfoundry.org/bbs/guidprovider"
	benchmarkconfig "code.cloudfoundry.org/benchmarkbbs/config"
	"code.cloudfoundry.org/benchmarkbbs/generator"
	"code.cloudfoundry.org/benchmarkbbs/reporter"
	"code.cloudfoundry.org/cfhttp"
	"code.cloudfoundry.org/clock"
	"code.cloudfoundry.org/lager"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/coreos/go-etcd/etcd"
	datadog "github.com/zorkian/go-datadog-api"

	_ "github.com/go-sql-driver/mysql"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

var (
	expectedLRPCount     int
	expectedLRPVariation float64

	expectedActualLRPCounts     map[string]int
	expectedActualLRPVariations map[string]float64

	config benchmarkconfig.BenchmarkBBSConfig

	logger          lager.Logger
	etcdClient      *etcd.Client
	etcdDB          *etcddb.ETCDDB
	sqlDB           *sqldb.SQLDB
	activeDB        db.DB
	bbsClient       bbs.InternalClient
	dataDogClient   *datadog.Client
	dataDogReporter reporter.DataDogReporter
	reporters       []Reporter
)

const (
	DEBUG = "debug"
	INFO  = "info"
	ERROR = "error"
	FATAL = "fatal"
)

func init() {
	runtime.GOMAXPROCS(runtime.NumCPU())
	configFile := flag.String("config", "", "config file")
	flag.Parse()

	var err error
	config, err = benchmarkconfig.NewBenchmarkBBSConfig(*configFile)
	if err != nil {
		panic(err)
	}

	if config.BBSAddress == "" {
		log.Fatal("bbsAddress is required")
	}

	BenchmarkTests(config.NumReps, config.NumTrials, config.LocalRouteEmitters)
}

func TestBenchmarkBbs(t *testing.T) {
	var lagerLogLevel lager.LogLevel
	switch config.LogLevel {
	case DEBUG:
		lagerLogLevel = lager.DEBUG
	case INFO:
		lagerLogLevel = lager.INFO
	case ERROR:
		lagerLogLevel = lager.ERROR
	case FATAL:
		lagerLogLevel = lager.FATAL
	default:
		panic(fmt.Errorf("unknown log level: %s", config.LogLevel))
	}

	var logWriter io.Writer
	if config.LogFilename == "" {
		logWriter = GinkgoWriter
	} else {
		logFile, err := os.Create(config.LogFilename)
		if err != nil {
			panic(fmt.Errorf("Error opening file '%s': %s", config.LogFilename, err.Error()))
		}
		defer logFile.Close()

		logWriter = logFile
	}

	logger = lager.NewLogger("bbs-benchmarks-test")
	logger.RegisterSink(lager.NewWriterSink(logWriter, lagerLogLevel))

	reporters = []Reporter{}

	if config.DataDogAPIKey != "" && config.DataDogAppKey != "" {
		dataDogClient = datadog.NewClient(config.DataDogAPIKey, config.DataDogAppKey)
		dataDogReporter = reporter.NewDataDogReporter(logger, config.MetricPrefix, dataDogClient)
		reporters = append(reporters, &dataDogReporter)
	}

	if config.AwsAccessKeyID != "" && config.AwsSecretAccessKey != "" && config.AwsBucketName != "" {
		creds := credentials.NewStaticCredentials(config.AwsAccessKeyID, config.AwsSecretAccessKey, "")
		sess, err := session.NewSession(aws.NewConfig())
		if err != nil {
			panic(fmt.Errorf("Error connecting to S3: %s\n", err))
		}

		s3Client := s3.New(sess, &aws.Config{
			Region:      &config.AwsRegion,
			Credentials: creds,
		})
		uploader := s3manager.NewUploaderWithClient(s3Client)
		reporter := reporter.NewS3Reporter(logger, config.AwsBucketName, uploader)
		reporters = append(reporters, &reporter)
	}

	RegisterFailHandler(Fail)
	RunSpecsWithDefaultAndCustomReporters(t, "Benchmark BBS Suite", reporters)
}

type expectedLRPCounts struct {
	DesiredLRPCount     int
	DesiredLRPVariation float64

	ActualLRPCounts     map[string]int
	ActualLRPVariations map[string]float64
}

func initializeActiveDB() *sql.DB {
	if activeDB != nil {
		return nil
	}

	if config.DatabaseConnectionString == "" {
		etcdOptions, err := config.ETCDConfig.Validate()
		Expect(err).NotTo(HaveOccurred())

		etcdClient = initializeEtcdClient(logger, etcdOptions)
		etcdDB = initializeETCDDB(logger, etcdClient)

		activeDB = etcdDB
		return nil
	}

	if config.DatabaseDriver == "postgres" && !strings.Contains(config.DatabaseConnectionString, "sslmode") {
		config.DatabaseConnectionString = fmt.Sprintf("%s?sslmode=disable", config.DatabaseConnectionString)
	}

	sqlConn, err := sql.Open(config.DatabaseDriver, config.DatabaseConnectionString)
	if err != nil {
		logger.Fatal("failed-to-open-sql", err)
	}
	sqlConn.SetMaxOpenConns(1)
	sqlConn.SetMaxIdleConns(1)

	err = sqlConn.Ping()
	Expect(err).NotTo(HaveOccurred())

	sqlDB = initializeSQLDB(logger, sqlConn)
	activeDB = sqlDB
	return sqlConn
}

var _ = BeforeSuite(func() {
	bbsClient = initializeBBSClient(logger, time.Duration(config.BBSClientHTTPTimeout))

	if conn := initializeActiveDB(); conn != nil {
		cleanupSQLDB(conn)
	} else {
		cleanupETCD()
	}

	_, err := bbsClient.Domains(logger)
	Expect(err).NotTo(HaveOccurred())

	expectedActualLRPVariations = make(map[string]float64)

	if config.DesiredLRPs > 0 {
		desiredLRPGenerator := generator.NewDesiredLRPGenerator(
			config.ErrorTolerance,
			config.MetricPrefix,
			config.NumPopulateWorkers,
			bbsClient,
			dataDogClient,
		)
		expectedLRPCount, expectedActualLRPCounts, err = desiredLRPGenerator.Generate(logger, config.NumReps, config.DesiredLRPs)
		Expect(err).NotTo(HaveOccurred())
		expectedLRPVariation = float64(expectedLRPCount) * config.ErrorTolerance

		for k, v := range expectedActualLRPCounts {
			expectedActualLRPVariations[k] = float64(v) * config.ErrorTolerance
		}
	}
})

var _ = AfterSuite(func() {
	if config.DatabaseConnectionString == "" {
		cleanupETCD()
	} else {
		sqlConn, err := sql.Open(config.DatabaseDriver, config.DatabaseConnectionString)
		if err != nil {
			logger.Fatal("failed-to-open-sql", err)
		}
		sqlConn.SetMaxOpenConns(1)
		sqlConn.SetMaxIdleConns(1)

		err = sqlConn.Ping()
		Expect(err).NotTo(HaveOccurred())
		cleanupSQLDB(sqlConn)
	}
})

func initializeEtcdClient(logger lager.Logger, etcdOptions *etcddb.ETCDOptions) *etcd.Client {
	var etcdClient *etcd.Client
	var tr *http.Transport

	if etcdOptions.IsSSL {
		if etcdOptions.CertFile == "" || etcdOptions.KeyFile == "" {
			logger.Fatal("failed-to-construct-etcd-tls-client", errors.New("Require both cert and key path"))
		}

		var err error
		etcdClient, err = etcd.NewTLSClient(etcdOptions.ClusterUrls, etcdOptions.CertFile, etcdOptions.KeyFile, etcdOptions.CAFile)
		if err != nil {
			logger.Fatal("failed-to-construct-etcd-tls-client", err)
		}

		tlsCert, err := tls.LoadX509KeyPair(etcdOptions.CertFile, etcdOptions.KeyFile)
		if err != nil {
			logger.Fatal("failed-to-construct-etcd-tls-client", err)
		}

		tlsConfig := &tls.Config{
			Certificates:       []tls.Certificate{tlsCert},
			InsecureSkipVerify: true,
			ClientSessionCache: tls.NewLRUClientSessionCache(etcdOptions.ClientSessionCacheSize),
		}
		tr = &http.Transport{
			TLSClientConfig:     tlsConfig,
			Dial:                etcdClient.DefaultDial,
			MaxIdleConnsPerHost: etcdOptions.MaxIdleConnsPerHost,
		}
		etcdClient.SetTransport(tr)
	} else {
		etcdClient = etcd.NewClient(etcdOptions.ClusterUrls)
	}
	etcdClient.SetConsistency(etcd.STRONG_CONSISTENCY)

	return etcdClient
}

func initializeETCDDB(logger lager.Logger, etcdClient *etcd.Client) *etcddb.ETCDDB {
	key, keys, err := config.EncryptionConfig.Parse()
	if err != nil {
		logger.Fatal("cannot-setup-encryption", err)
	}
	keyManager, err := encryption.NewKeyManager(key, keys)
	if err != nil {
		logger.Fatal("cannot-setup-encryption", err)
	}
	cryptor := encryption.NewCryptor(keyManager, rand.Reader)

	return etcddb.NewETCD(format.ENCRYPTED_PROTO, 1000, 1000, 1*time.Minute, cryptor, etcddb.NewStoreClient(etcdClient), clock.NewClock())
}

func initializeSQLDB(logger lager.Logger, sqlConn *sql.DB) *sqldb.SQLDB {
	key, keys, err := config.EncryptionConfig.Parse()
	if err != nil {
		logger.Fatal("cannot-setup-encryption", err)
	}
	keyManager, err := encryption.NewKeyManager(key, keys)
	if err != nil {
		logger.Fatal("cannot-setup-encryption", err)
	}
	cryptor := encryption.NewCryptor(keyManager, rand.Reader)

	return sqldb.NewSQLDB(
		sqlConn,
		1000,
		1000,
		format.ENCODED_PROTO,
		cryptor,
		guidprovider.DefaultGuidProvider,
		clock.NewClock(),
		config.DatabaseDriver,
	)
}

func initializeBBSClient(logger lager.Logger, bbsClientHTTPTimeout time.Duration) bbs.InternalClient {
	bbsURL, err := url.Parse(config.BBSAddress)
	if err != nil {
		logger.Fatal("Invalid BBS URL", err)
	}

	if bbsURL.Scheme != "https" {
		return bbs.NewClient(config.BBSAddress)
	}

	cfhttp.Initialize(bbsClientHTTPTimeout)
	bbsClient, err := bbs.NewSecureClient(
		config.BBSAddress,
		config.BBSCACert,
		config.BBSClientCert,
		config.BBSClientKey,
		1,
		25000,
	)
	if err != nil {
		logger.Fatal("Failed to configure secure BBS client", err)
	}
	return bbsClient
}

func purge(key string) {
	_, err := etcdClient.Delete(key, true)
	if err != nil {
		matches, matchErr := regexp.Match(".*Key not found.*", []byte(err.Error()))
		if matchErr != nil {
			Fail(matchErr.Error())
		}
		if !matches {
			Fail(err.Error())
		}
	}
}

func cleanupETCD() {
	purge("/v1/desired_lrp")
	purge("/v1/actual")
}

func cleanupSQLDB(conn *sql.DB) {
	_, err := conn.Exec("TRUNCATE actual_lrps")
	Expect(err).NotTo(HaveOccurred())
	_, err = conn.Exec("TRUNCATE desired_lrps")
	Expect(err).NotTo(HaveOccurred())
}
