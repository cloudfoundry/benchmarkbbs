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

var repBulkCycle = 30

var BenchmarkRepFetching = func(numReps, numTrials int) {
	Describe("Fetching for rep bulk loop", func() {
		Measure("data for rep bulk", func(b Benchmarker) {
			totalRan := int32(0)
			totalQueued := int32(0)
			var err error
			wg := sync.WaitGroup{}
			queue := operationq.NewSlidingQueue(1)

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
							var actuals []*models.ActualLRPGroup
							b.Time("rep bulk fetch", func() {
								actuals, err = bbsClient.ActualLRPGroups(models.ActualLRPFilter{CellID: cellID})
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
								queue.Push(&lrpOperation{actualLRP, percentWrites, b, &totalRan})
							}
						}, reporter.ReporterInfo{
							MetricName: RepBulkLoop,
						})
					}
				}(cellID)
			}

			wg.Wait()
			Eventually(func() int32 { return totalQueued }, 5*time.Second).Should(Equal(totalRan), "should've ran the same number of queued LRP operations")
		}, 1)
	})
}

type lrpOperation struct {
	actualLRP     *models.ActualLRP
	percentWrites int
	b             Benchmarker
	globalCount   *int32
}

func (lo *lrpOperation) Key() string {
	return lo.actualLRP.ProcessGuid
}

func (lo *lrpOperation) Execute() {
	defer atomic.AddInt32(lo.globalCount, 1)
	var err error
	randomNum := rand.Intn(100)
	isStarting := randomNum < lo.percentWrites
	actualLRP := lo.actualLRP

	lo.b.Time(fmt.Sprintf("claim actual LRP"), func() {
		index := int(actualLRP.ActualLRPKey.Index)
		err = bbsClient.ClaimActualLRP(actualLRP.ActualLRPKey.ProcessGuid, index, &actualLRP.ActualLRPInstanceKey)
		Expect(err).NotTo(HaveOccurred())
	}, reporter.ReporterInfo{
		MetricName: RepClaimActualLRP,
	})

	if isStarting {
		lo.b.Time(fmt.Sprintf("start actual LRP"), func() {
			netInfo := models.NewActualLRPNetInfo("1.2.3.4", models.NewPortMapping(61999, 8080))
			err = bbsClient.StartActualLRP(&actualLRP.ActualLRPKey, &actualLRP.ActualLRPInstanceKey, &netInfo)
			Expect(err).NotTo(HaveOccurred())
		}, reporter.ReporterInfo{
			MetricName: RepStartActualLRP,
		})
	}
}
