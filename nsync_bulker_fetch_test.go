package benchmark_bbs_test

import (
	"github.com/cloudfoundry-incubator/bbs/models"
	"github.com/cloudfoundry-incubator/benchmark-bbs/datadog_reporter"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

const (
	FetchAllSchedulingInfos = "FetchAllSchedulingInfos"
)

var _ = Describe("Fetching for nsync bulker", func() {
	Measure("DesiredLRPs", func(b Benchmarker) {
		b.Time("fetch all desired LRP scheduling info", func() {
			_, err := bbsClient.DesiredLRPSchedulingInfos(models.DesiredLRPFilter{})
			Expect(err).NotTo(HaveOccurred())
		}, datadog_reporter.DataDogReporterInfo{
			MetricName: FetchAllSchedulingInfos,
		})
	}, 10)
})
