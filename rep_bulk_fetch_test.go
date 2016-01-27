package benchmark_bbs_test

import (
	"fmt"
	"math/rand"
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

var repBulkCycle = 30

var BenchmarkRepFetching = func(numReps, numTrials int) {
	Describe("Fetching for rep bulk loop", func() {
		var (
			r *rand.Rand
		)

		BeforeEach(func() {
			r = rand.New(rand.NewSource(99))
		})

		Measure("data for rep bulk", func(b Benchmarker) {
			b.Time("rep bulk loop", func() {
				wg := sync.WaitGroup{}
				for i := 0; i < numReps; i++ {
					cellID := fmt.Sprintf("cell-%d", i)
					wg.Add(1)
					go func(cellID string) {
						defer func() {
							wg.Done()
						}()

						// We are setting the sleep duration to a constant spread of randomly generated numbers.
						// This ensures that there should be no issues with timing causing a flacky test as long as
						// we are confident that the numbers should pass.
						sleepDuration := time.Duration(repBulkCycle-r.Intn(repBulkCycle)) * time.Second
						time.Sleep(sleepDuration)

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
