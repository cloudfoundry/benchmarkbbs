package benchmarkbbs_test

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"code.cloudfoundry.org/bbs"
	"code.cloudfoundry.org/bbs/db"
	"code.cloudfoundry.org/bbs/db/sqldb"
	"code.cloudfoundry.org/bbs/db/sqldb/helpers"
	"code.cloudfoundry.org/bbs/db/sqldb/helpers/monitor"
	"code.cloudfoundry.org/bbs/encryption"
	"code.cloudfoundry.org/bbs/guidprovider"
	"code.cloudfoundry.org/bbs/models"
	benchmarkconfig "code.cloudfoundry.org/benchmarkbbs/config"
	"code.cloudfoundry.org/benchmarkbbs/generator"
	"code.cloudfoundry.org/benchmarkbbs/reporter"
	"code.cloudfoundry.org/clock"
	fakes "code.cloudfoundry.org/diego-logging-client/testhelpers"
	"code.cloudfoundry.org/executor"
	"code.cloudfoundry.org/lager"
	"code.cloudfoundry.org/locket"
	locketmodels "code.cloudfoundry.org/locket/models"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	_ "github.com/go-sql-driver/mysql"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/zorkian/go-datadog-api"
)

var (
	expectedLRPCount int

	expectedActualLRPCounts map[string]int
	config                  benchmarkconfig.BenchmarkBBSConfig

	logger          lager.Logger
	locketClient    locketmodels.LocketClient
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
		logFile, err := os.OpenFile(config.LogFilename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			panic("Error opening file, err: " + err.Error())
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
		s3Client := s3.New(&aws.Config{
			Region:      &config.AwsRegion,
			Credentials: creds,
		})
		uploader := s3manager.NewUploader(&s3manager.UploadOptions{S3: s3Client})
		reporter := reporter.NewS3Reporter(logger, config.AwsBucketName, uploader)
		reporters = append(reporters, &reporter)
	}

	RegisterFailHandler(Fail)
	RunSpecsWithDefaultAndCustomReporters(t, "Benchmark BBS Suite", reporters)
}

func initializeActiveDB() *sql.DB {
	if activeDB != nil {
		return nil
	}

	if config.DatabaseConnectionString == "" {
		logger.Fatal("no-sql-configuration", errors.New("no-sql-configuration"))
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

	wrappedDB := helpers.NewMonitoredDB(sqlConn, monitor.New())
	sqlDB = initializeSQLDB(logger, wrappedDB)
	activeDB = sqlDB
	return sqlConn
}

var _ = BeforeSuite(func() {
	bbsClient = initializeBBSClient(logger, time.Duration(config.BBSClientHTTPTimeout))

	conn := initializeActiveDB()
	initializeLocketClient()

	resp, err := locketClient.FetchAll(context.Background(), &locketmodels.FetchAllRequest{TypeCode: locketmodels.PRESENCE})
	Expect(err).NotTo(HaveOccurred())

	cells := make(map[string]struct{})
	for i := 0; i < config.NumReps; i++ {
		cells[fmt.Sprintf("cell-%d", i)] = struct{}{}
	}

	if config.ReseedDatabase || config.NumReps != len(resp.Resources) {
		var err error

		cleanupSQLDB(conn)
		resetLocketDB(locketClient, cells, resp)

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
		}
	} else {
		resetUnclaimedActualLRPs(conn, cells)

		query := `
			SELECT
				COUNT(*)
			FROM desired_lrps
		`
		res := conn.QueryRow(query)
		err := res.Scan(&expectedLRPCount)
		Expect(err).NotTo(HaveOccurred())

		expectedActualLRPCounts = make(map[string]int)
		query = `
			SELECT
				cell_id, COUNT(*)
			FROM actual_lrps
			GROUP BY cell_id
		`
		rows, err := conn.Query(query)
		Expect(err).NotTo(HaveOccurred())

		defer rows.Close()
		for rows.Next() {
			var cellId string
			var count int
			err = rows.Scan(&cellId, &count)
			Expect(err).NotTo(HaveOccurred())

			expectedActualLRPCounts[cellId] = count
		}
	}

	if float64(expectedLRPCount) < float64(config.DesiredLRPs)*config.ErrorTolerance {
		Fail(fmt.Sprintf("Error rate of %.3f for actuals exceeds tolerance of %.3f", float64(expectedLRPCount)/float64(config.DesiredLRPs), config.ErrorTolerance))
	}
})

func initializeSQLDB(logger lager.Logger, sqlConn helpers.QueryableDB) *sqldb.SQLDB {
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
		cryptor,
		guidprovider.DefaultGuidProvider,
		clock.NewClock(),
		config.DatabaseDriver,
		&fakes.FakeIngressClient{},
	)
}

func initializeLocketClient() {
	var err error
	locketClient, err = locket.NewClient(logger, config.ClientLocketConfig)
	Expect(err).NotTo(HaveOccurred())
}

func initializeBBSClient(logger lager.Logger, bbsClientHTTPTimeout time.Duration) bbs.InternalClient {
	bbsClient, err := bbs.NewClientWithConfig(bbs.ClientConfig{
		URL:                    config.BBSAddress,
		IsTLS:                  true,
		CAFile:                 config.BBSCACert,
		CertFile:               config.BBSClientCert,
		KeyFile:                config.BBSClientKey,
		ClientSessionCacheSize: 1,
		MaxIdleConnsPerHost:    25000,
		RequestTimeout:         bbsClientHTTPTimeout,
	})
	if err != nil {
		logger.Fatal("Failed to configure secure BBS client", err)
	}
	return bbsClient
}

func cleanupSQLDB(conn *sql.DB) {
	_, err := conn.Exec("TRUNCATE actual_lrps")
	Expect(err).NotTo(HaveOccurred())
	_, err = conn.Exec("TRUNCATE desired_lrps")
	Expect(err).NotTo(HaveOccurred())
}

func resetLocketDB(locketClient locketmodels.LocketClient, cells map[string]struct{}, resp *locketmodels.FetchAllResponse) {
	for _, resource := range resp.Resources {
		if _, ok := cells[resource.Owner]; !ok {
			_, err := locketClient.Release(context.Background(), &locketmodels.ReleaseRequest{Resource: resource})
			Expect(err).NotTo(HaveOccurred())
		}
	}

	for cellId, _ := range cells {
		_, err := locketClient.Lock(context.Background(), &locketmodels.LockRequest{Resource: lockResource(cellId), TtlInSeconds: longTermTtl})
		Expect(err).NotTo(HaveOccurred())
	}
}

func lockResource(cellID string) *locketmodels.Resource {
	resources := executor.ExecutorResources{
		MemoryMB:   1000,
		DiskMB:     2000,
		Containers: 300,
	}
	cellCapacity := models.NewCellCapacity(int32(resources.MemoryMB), int32(resources.DiskMB), int32(resources.Containers))
	cellPresence := models.NewCellPresence(cellID, cellID+".address", cellID+".url",
		"z1", cellCapacity, []string{"providers"},
		[]string{"cflinuxfs9"}, []string{}, []string{})

	payload, err := json.Marshal(cellPresence)
	Expect(err).NotTo(HaveOccurred())

	return &locketmodels.Resource{
		Key:   cellID,
		Owner: cellID,
		Value: string(payload),
		Type:  locketmodels.PresenceType,
	}
}

func resetUnclaimedActualLRPs(conn *sql.DB, cells map[string]struct{}) {
	existingCellIds := make(map[string]struct{})

	query := `
			SELECT
				cell_id
			FROM actual_lrps
			GROUP BY cell_id
		`
	rows, err := conn.Query(query)
	Expect(err).NotTo(HaveOccurred())

	defer rows.Close()
	for rows.Next() {
		var cellId string
		err = rows.Scan(&cellId)
		Expect(err).NotTo(HaveOccurred())

		existingCellIds[cellId] = struct{}{}
	}

	missingCells := findMissingCells(existingCellIds, cells)

	query = `
				SELECT
					process_guid, domain
				FROM actual_lrps WHERE cell_id = ''
			`

	type guidAndDomain struct {
		ProcessGuid string
		Domain      string
	}
	var lrps []guidAndDomain

	rows, err = conn.Query(query)
	Expect(err).NotTo(HaveOccurred())

	defer rows.Close()
	for rows.Next() {
		var guid, domain string
		err = rows.Scan(&guid, &domain)
		Expect(err).NotTo(HaveOccurred())
		lrps = append(lrps, guidAndDomain{ProcessGuid: guid, Domain: domain})
	}

	for i, lrp := range lrps {
		cellID := missingCells[i%len(missingCells)]
		actualLRPInstanceKey := &models.ActualLRPInstanceKey{InstanceGuid: lrp.ProcessGuid + "-i", CellId: cellID}
		netInfo := models.NewActualLRPNetInfo("1.2.3.4", "2.2.2.2", models.ActualLRPNetInfo_PreferredAddressHost, models.NewPortMapping(61999, 8080))
		err := bbsClient.StartActualLRP(logger, &models.ActualLRPKey{Domain: lrp.Domain, ProcessGuid: lrp.ProcessGuid, Index: 0}, actualLRPInstanceKey, &netInfo)
		Expect(err).NotTo(HaveOccurred())
	}
}

func findMissingCells(existing map[string]struct{}, expected map[string]struct{}) []string {
	var missing []string
	for cell, _ := range expected {
		if _, ok := existing[cell]; !ok {
			missing = append(missing, cell)
		}
	}

	return missing
}
