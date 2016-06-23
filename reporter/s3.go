package reporter

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/onsi/ginkgo/config"
	ginkgotypes "github.com/onsi/ginkgo/types"
	"github.com/pivotal-golang/lager"
)

type S3Reporter struct {
	logger     lager.Logger
	bucketName string
	uploader   *s3manager.Uploader
}

func NewS3Reporter(
	logger lager.Logger,
	bucketName string,
	uploader *s3manager.Uploader,
) S3Reporter {
	return S3Reporter{
		logger:     logger.Session("s3-reporter"),
		bucketName: bucketName,
		uploader:   uploader,
	}
}

type Data struct {
	Timestamp   int64
	Measurement ginkgotypes.SpecMeasurement
}

func (r *S3Reporter) SpecSuiteWillBegin(config config.GinkgoConfigType, summary *ginkgotypes.SuiteSummary) {
}

func (r *S3Reporter) BeforeSuiteDidRun(setupSummary *ginkgotypes.SetupSummary) {
}

func (r *S3Reporter) AfterSuiteDidRun(setupSummary *ginkgotypes.SetupSummary) {
}

func (r *S3Reporter) SpecWillRun(specSummary *ginkgotypes.SpecSummary) {
}

func (r *S3Reporter) SpecDidComplete(specSummary *ginkgotypes.SpecSummary) {
	if specSummary.Passed() && specSummary.IsMeasurement {
		var metricNames []string
		var measurementData string

		for _, measurement := range specSummary.Measurements {
			if measurement.Info == nil {
				panic(fmt.Sprintf("%#v", specSummary))
			}

			info, ok := measurement.Info.(ReporterInfo)
			if !ok {
				r.logger.Error("failed-type-assertion-on-measurement-info", errors.New("type-assertion-failed"))
				continue
			}

			if info.MetricName == "" {
				r.logger.Error("failed-blank-metric-name", errors.New("blank-metric-name"))
				continue
			}

			now := time.Now()
			data := Data{
				Timestamp:   now.Unix(),
				Measurement: *measurement,
			}

			dataJSON, err := json.Marshal(data)
			if err != nil {
				r.logger.Error("failed-marshaling-data", err)
				continue
			}

			if !foundMetricName(metricNames, info.MetricName) {
				metricNames = append(metricNames, info.MetricName)
			}

			if measurementData == "" {
				measurementData = string(dataJSON)
			} else {
				measurementData = fmt.Sprintf("%s\n%s", measurementData, string(dataJSON))
			}
		}

		var key string
		sort.Strings(metricNames)
		for _, name := range metricNames {
			key += fmt.Sprintf("%s-", name)
		}
		key = key[:len(key)-1]

		_, err := r.uploader.Upload(&s3manager.UploadInput{
			Bucket: aws.String(r.bucketName),
			Key:    aws.String(key),
			Body:   bytes.NewReader([]byte(measurementData)),
		})

		if err != nil {
			r.logger.Error("failed-uploading-metrics-to-s3", err)
			return
		}

		r.logger.Debug("successfully-uploaded-metrics-to-s3", lager.Data{
			"bucket-name": r.bucketName,
			"key":         key,
			"content":     measurementData,
		})
	}
}

func (r *S3Reporter) SpecSuiteDidEnd(summary *ginkgotypes.SuiteSummary) {
}

func foundMetricName(names []string, key string) bool {
	for _, name := range names {
		if name == key {
			return true
		}
	}

	return false
}
