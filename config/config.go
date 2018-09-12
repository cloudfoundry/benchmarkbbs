package config

import (
	"encoding/json"
	"os"

	"code.cloudfoundry.org/bbs/encryption"
	"code.cloudfoundry.org/durationjson"
	"code.cloudfoundry.org/lager/lagerflags"
	"code.cloudfoundry.org/locket"
)

type BenchmarkBBSConfig struct {
	AwsAccessKeyID           string                `json:"aws_access_key_id,omitempty"`
	AwsBucketName            string                `json:"aws_bucket_name,omitempty"`
	AwsRegion                string                `json:"aws_region,omitempty"`
	AwsSecretAccessKey       string                `json:"aws_secret_access_key,omitempty"`
	BBSAddress               string                `json:"bbs_address,omitempty"`
	BBSCACert                string                `json:"bbs_ca_cert,omitempty"`
	BBSClientCert            string                `json:"bbs_client_cert,omitempty"`
	BBSClientHTTPTimeout     durationjson.Duration `json:"bbs_client_http_timeout,omitempty"`
	BBSClientKey             string                `json:"bbs_client_key,omitempty"`
	DataDogAPIKey            string                `json:"datadog_api_key,omitempty"`
	DataDogAppKey            string                `json:"datadog_app_key,omitempty"`
	DesiredLRPs              int                   `json:"desired_lrps,omitempty"`
	ErrorTolerance           float64               `json:"error_tolerance,omitempty"`
	LocalRouteEmitters       bool                  `json:"local_route_emitters"`
	LogFilename              string                `json:"log_filename,omitempty"`
	MetricPrefix             string                `json:"metric_prefix,omitempty"`
	NumPopulateWorkers       int                   `json:"num_populate_workers,omitempty"`
	NumReps                  int                   `json:"num_reps,omitempty"`
	NumTrials                int                   `json:"num_trials,omitempty"`
	PercentWrites            float64               `json:"percent_writes,omitempty"`
	ReseedDatabase           bool                  `json:"reseed_database,omitempty"`
	DatabaseConnectionString string                `json:"database_connection_string"`
	DatabaseDriver           string                `json:"database_driver,omitempty"`
	encryption.EncryptionConfig
	lagerflags.LagerConfig
	locket.ClientLocketConfig
}

func NewBenchmarkBBSConfig(configPath string) (BenchmarkBBSConfig, error) {
	benchmarkBBSConfig := BenchmarkBBSConfig{}
	configFile, err := os.Open(configPath)
	if err != nil {
		return BenchmarkBBSConfig{}, err
	}

	defer configFile.Close()

	decoder := json.NewDecoder(configFile)

	err = decoder.Decode(&benchmarkBBSConfig)
	if err != nil {
		return BenchmarkBBSConfig{}, err
	}

	return benchmarkBBSConfig, nil
}
