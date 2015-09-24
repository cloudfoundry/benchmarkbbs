package main

import (
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/cloudfoundry-incubator/bbs"
	etcddb "github.com/cloudfoundry-incubator/bbs/db/etcd"
	"github.com/cloudfoundry-incubator/benchmark-bbs/generator"
	"github.com/cloudfoundry-incubator/cf-lager"
	"github.com/coreos/go-etcd/etcd"
	"github.com/pivotal-golang/lager"
	// "github.com/pivotal-golang/lager"
)

var bbsAddress = flag.String(
	"bbsAddress",
	"",
	"Address of the BBS Server",
)

var bbsCACert = flag.String(
	"bbsCACert",
	"",
	"path to certificate authority cert used for mutually authenticated TLS BBS communication",
)

var bbsClientCert = flag.String(
	"bbsClientCert",
	"",
	"path to client cert used for mutually authenticated TLS BBS communication",
)

var bbsClientKey = flag.String(
	"bbsClientKey",
	"",
	"path to client key used for mutually authenticated TLS BBS communication",
)

var desiredLRPs = flag.Int(
	"desiredLRPs",
	0,
	"number of DesiredLRPs to create",
)

func main() {
	cf_lager.AddFlags(flag.CommandLine)
	etcdFlags := AddETCDFlags(flag.CommandLine)
	flag.Parse()

	logger, _ := cf_lager.New("benchmark-bbs")

	if *bbsAddress == "" {
		log.Fatal("bbsAddress is required")
	}

	etcdOptions, err := etcdFlags.Validate()
	if err != nil {
		log.Fatalf("etcd-validation-failed: %s", err.Error())
	}

	etcdClient := initializeEtcdClient(logger, etcdOptions)

	bbsClient := initializeBBSClient(logger)
	if !bbsClient.Ping() {
		log.Fatal("Couldn't reach BBS")
	}

	_, err = etcdClient.Delete("/v1/desired_lrp/", true)
	if err != nil {
		matches, err := regexp.Match(".*Key not found.*", []byte(err.Error()))
		if !matches {
			log.Fatalf("Failed to wipe the slate clean", err)
		}
	}

	desiredLRPGenerator := generator.NewDesiredLRPGenerator(logger, bbsClient, *etcdClient)

	if *desiredLRPs > 0 {
		err = desiredLRPGenerator.Generate(*desiredLRPs)
		if err != nil {
			log.Fatalf("Unable to geneate LRPs %s", err)
		}
	}

	_, err = etcdClient.Delete("/v1/desired_lrp/", true)
	if err != nil {
		log.Fatalf("Failed to clean up", err)
	}
}

func initializeBBSClient(logger lager.Logger) bbs.Client {
	bbsURL, err := url.Parse(*bbsAddress)
	if err != nil {
		logger.Fatal("Invalid BBS URL", err)
	}

	if bbsURL.Scheme != "https" {
		return bbs.NewClient(*bbsAddress)
	}

	bbsClient, err := bbs.NewSecureClient(*bbsAddress, *bbsCACert, *bbsClientCert, *bbsClientKey, 1, 1)
	if err != nil {
		logger.Fatal("Failed to configure secure BBS client", err)
	}
	return bbsClient
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
		etcdClient.AddRootCA(etcdOptions.CAFile)
	} else {
		etcdClient = etcd.NewClient(etcdOptions.ClusterUrls)
	}
	etcdClient.SetConsistency(etcd.STRONG_CONSISTENCY)

	return etcdClient
}

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
