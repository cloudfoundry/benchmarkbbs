package benchmark_bbs_test

import (
	"github.com/cloudfoundry-incubator/bbs/models"
	"github.com/cloudfoundry-incubator/benchmark-bbs/reporter"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

const (
	NsyncBulkerFetching = "NsyncBulkerFetching"
)

var BenchmarkNsyncFetching = func(numTrials int) {
	Describe("Fetching for nsync bulker", func() {
		Measure("DesiredLRPs", func(b Benchmarker) {
			b.Time("fetch all desired LRP scheduling info", func() {
				desireds, err := bbsClient.DesiredLRPSchedulingInfos(models.DesiredLRPFilter{})
				Expect(err).NotTo(HaveOccurred())
				Expect(desireds).To(HaveLen(expectedLRPCount))
			}, reporter.ReporterInfo{
				MetricName: NsyncBulkerFetching,
			})
		}, numTrials)
	})
}
