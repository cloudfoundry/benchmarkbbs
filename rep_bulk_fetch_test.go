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
	RepBulkLoop     = "RepBulkLoop"
)

var repBulkCycle = 30

var BenchmarkRepFetching = func(numReps, numTrials int) {
	Describe("Fetching for rep bulk loop", func() {
		var (
			r *rand.Rand
		)

		BeforeEach(func() {
			r = rand.New(rand.NewSource(time.Now().UnixNano()))
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
							MetricName:  RepBulkFetching,
							MetricIndex: cellID,
						})
					}(cellID)
				}
				wg.Wait()
			}, reporter.ReporterInfo{
				MetricName: RepBulkLoop,
			})
		}, numTrials)
	})
}
