package benchmark_bbs_test

import (
	"github.com/cloudfoundry-incubator/bbs/models"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Fetching for Route Emitter", func() {
	Measure("data for route emitter", func(b Benchmarker) {

		b.Time("fetch all actualLRPs", func() {
			_, err := bbsClient.ActualLRPGroups(models.ActualLRPFilter{})
			Expect(err).NotTo(HaveOccurred())
		})

		b.Time("fetch all desiredLRP scheduling info", func() {
			_, err := bbsClient.DesiredLRPSchedulingInfos(models.DesiredLRPFilter{})
			Expect(err).NotTo(HaveOccurred())
		})

	}, 10)
})
