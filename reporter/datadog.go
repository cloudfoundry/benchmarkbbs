package reporter

import (
	"errors"
	"fmt"
	"time"

	"github.com/onsi/ginkgo/config"
	"github.com/onsi/ginkgo/types"
	"github.com/pivotal-golang/lager"
	"github.com/zorkian/go-datadog-api"
)

type DataDogReporterInfo struct {
	MetricName string
}

type DataDogReporter struct {
	logger        lager.Logger
	metricPrefix  string
	dataDogClient *datadog.Client
}

func NewDataDogReporter(
	logger lager.Logger,
	metricPrefix string,
	dataDogClient *datadog.Client,
) DataDogReporter {
	return DataDogReporter{
		logger:        logger,
		metricPrefix:  metricPrefix,
		dataDogClient: dataDogClient,
	}
}

func (r *DataDogReporter) SpecSuiteWillBegin(config config.GinkgoConfigType, summary *types.SuiteSummary) {
}

func (r *DataDogReporter) BeforeSuiteDidRun(setupSummary *types.SetupSummary) {
}

func (r *DataDogReporter) AfterSuiteDidRun(setupSummary *types.SetupSummary) {
}

func (r *DataDogReporter) SpecWillRun(specSummary *types.SpecSummary) {
}

func (r *DataDogReporter) SpecDidComplete(specSummary *types.SpecSummary) {
	if specSummary.Passed() && specSummary.IsMeasurement {
		for _, measurement := range specSummary.Measurements {
			info, ok := measurement.Info.(DataDogReporterInfo)
			if !ok {
				r.logger.Error("failed-type-assertion-on-measurement-info", errors.New("type-assertion-failed"))
				continue
			}

			if info.MetricName == "" {
				r.logger.Error("failed-blank-metric-name", errors.New("blank-metric-name"))
				continue
			}

			timestamp := float64(time.Now().Unix())
			err := r.dataDogClient.PostMetrics([]datadog.Metric{
				{
					Metric: fmt.Sprintf("%s.%s", r.metricPrefix, info.MetricName),
					Points: []datadog.DataPoint{
						{timestamp, measurement.Average},
					},
				},
			})
			if err != nil {
				r.logger.Error("failed-sending-metrics-to-datadog", err)
				continue
			}
		}
	}
}

func (r *DataDogReporter) SpecSuiteDidEnd(summary *types.SuiteSummary) {
}
