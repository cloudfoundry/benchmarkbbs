package benchmark_bbs_test

import (
	"github.com/cloudfoundry-incubator/bbs/models"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Fetching", func() {
	Measure("data for route emitter", func(b Benchmarker) {
		b.Time("fetch all desiredLRP scheduling and actualLRP info", func() {

			_, err := bbsClient.ActualLRPGroups(models.ActualLRPFilter{})
			Expect(err).NotTo(HaveOccurred())

			_, err = bbsClient.DesiredLRPSchedulingInfos(models.DesiredLRPFilter{})
			Expect(err).NotTo(HaveOccurred())
		})

	}, 10)
})
