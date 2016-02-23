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
	RepBulkFetching   = "RepBulkFetching"
	RepBulkLoop       = "RepBulkLoop"
	RepClaimActualLRP = "RepClaimActualLRP"
	RepStartActualLRP = "RepStartActualLRP"
)

var repBulkCycle = 30

var BenchmarkRepFetching = func(numReps, numTrials int) {
	Describe("Fetching for rep bulk loop", func() {
		Measure("data for rep bulk", func(b Benchmarker) {
			wg := sync.WaitGroup{}
			for i := 0; i < numReps; i++ {
				cellID := fmt.Sprintf("cell-%d", i)
				wg.Add(1)
				go func(cellID string) {
					defer wg.Done()

					for j := 0; j < numTrials; j++ {
						sleepDuration := 30 * time.Second
						if j == 0 {
							numMilli := rand.Intn(30000)
							sleepDuration = time.Duration(numMilli) * time.Millisecond
						}
						time.Sleep(sleepDuration)

						b.Time("rep bulk loop", func() {
							defer GinkgoRecover()
							actuals, err := bbsClient.ActualLRPGroups(models.ActualLRPFilter{CellID: cellID})
							Expect(err).NotTo(HaveOccurred())

							expectedActualLRPCount, ok := expectedActualLRPCounts[cellID]
							Expect(ok).To(BeTrue())

							expectedActualLRPVariation, ok := expectedActualLRPVariations[cellID]
							Expect(ok).To(BeTrue())

							Expect(len(actuals)).To(BeNumerically("~", expectedActualLRPCount, expectedActualLRPVariation))

							numActuals := len(actuals)
							for k := 0; k < numActuals; k++ {
								randomNum := rand.Intn(100)
								isStarting := randomNum < percentWrites

								actualLRP, _ := actuals[k].Resolve()

								b.Time(fmt.Sprintf("claim actual LRP"), func() {
									index := int(actualLRP.ActualLRPKey.Index)
									err = bbsClient.ClaimActualLRP(actualLRP.ActualLRPKey.ProcessGuid, index, &actualLRP.ActualLRPInstanceKey)
									Expect(err).NotTo(HaveOccurred())
								}, reporter.ReporterInfo{
									MetricName: RepClaimActualLRP,
								})

								if isStarting {
									b.Time(fmt.Sprintf("start actual LRP"), func() {
										netInfo := models.NewActualLRPNetInfo("1.2.3.4", models.NewPortMapping(61999, 8080))
										err = bbsClient.StartActualLRP(&actualLRP.ActualLRPKey, &actualLRP.ActualLRPInstanceKey, &netInfo)
										Expect(err).NotTo(HaveOccurred())
									}, reporter.ReporterInfo{
										MetricName: RepStartActualLRP,
									})
								}
							}
						}, reporter.ReporterInfo{
							MetricName: RepBulkLoop,
						})
					}
				}(cellID)
			}

			wg.Wait()
		}, 1)
	})
}
