package benchmark_bbs_test

import (
	"fmt"
	"sync"
	"time"

	"github.com/cloudfoundry-incubator/bbs/models"
	"github.com/cloudfoundry-incubator/benchmark-bbs/reporter"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

const (
	RepBulkFetching = "RepBulkFetching"
)

var repBulkCycle = 30 * time.Second

var BenchmarkRepFetching = func(numReps, numTrials int) {
	Describe("Fetching for rep bulk loop", func() {
		Measure("data for rep bulk", func(b Benchmarker) {
			time.Sleep(repBulkCycle)
			b.Time("rep bulk loop", func() {
				wg := sync.WaitGroup{}
				for i := 0; i < numReps; i++ {
					cellID := fmt.Sprintf("cell-%d", i)
					wg.Add(1)
					go func(cellID string) {
						defer func() {
							wg.Done()
						}()

						defer GinkgoRecover()
						b.Time(fmt.Sprintf("%s: fetch actualLRPs", cellID), func() {
							defer GinkgoRecover()
							actuals, err := bbsClient.ActualLRPGroups(models.ActualLRPFilter{CellID: cellID})
							Expect(err).NotTo(HaveOccurred())

							expectedActualLRPCount, ok := expectedActualLRPCounts[cellID]
							Expect(ok).To(BeTrue())

							expectedActualLRPVariation, ok := expectedActualLRPVariations[cellID]
							Expect(ok).To(BeTrue())

							Expect(len(actuals)).To(BeNumerically("~", expectedActualLRPCount, expectedActualLRPVariation))
						}, reporter.ReporterInfo{
							MetricName: RepBulkFetching,
						})
					}(cellID)
				}
				wg.Wait()
			})
		}, numTrials)
	})
}
