package benchmark_bbs_test

import (
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cloudfoundry-incubator/bbs/models"
	"github.com/cloudfoundry-incubator/benchmark-bbs/reporter"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/pivotal-golang/operationq"
)

const (
	RepBulkFetching   = "RepBulkFetching"
	RepBulkLoop       = "RepBulkLoop"
	RepClaimActualLRP = "RepClaimActualLRP"
	RepStartActualLRP = "RepStartActualLRP"
)

var repBulkCycle = 30 * time.Second

var BenchmarkRepFetching = func(numReps, numTrials int) {
	Describe("Fetching for rep bulk loop", func() {
		Measure("data for rep bulk", func(b Benchmarker) {
			totalRan := int32(0)
			totalQueued := int32(0)
			var err error
			wg := sync.WaitGroup{}
			queue := operationq.NewSlidingQueue(numTrials)

			// we need to make sure we don't run out of ports so limit amount of
			// active http requests to 25000
			semaphore := make(chan struct{}, 25000)

			for i := 0; i < numReps; i++ {
				cellID := fmt.Sprintf("cell-%d", i)
				wg.Add(1)

				go func(cellID string) {
					defer wg.Done()

					for j := 0; j < numTrials; j++ {
						sleepDuration := repBulkCycle
						if j == 0 {
							numMilli := rand.Intn(int(repBulkCycle.Nanoseconds() / 1000000))
							sleepDuration = time.Duration(numMilli) * time.Millisecond
						}
						time.Sleep(sleepDuration)

						b.Time("rep bulk loop", func() {
							defer GinkgoRecover()
							var actuals []*models.ActualLRPGroup
							b.Time("rep bulk fetch", func() {
								semaphore <- struct{}{}
								actuals, err = bbsClient.ActualLRPGroups(models.ActualLRPFilter{CellID: cellID})
								<-semaphore
								Expect(err).NotTo(HaveOccurred())
							}, reporter.ReporterInfo{
								MetricName: RepBulkFetching,
							})

							expectedActualLRPCount, ok := expectedActualLRPCounts[cellID]
							Expect(ok).To(BeTrue())

							expectedActualLRPVariation, ok := expectedActualLRPVariations[cellID]
							Expect(ok).To(BeTrue())

							Expect(len(actuals)).To(BeNumerically("~", expectedActualLRPCount, expectedActualLRPVariation))

							numActuals := len(actuals)
							for k := 0; k < numActuals; k++ {
								actualLRP, _ := actuals[k].Resolve()
								atomic.AddInt32(&totalQueued, 1)
								queue.Push(&lrpOperation{actualLRP, percentWrites, b, &totalRan, semaphore})
							}
						}, reporter.ReporterInfo{
							MetricName: RepBulkLoop,
						})
					}
				}(cellID)
			}

			wg.Wait()
			Eventually(func() int32 { return totalRan }, 2*time.Minute).Should(Equal(totalQueued), "should have run the same number of queued LRP operations")
		}, 1)
	})
}

type lrpOperation struct {
	actualLRP     *models.ActualLRP
	percentWrites float64
	b             Benchmarker
	globalCount   *int32
	semaphore     chan struct{}
}

func (lo *lrpOperation) Key() string {
	return lo.actualLRP.ProcessGuid
}

func (lo *lrpOperation) Execute() {
	defer GinkgoRecover()
	defer atomic.AddInt32(lo.globalCount, 1)
	var err error
	randomNum := rand.Float64() * 100.0

	// divided by 2 because the start following the claim cause two writes.
	isClaiming := randomNum < (lo.percentWrites / 2)
	actualLRP := lo.actualLRP

	lo.b.Time("start actual LRP", func() {
		netInfo := models.NewActualLRPNetInfo("1.2.3.4", models.NewPortMapping(61999, 8080))
		lo.semaphore <- struct{}{}
		err = bbsClient.StartActualLRP(&actualLRP.ActualLRPKey, &actualLRP.ActualLRPInstanceKey, &netInfo)
		<-lo.semaphore
		Expect(err).NotTo(HaveOccurred())
	}, reporter.ReporterInfo{
		MetricName: RepStartActualLRP,
	})

	if isClaiming {
		lo.b.Time("claim actual LRP", func() {
			index := int(actualLRP.ActualLRPKey.Index)
			lo.semaphore <- struct{}{}
			err = bbsClient.ClaimActualLRP(actualLRP.ActualLRPKey.ProcessGuid, index, &actualLRP.ActualLRPInstanceKey)
			<-lo.semaphore
			Expect(err).NotTo(HaveOccurred())
		}, reporter.ReporterInfo{
			MetricName: RepClaimActualLRP,
		})
	}
}
