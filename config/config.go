package config

import (
	"encoding/json"
	"os"

	bbsconfig "code.cloudfoundry.org/bbs/cmd/bbs/config"
	"code.cloudfoundry.org/lager/lagerflags"
)

type BenchmarkBBSConfig struct {
	NumTrials          int     `json:"num_trials,omitempty"`
	NumReps            int     `json:"num_reps,omitempty"`
	NumPopulateWorkers int     `json:"num_populate_workers,omitempty"`
	DesiredLRPs        int     `json:"desired_lrps,omitempty"`
	PercentWrites      float64 `json:"percent_writes,omitempty"`
	ErrorTolerance     float64 `json:"error_tolerance,omitempty"`
	LocalRouteEmitters bool    `json:"local_router_emitters"`

	BBSAddress           string             `json:"bbs_address,omitempty"`
	BBSClientCert        string             `json:"bbs_client_cert,omitempty"`
	BBSClientKey         string             `json:"bbs_client_key,omitempty"`
	BBSClientHTTPTimeout bbsconfig.Duration `json:"bbs_client_http_timeout,omitempty"`

	AwsAccessKeyID     string `json:"aws_access_key_id,omitempty"`
	AwsSecretAccessKey string `json:"aws_secret_access_key,omitempty"`
	AwsBucketName      string `json:"aws_bucket_name,omitempty"`
	AwsRegion          string `json:"aws_region,omitempty"`

	DataDogAPIKey string `json:"datadog_api_key,omitempty"`
	DataDogAppKey string `json:"datadog_app_key,omitempty"`
	MetricPrefix  string `json:"metric_prefix,omitempty"`

	LogFilename string `json:"log_filename,omitempty"`

	bbsconfig.BBSConfig // used to get the encryption flags, database, etcd, etc.
	lagerflags.LagerConfig
}

func DefaultConfig() BenchmarkBBSConfig {
	return BenchmarkBBSConfig{
		ErrorTolerance:       0.5,
		NumTrials:            10,
		NumReps:              10,
		NumPopulateWorkers:   10,
		DesiredLRPs:          0,
		PercentWrites:        5.0,
		BBSClientHTTPTimeout: 0,
		LocalRouteEmitters:   false,
		AwsRegion:            "us-west-1",
		LagerConfig:          lagerflags.DefaultLagerConfig(),
		BBSConfig:            bbsconfig.DefaultConfig(),
	}
}

func NewBenchmarkBBSConfig(configPath string) (BenchmarkBBSConfig, error) {
	benchmarkBBSConfig := DefaultConfig()
	configFile, err := os.Open(configPath)
	if err != nil {
		return BenchmarkBBSConfig{}, err
	}
	decoder := json.NewDecoder(configFile)

	err = decoder.Decode(&benchmarkBBSConfig)
	if err != nil {
		return BenchmarkBBSConfig{}, err
	}

	return benchmarkBBSConfig, nil
}
