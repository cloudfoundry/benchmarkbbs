package benchmark_bbs_test

import (
	"github.com/cloudfoundry-incubator/bbs/models"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Fetching", func() {
	Measure("DesiredLRPs", func(b Benchmarker) {
		b.Time("fetch all desired LRP scheduling info", func() {
			_, err := bbsClient.DesiredLRPSchedulingInfos(models.DesiredLRPFilter{})
			Expect(err).NotTo(HaveOccurred())
		})

	}, 10)
})
