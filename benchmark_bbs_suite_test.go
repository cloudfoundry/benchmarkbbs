package benchmark_bbs_test

import (
	"crypto/rand"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/cloudfoundry-incubator/bbs"
	etcddb "github.com/cloudfoundry-incubator/bbs/db/etcd"
	"github.com/cloudfoundry-incubator/bbs/encryption"
	"github.com/cloudfoundry-incubator/bbs/format"
	"github.com/cloudfoundry-incubator/benchmark-bbs/generator"
	"github.com/cloudfoundry-incubator/benchmark-bbs/reporter"
	"github.com/cloudfoundry-incubator/cf-lager"
	"github.com/cloudfoundry-incubator/cf_http"
	"github.com/coreos/go-etcd/etcd"
	"github.com/pivotal-golang/lager"
	"github.com/zorkian/go-datadog-api"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

var bbsAddress string
var bbsClientCert string
var bbsClientKey string
var etcdFlags *ETCDFlags
var dataDogAPIKey string
var dataDogAppKey string
var awsAccessKeyID string
var awsSecretAccessKey string
var awsBucketName string
var awsRegion string
var desiredLRPs int
var actualLRPs int
var encryptionFlags *encryption.EncryptionFlags
var metricPrefix string

var logger lager.Logger
var etcdClient *etcd.Client
var etcdDB *etcddb.ETCDDB
var bbsClient bbs.Client
var bbsClientHTTPTimeout time.Duration
var dataDogReporter reporter.DataDogReporter
var reporters []Reporter

func init() {
	flag.StringVar(&bbsAddress, "bbsAddress", "", "Address of the BBS Server")
	flag.StringVar(&bbsClientCert, "bbsClientCert", "", "BBS client SSL certificate")
	flag.StringVar(&bbsClientKey, "bbsClientKey", "", "BBS client SSL key")
	flag.DurationVar(&bbsClientHTTPTimeout, "bbsClientHTTPTimeout", 0, "BBS client HTTP timeout")
	flag.StringVar(&dataDogAPIKey, "dataDogAPIKey", "", "DataDog API key")
	flag.StringVar(&dataDogAppKey, "dataDogAppKey", "", "DataDog app Key")
	flag.StringVar(&awsAccessKeyID, "awsAccessKeyID", "", "AWS Access Key ID")
	flag.StringVar(&awsSecretAccessKey, "awsSecretAccessKey", "", "AWS Secret Access Key")
	flag.StringVar(&awsBucketName, "awsBucketName", "", "AWS Bucket to store metrics")
	flag.StringVar(&awsRegion, "awsRegion", "us-west-1", "AWS Bucket to store metrics")
	flag.StringVar(&metricPrefix, "metricPrefix", "", "DataDog metric prefix")

	flag.IntVar(&desiredLRPs, "desiredLRPs", 0, "number of DesiredLRPs to create")
	flag.IntVar(&actualLRPs, "actualLRPs", 0, "number of ActualLRPs to create")

	cf_lager.AddFlags(flag.CommandLine)
	etcdFlags = AddETCDFlags(flag.CommandLine)
	encryptionFlags = encryption.AddEncryptionFlags(flag.CommandLine)

	flag.Parse()

	if bbsAddress == "" {
		log.Fatal("bbsAddress is required")
	}
}

func TestBenchmarkBbs(t *testing.T) {
	logger = lager.NewLogger("test")
	logger.RegisterSink(lager.NewWriterSink(GinkgoWriter, lager.DEBUG))

	reporters = []Reporter{}

	if dataDogAPIKey != "" && dataDogAppKey != "" {
		dataDogClient := datadog.NewClient(dataDogAPIKey, dataDogAppKey)
		dataDogReporter = reporter.NewDataDogReporter(logger, metricPrefix, dataDogClient)
		reporters = append(reporters, &dataDogReporter)
	}

	if awsAccessKeyID != "" && awsSecretAccessKey != "" && awsBucketName != "" {
		creds := credentials.NewStaticCredentials(awsAccessKeyID, awsSecretAccessKey, "")
		s3Client := s3.New(&aws.Config{
			Region:      &awsRegion,
			Credentials: creds,
		})
		uploader := s3manager.NewUploader(&s3manager.UploadOptions{S3: s3Client})
		reporter := reporter.NewS3Reporter(logger, awsBucketName, uploader)
		reporters = append(reporters, &reporter)
	}

	RegisterFailHandler(Fail)
	RunSpecsWithDefaultAndCustomReporters(t, "Benchmark BBS Suite", reporters)
}

var _ = BeforeSuite(func() {
	etcdOptions, err := etcdFlags.Validate()
	Expect(err).NotTo(HaveOccurred())

	etcdClient = initializeEtcdClient(logger, etcdOptions)
	bbsClient = initializeBBSClient(logger, bbsClientHTTPTimeout)
	etcdDB = initializeETCDDB(logger, etcdClient)

	purge("/v1/desired_lrp")
	purge("/v1/actual")

	_, err = bbsClient.Domains()
	Expect(err).NotTo(HaveOccurred())

	if desiredLRPs > 0 {
		desiredLRPGenerator := generator.NewDesiredLRPGenerator(logger, bbsClient, *etcdClient)
		err := desiredLRPGenerator.Generate(desiredLRPs)
		Expect(err).NotTo(HaveOccurred())
	}

	if actualLRPs > 0 {
		actualLRPGenerator := generator.NewActualLRPGenerator(logger, bbsClient, *etcdClient)
		err := actualLRPGenerator.Generate(actualLRPs)
		Expect(err).NotTo(HaveOccurred())
	}
})

var _ = AfterSuite(func() {
	purge("/v1/desired_lrp")
	purge("/v1/actual")
})

type ETCDFlags struct {
	etcdCertFile           string
	etcdKeyFile            string
	etcdCaFile             string
	clusterUrls            string
	clientSessionCacheSize int
	maxIdleConnsPerHost    int
}

func AddETCDFlags(flagSet *flag.FlagSet) *ETCDFlags {
	flags := &ETCDFlags{}

	flagSet.StringVar(
		&flags.clusterUrls,
		"etcdCluster",
		"http://127.0.0.1:4001",
		"comma-separated list of etcd URLs (scheme://ip:port)",
	)
	flagSet.StringVar(
		&flags.etcdCertFile,
		"etcdCertFile",
		"",
		"Location of the client certificate for mutual auth",
	)
	flagSet.StringVar(
		&flags.etcdKeyFile,
		"etcdKeyFile",
		"",
		"Location of the client key for mutual auth",
	)
	flagSet.StringVar(
		&flags.etcdCaFile,
		"etcdCaFile",
		"",
		"Location of the CA certificate for mutual auth",
	)

	flagSet.IntVar(
		&flags.clientSessionCacheSize,
		"etcdSessionCacheSize",
		0,
		"Capacity of the ClientSessionCache option on the TLS configuration. If zero, golang's default will be used",
	)
	flagSet.IntVar(
		&flags.maxIdleConnsPerHost,
		"etcdMaxIdleConnsPerHost",
		0,
		"Controls the maximum number of idle (keep-alive) connctions per host. If zero, golang's default will be used",
	)
	return flags
}

func (flags *ETCDFlags) Validate() (*etcddb.ETCDOptions, error) {
	scheme := ""
	clusterUrls := strings.Split(flags.clusterUrls, ",")
	for i, uString := range clusterUrls {
		uString = strings.TrimSpace(uString)
		clusterUrls[i] = uString
		u, err := url.Parse(uString)
		if err != nil {
			return nil, fmt.Errorf("Invalid cluster URL: '%s', error: [%s]", uString, err.Error())
		}
		if scheme == "" {
			if u.Scheme != "http" && u.Scheme != "https" {
				return nil, errors.New("Invalid scheme: " + uString)
			}
			scheme = u.Scheme
		} else if scheme != u.Scheme {
			return nil, fmt.Errorf("Multiple url schemes provided: %s", flags.clusterUrls)
		}
	}

	isSSL := false
	if scheme == "https" {
		isSSL = true
		if flags.etcdCertFile == "" {
			return nil, errors.New("Cert file must be provided for https connections")
		}
		if flags.etcdKeyFile == "" {
			return nil, errors.New("Key file must be provided for https connections")
		}
	}

	return &etcddb.ETCDOptions{
		CertFile:    flags.etcdCertFile,
		KeyFile:     flags.etcdKeyFile,
		CAFile:      flags.etcdCaFile,
		ClusterUrls: clusterUrls,
		IsSSL:       isSSL,
		ClientSessionCacheSize: flags.clientSessionCacheSize,
		MaxIdleConnsPerHost:    flags.maxIdleConnsPerHost,
	}, nil
}

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
	keyManager, err := encryptionFlags.Validate()
	Expect(err).NotTo(HaveOccurred())
	cryptor := encryption.NewCryptor(keyManager, rand.Reader)

	return etcddb.NewETCD(format.ENCRYPTED_PROTO, cryptor, etcddb.NewStoreClient(etcdClient), nil, nil, nil, nil, nil)
}

func initializeBBSClient(logger lager.Logger, bbsClientHTTPTimeout time.Duration) bbs.Client {
	bbsURL, err := url.Parse(bbsAddress)
	if err != nil {
		logger.Fatal("Invalid BBS URL", err)
	}

	if bbsURL.Scheme != "https" {
		return bbs.NewClient(bbsAddress)
	}

	cf_http.Initialize(bbsClientHTTPTimeout)
	bbsClient, err := bbs.NewSecureSkipVerifyClient(bbsAddress, bbsClientCert, bbsClientKey, 1, 1)
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
