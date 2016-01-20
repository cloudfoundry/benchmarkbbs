package benchmark_bbs_test

import (
	"github.com/cloudfoundry-incubator/bbs/db/etcd"
	"github.com/cloudfoundry-incubator/benchmark-bbs/reporter"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

const (
	ConvergenceGathering = "ConvergenceGathering"
)

var BenchmarkConvergenceGathering = func(numTrials int) {
	Describe("Gathering", func() {
		Measure("data for convergence", func(b Benchmarker) {
			guids := map[string]struct{}{}

			b.Time("BBS' internal gathering of LRPs", func() {
				actuals, err := etcdDB.GatherActualLRPs(logger, guids, &etcd.LRPMetricCounter{})
				Expect(err).NotTo(HaveOccurred())
				Expect(len(actuals)).To(BeNumerically("~", expectedLRPCount, expectedLRPVariation))

				desireds, err := etcdDB.GatherAndPruneDesiredLRPs(logger, guids, &etcd.LRPMetricCounter{})
				Expect(err).NotTo(HaveOccurred())
				Expect(len(desireds)).To(BeNumerically("~", expectedLRPCount, expectedLRPVariation))
			}, reporter.ReporterInfo{
				MetricName: ConvergenceGathering,
			})
		}, numTrials)
	})
}
