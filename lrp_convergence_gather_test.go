package benchmark_bbs_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Gathering", func() {
	Measure("data for convergence", func(b Benchmarker) {
		guids := map[string]struct{}{}

		b.Time("BBS' internal gathering of Actual LRPs", func() {
			_, err := etcdDB.GatherActualLRPs(logger, guids)
			Expect(err).NotTo(HaveOccurred())
		})

		b.Time("BBS' internal gathering of Desired LRPs", func() {
			_, err := etcdDB.GatherDesiredLRPs(logger, guids)
			Expect(err).NotTo(HaveOccurred())
		})
	}, 10)
})
