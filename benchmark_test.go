package benchmarkbbs_test

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"code.cloudfoundry.org/bbs/models"
	"code.cloudfoundry.org/benchmarkbbs/reporter"
	"code.cloudfoundry.org/clock"
	"code.cloudfoundry.org/lager"
	"code.cloudfoundry.org/locket"
	"code.cloudfoundry.org/locket/lock"
	locketmodels "code.cloudfoundry.org/locket/models"
	"code.cloudfoundry.org/operationq"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/tedsuo/ifrit"
	"github.com/tedsuo/ifrit/ginkgomon"
)

const (
	CellPresenceFetching              = "CellPresenceFetching"
	CellPresenceSetting               = "CellPresenceSetting"
	FetchActualLRPsAndSchedulingInfos = "FetchActualLRPsAndSchedulingInfos"
	NsyncBulkerFetching               = "NsyncBulkerFetching"
	RepBulkFetching                   = "RepBulkFetching"
	RepBulkLoop                       = "RepBulkLoop"
	RepClaimActualLRP                 = "RepClaimActualLRP"
	RepStartActualLRP                 = "RepStartActualLRP"
)

type lockInfo struct {
	lockPayload *locketmodels.Resource
	lockProcess ifrit.Process
}

var (
	bulkCycle               = 30 * time.Second
	eventCount              = int32(0)
	expectedEventCount      = int32(0)
	expectedLocalEventCount = make(map[string]*int32)
	lockInfos               = []lockInfo{}
	longTermTtl             = int64(864000) // 10 days
)

func expectEventToHaveCellID(cellID string, event models.Event) {
	defer GinkgoRecover()

	e, ok := event.(*models.ActualLRPChangedEvent)
	if !ok || cellID == "" {
		return
	}
	beforeLRP, _, err := e.Before.Resolve()
	Expect(err).NotTo(HaveOccurred())
	Expect(beforeLRP.CellId).To(Equal(cellID))
	afterLRP, _, err := e.After.Resolve()
	Expect(err).NotTo(HaveOccurred())
	Expect(afterLRP.CellId).To(Equal(cellID))
	logger.Info("received-event", lager.Data{"cell_id": cellID, "process_guid": beforeLRP.ProcessGuid, "before_state": beforeLRP.State, "after_state": afterLRP.State})
}

func eventCountRunner(cellID string, counter *int32) func(signals <-chan os.Signal, ready chan<- struct{}) error {
	return func(signals <-chan os.Signal, ready chan<- struct{}) error {
		eventSource, err := bbsClient.SubscribeToEventsByCellID(logger, cellID)
		Expect(err).NotTo(HaveOccurred())
		close(ready)

		eventChan := make(chan models.Event)
		go func() {
			for {
				event, err := eventSource.Next()
				if err != nil {
					logger.Error("error-getting-next-event", err)
					return
				}
				if event != nil {
					eventChan <- event
					continue
				}
				logger.Info("received-nil-event")
			}
		}()

		for {
			select {
			case event := <-eventChan:
				expectEventToHaveCellID(cellID, event)
				atomic.AddInt32(counter, 1)

			case <-signals:
				if eventSource != nil {
					err := eventSource.Close()
					if err != nil {
						logger.Error("failed-closing-event-source", err)
					}
				}
				return nil
			}
		}
	}
}

var BenchmarkTests = func(numReps, numTrials int, localRouteEmitters bool) {
	Describe("main benchmark test", func() {
		var (
			routeEmitterProcesses []ifrit.Process
			process               ifrit.Process
			err                   error
		)

		BeforeEach(func() {
			process = ifrit.Invoke(ifrit.RunFunc(eventCountRunner("", &eventCount)))
			<-process.Ready()
		})

		AfterEach(func() {
			ginkgomon.Kill(process)
			for _, emitterProcess := range routeEmitterProcesses {
				ginkgomon.Kill(emitterProcess)
			}

			for _, lock := range lockInfos {
				logger.Info("killing-lock", lager.Data{"owner": lock.lockPayload.Owner})
				ginkgomon.Kill(lock.lockProcess)

				Eventually(
					lock.lockProcess.Wait(),
					5*time.Second,
				).Should(Receive(), "timed out trying to kill lock "+lock.lockPayload.Owner)

				logger.Info("lock-killed", lager.Data{"owner": lock.lockPayload.Owner})
				_, err = locketClient.Lock(context.Background(), &locketmodels.LockRequest{Resource: lock.lockPayload, TtlInSeconds: longTermTtl})
				Expect(err).NotTo(HaveOccurred())
			}
		})

		Measure("data for benchmarks", func(b Benchmarker) {
			for i := 0; i < numReps; i++ {
				cellID := fmt.Sprintf("cell-%d", i)
				lockInfos = append(lockInfos, cellRegistrar(b, cellID, locketClient))
			}

			wg := sync.WaitGroup{}

			// start fetching cell presences
			wg.Add(1)
			go ensureCellsRegistered(b, &wg, numTrials, numReps)

			// start nsync
			wg.Add(1)
			go nsyncBulkerLoop(b, &wg, numTrials)

			// we need to make sure we don't run out of ports so limit amount of
			// active http requests to 25000
			semaphore := make(chan struct{}, 25000)

			routeEmitterEventCounts := make(map[string]*int32)
			if localRouteEmitters {
				for i := 0; i < numReps; i++ {
					cellID := fmt.Sprintf("cell-%d", i)
					routeEmitterEventCount := new(int32)
					routeEmitterEventCounts[cellID] = routeEmitterEventCount
					// start local route-emitter
					wg.Add(1)
					process := ifrit.Invoke(ifrit.RunFunc(eventCountRunner(cellID, routeEmitterEventCount)))
					<-process.Ready()
					go localRouteEmitter(b, &wg, cellID, semaphore, numTrials)

					routeEmitterProcesses = append(routeEmitterProcesses, process)
				}
			} else {
				wg.Add(1)
				routeEmitterEventCount := new(int32)
				routeEmitterEventCounts["global"] = routeEmitterEventCount
				process := ifrit.Invoke(ifrit.RunFunc(eventCountRunner("", routeEmitterEventCount)))
				<-process.Ready()
				go globalRouteEmitter(b, &wg, semaphore, numTrials)

				routeEmitterProcesses = append(routeEmitterProcesses, process)
			}

			queue := operationq.NewSlidingQueue(numTrials)

			totalRan := int32(0)
			totalQueued := int32(0)

			for i := 0; i < numReps; i++ {
				cellID := fmt.Sprintf("cell-%d", i)
				wg.Add(1)

				localEventCount := new(int32)
				expectedLocalEventCount[cellID] = localEventCount
				go repBulker(b, &wg, cellID, numTrials, semaphore, &totalQueued, &totalRan, &expectedEventCount, localEventCount, queue)
			}

			wg.Wait()

			eventTolerance := float64(atomic.LoadInt32(&expectedEventCount)) * config.ErrorTolerance

			Eventually(func() int32 {
				return atomic.LoadInt32(&eventCount)
			}, 2*time.Minute).Should(BeNumerically("~", expectedEventCount, eventTolerance), "events received")

			Eventually(func() int32 {
				return atomic.LoadInt32(&totalRan)
			}, 2*time.Minute).Should(Equal(totalQueued), "should have run the same number of queued LRP operations")

			if localRouteEmitters {
				for cellID, v := range routeEmitterEventCounts {
					expectedEventCount := float64(atomic.LoadInt32(expectedLocalEventCount[cellID]))
					tolerance := expectedEventCount * config.ErrorTolerance
					logger.Info("checking-local-event-count", lager.Data{
						"cell-id":   cellID,
						"expected":  expectedEventCount,
						"tolerance": tolerance,
					})
					Eventually(func() int32 {
						return atomic.LoadInt32(v)
					}, 2*time.Minute).Should(BeNumerically("~", expectedEventCount, tolerance), "events received")
				}
			} else {
				logger.Info("checking-global-event-count", lager.Data{
					"expected":  expectedEventCount,
					"tolerance": eventTolerance,
				})

				tolerance := float64(expectedEventCount) * config.ErrorTolerance
				Eventually(func() int32 {
					return atomic.LoadInt32(routeEmitterEventCounts["global"])
				}, 2*time.Minute).Should(BeNumerically("~", expectedEventCount, tolerance), "events received")
			}
		}, 1)
	})
}

func getSleepDuration(loopCounter int, cycleTime time.Duration) time.Duration {
	sleepDuration := cycleTime
	if loopCounter == 0 {
		numMilli := rand.Intn(int(cycleTime.Nanoseconds() / 1000000))
		sleepDuration = time.Duration(numMilli) * time.Millisecond
	}
	return sleepDuration
}

func ensureCellsRegistered(b Benchmarker, wg *sync.WaitGroup, numTrials, numReps int) {
	defer GinkgoRecover()
	logger.Info("start-fetching-cell-presences")
	defer logger.Info("finish-fetching-cell-presences")
	defer wg.Done()

	for i := 0; i < numTrials; i++ {
		sleepDuration := getSleepDuration(i, bulkCycle)
		time.Sleep(sleepDuration)
		b.Time("fetch all the cell presences", func() {
			defer GinkgoRecover()
			cells, err := bbsClient.Cells(logger)
			Expect(err).NotTo(HaveOccurred())
			Expect(cells).To(HaveLen(numReps))
		}, reporter.ReporterInfo{
			MetricName: CellPresenceFetching,
		})
	}
}

func nsyncBulkerLoop(b Benchmarker, wg *sync.WaitGroup, numTrials int) {
	defer GinkgoRecover()
	logger.Info("start-nsync-bulker-loop")
	defer logger.Info("finish-nsync-bulker-loop")
	defer wg.Done()

	for i := 0; i < numTrials; i++ {
		sleepDuration := getSleepDuration(i, bulkCycle)
		time.Sleep(sleepDuration)
		b.Time("fetch all desired LRP scheduling info", func() {
			desireds, err := bbsClient.DesiredLRPSchedulingInfos(logger, models.DesiredLRPFilter{})
			Expect(err).NotTo(HaveOccurred())
			Expect(len(desireds)).To(Equal(expectedLRPCount), "Number of DesiredLRPs retrieved in Nsync Bulk Loop")
		}, reporter.ReporterInfo{
			MetricName: NsyncBulkerFetching,
		})
	}
}

func repBulker(b Benchmarker, wg *sync.WaitGroup, cellID string, numTrials int, semaphore chan struct{}, totalQueued, totalRan, expectedEventCount *int32, expectedLocalEventCount *int32, queue operationq.Queue) {
	defer GinkgoRecover()
	defer wg.Done()

	var err error

	for j := 0; j < numTrials; j++ {
		sleepDuration := getSleepDuration(j, bulkCycle)
		time.Sleep(sleepDuration)

		b.Time("rep bulk loop", func() {
			defer GinkgoRecover()
			var actuals []*models.ActualLRPGroup
			b.Time("rep bulk fetch", func() {
				semaphore <- struct{}{}
				actuals, err = bbsClient.ActualLRPGroups(logger, models.ActualLRPFilter{CellID: cellID})
				<-semaphore
				Expect(err).NotTo(HaveOccurred())
			}, reporter.ReporterInfo{
				MetricName: RepBulkFetching,
			})

			expectedActualLRPCount, ok := expectedActualLRPCounts[cellID]
			Expect(ok).To(BeTrue())

			Expect(len(actuals)).To(Equal(expectedActualLRPCount), "Number of ActualLRPs retrieved by cell %s in rep bulk loop", cellID)

			numActuals := len(actuals)
			for k := 0; k < numActuals; k++ {
				actualLRP, _, err := actuals[k].Resolve()
				Expect(err).NotTo(HaveOccurred())
				atomic.AddInt32(totalQueued, 1)
				queue.Push(&lrpOperation{actualLRP, config.PercentWrites, b, totalRan, expectedEventCount, expectedLocalEventCount, semaphore})
			}
		}, reporter.ReporterInfo{
			MetricName: RepBulkLoop,
		})
	}
}

func cellRegistrar(b Benchmarker, cellID string, locketClient locketmodels.LocketClient) lockInfo {
	logger.Info("registering-cell", lager.Data{"cell-id": cellID})
	defer GinkgoRecover()

	lockPayload := lockResource(cellID)

	lockRunner := lock.NewPresenceRunner(
		logger,
		locketClient,
		lockPayload,
		int64(locket.DefaultSessionTTL/time.Second),
		clock.NewClock(),
		locket.RetryInterval,
	)

	var lockProcess ifrit.Process
	b.Time("acquiring lock", func() {
		defer GinkgoRecover()
		lockProcess = ifrit.Background(lockRunner)
		Eventually(lockProcess.Ready()).Should(BeClosed(), "cell "+cellID+" failed to acquire lock")
	}, reporter.ReporterInfo{
		MetricName: CellPresenceSetting,
	})

	return lockInfo{
		lockPayload: lockPayload,
		lockProcess: lockProcess,
	}
}

func localRouteEmitter(b Benchmarker, wg *sync.WaitGroup, cellID string, semaphore chan struct{}, numTrials int) {
	defer GinkgoRecover()

	logger := logger.WithData(lager.Data{"cell-id": cellID})
	logger.Info("start-local-route-emitter-loop")

	defer func() {
		logger.Info("finish-local-route-emitter-loop")
		wg.Done()
	}()

	expectedActualLRPCount, ok := expectedActualLRPCounts[cellID]
	Expect(ok).To(BeTrue())

	for j := 0; j < numTrials; j++ {
		sleepDuration := getSleepDuration(j, bulkCycle)
		time.Sleep(sleepDuration)
		b.Time("fetch all actualLRPs and schedulingInfos", func() {
			semaphore <- struct{}{}
			actuals, err := bbsClient.ActualLRPGroups(logger, models.ActualLRPFilter{CellID: cellID})
			<-semaphore
			Expect(err).NotTo(HaveOccurred())
			Expect(len(actuals)).To(Equal(expectedActualLRPCount), "Number of ActualLRPs retrieved in router-emitter")

			guids := []string{}
			for _, actual := range actuals {
				lrp, _, err := actual.Resolve()
				Expect(err).NotTo(HaveOccurred())
				guids = append(guids, lrp.ProcessGuid)
			}

			semaphore <- struct{}{}
			desireds, err := bbsClient.DesiredLRPSchedulingInfos(logger, models.DesiredLRPFilter{
				ProcessGuids: guids,
			})
			<-semaphore
			Expect(err).NotTo(HaveOccurred())
			Expect(desireds).To(HaveLen(len(guids)))

		}, reporter.ReporterInfo{
			MetricName: FetchActualLRPsAndSchedulingInfos,
		})
	}
}

func globalRouteEmitter(b Benchmarker, wg *sync.WaitGroup, semaphore chan struct{}, numTrials int) {
	defer GinkgoRecover()

	logger.Info("start-global-route-emitter-loop")

	defer func() {
		wg.Done()
		logger.Info("finish-global-route-emitter-loop")
	}()

	for j := 0; j < numTrials; j++ {
		sleepDuration := getSleepDuration(j, bulkCycle)
		time.Sleep(sleepDuration)
		b.Time("fetch all actualLRPs and schedulingInfos", func() {
			semaphore <- struct{}{}
			actuals, err := bbsClient.ActualLRPGroups(logger, models.ActualLRPFilter{})
			<-semaphore
			Expect(err).NotTo(HaveOccurred())
			Expect(len(actuals)).To(Equal(expectedLRPCount), "Number of ActualLRPs retrieved in router-emitter")

			semaphore <- struct{}{}
			desireds, err := bbsClient.DesiredLRPSchedulingInfos(logger, models.DesiredLRPFilter{})
			<-semaphore
			Expect(err).NotTo(HaveOccurred())
			Expect(len(desireds)).To(Equal(expectedLRPCount), "Number of DesiredLRPs retrieved in route-emitter")
		}, reporter.ReporterInfo{
			MetricName: FetchActualLRPsAndSchedulingInfos,
		})
	}
}

type lrpOperation struct {
	actualLRP        *models.ActualLRP
	percentWrites    float64
	b                Benchmarker
	globalCount      *int32
	globalEventCount *int32
	localEventCount  *int32
	semaphore        chan struct{}
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
		netInfo := models.NewActualLRPNetInfo("1.2.3.4", "2.2.2.2", models.NewPortMapping(61999, 8080))
		lo.semaphore <- struct{}{}
		err = bbsClient.StartActualLRP(logger, &actualLRP.ActualLRPKey, &actualLRP.ActualLRPInstanceKey, &netInfo)
		<-lo.semaphore
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Failed to start LRP for process %s, err: %s", actualLRP.ProcessGuid, err.Error()))

		// if the actual lrp was not already started, an event will be generated
		if actualLRP.State != models.ActualLRPStateRunning {
			logger.Info("expecting-start-event", lager.Data{"cell_id": actualLRP.CellId, "process_guid": actualLRP.ProcessGuid})
			atomic.AddInt32(lo.globalEventCount, 1)
			atomic.AddInt32(lo.localEventCount, 1)
		}
	}, reporter.ReporterInfo{
		MetricName: RepStartActualLRP,
	})

	if isClaiming {
		lo.b.Time("claim actual LRP", func() {
			lo.semaphore <- struct{}{}
			err = bbsClient.ClaimActualLRP(logger, &actualLRP.ActualLRPKey, &actualLRP.ActualLRPInstanceKey)
			<-lo.semaphore
			Expect(err).NotTo(HaveOccurred())
			logger.Info("expecting-claim-event", lager.Data{"cell_id": actualLRP.CellId, "process_guid": actualLRP.ProcessGuid})
			atomic.AddInt32(lo.globalEventCount, 1)
			atomic.AddInt32(lo.localEventCount, 1)
		}, reporter.ReporterInfo{
			MetricName: RepClaimActualLRP,
		})
	}
}
