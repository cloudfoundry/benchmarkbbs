package benchmark_bbs_test

import (
	"github.com/cloudfoundry-incubator/benchmark-bbs/reporter"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

const (
	ConvergenceGathering = "ConvergenceGathering"
)

var _ = Describe("Gathering", func() {
	Measure("data for convergence", func(b Benchmarker) {
		guids := map[string]struct{}{}

		b.Time("BBS' internal gathering of LRPs", func() {
			_, err := etcdDB.GatherActualLRPs(logger, guids)
			Expect(err).NotTo(HaveOccurred())

			_, err = etcdDB.GatherDesiredLRPs(logger, guids)
			Expect(err).NotTo(HaveOccurred())
		}, reporter.ReporterInfo{
			MetricName: ConvergenceGathering,
		})
	}, 10)
})
