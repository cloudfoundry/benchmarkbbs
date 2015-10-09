package benchmark_bbs_test

import (
	"github.com/cloudfoundry-incubator/bbs/models"
	"github.com/cloudfoundry-incubator/benchmark-bbs/reporter"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

const (
	FetchActualLRPsAndSchedulingInfos = "FetchActualLRPsAndSchedulingInfos"
)

var BenchmarkRouteEmitterFetching = func(numTrials int) {
	Describe("Fetching for Route Emitter", func() {
		Measure("data for route emitter", func(b Benchmarker) {
			b.Time("fetch all actualLRPs", func() {
				actuals, err := bbsClient.ActualLRPGroups(models.ActualLRPFilter{})
				Expect(err).NotTo(HaveOccurred())
				Expect(actuals).To(HaveLen(expectedLRPCount))

				desireds, err := bbsClient.DesiredLRPSchedulingInfos(models.DesiredLRPFilter{})
				Expect(err).NotTo(HaveOccurred())
				Expect(desireds).To(HaveLen(expectedLRPCount))
			}, reporter.ReporterInfo{
				MetricName: FetchActualLRPsAndSchedulingInfos,
			})
		}, numTrials)
	})
}
