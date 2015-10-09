package generator

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/cloudfoundry-incubator/bbs"
	"github.com/cloudfoundry-incubator/bbs/models"
	"github.com/cloudfoundry/gunk/workpool"
	"github.com/coreos/go-etcd/etcd"
	"github.com/nu7hatch/gouuid"
	"github.com/pivotal-golang/lager"
)

const ERROR_TOLERANCE = 0.05

type DesiredLRPGenerator struct {
	bbsClient  bbs.Client
	etcdClient etcd.Client
	workPool   *workpool.WorkPool
}

func NewDesiredLRPGenerator(
	workpoolSize int,
	bbsClient bbs.Client,
	etcdClient etcd.Client,
) *DesiredLRPGenerator {
	workPool, err := workpool.NewWorkPool(workpoolSize)
	if err != nil {
		panic(err)
	}
	return &DesiredLRPGenerator{
		bbsClient:  bbsClient,
		etcdClient: etcdClient,
		workPool:   workPool,
	}
}

func (g *DesiredLRPGenerator) Generate(logger lager.Logger, count int) (int, error) {
	logger = logger.Session("generate-desired-lrp", lager.Data{"count": count})
	start := time.Now()
	var wg sync.WaitGroup
	errCh := make(chan error, count)

	logger.Info("queing-started")
	for i := 0; i < count; i++ {
		wg.Add(1)
		g.workPool.Submit(func() {
			defer wg.Done()
			id, err := uuid.NewV4()
			if err != nil {
				panic(err)
			}
			desired, err := newDesiredLRP(id.String())
			if err != nil {
				errCh <- err
				return
			}
			errCh <- g.bbsClient.DesireLRP(desired)
		})

		if i%10000 == 0 {
			logger.Info("queing-progress", lager.Data{"current": i, "total": count})
		}
	}

	logger.Info("queing-complete", lager.Data{"duration": time.Since(start)})

	go func() {
		wg.Wait()
		close(errCh)
	}()

	return processResults(logger, errCh)
}

func newDesiredLRP(guid string) (*models.DesiredLRP, error) {
	myRouterJSON := json.RawMessage(`{"foo":"bar"}`)
	modTag := models.NewModificationTag("epoch", 0)
	desiredLRP := &models.DesiredLRP{
		ProcessGuid:          guid,
		Domain:               "benchmark-bbs",
		RootFs:               "some:rootfs",
		Instances:            1,
		EnvironmentVariables: []*models.EnvironmentVariable{{Name: "FOO", Value: "bar"}},
		Setup:                models.WrapAction(&models.RunAction{Path: "ls", User: "name"}),
		Action:               models.WrapAction(&models.RunAction{Path: "ls", User: "name"}),
		StartTimeout:         15,
		Monitor: models.WrapAction(models.EmitProgressFor(
			models.Timeout(models.Try(models.Parallel(models.Serial(&models.RunAction{Path: "ls", User: "name"}))),
				10*time.Second,
			),
			"start-message",
			"success-message",
			"failure-message",
		)),
		DiskMb:      512,
		MemoryMb:    1024,
		CpuWeight:   42,
		Routes:      &models.Routes{"my-router": &myRouterJSON},
		LogSource:   "some-log-source",
		LogGuid:     "some-log-guid",
		MetricsGuid: "some-metrics-guid",
		Annotation:  "some-annotation",
		EgressRules: []*models.SecurityGroupRule{{
			Protocol:     models.TCPProtocol,
			Destinations: []string{"1.1.1.1/32", "2.2.2.2/32"},
			PortRange:    &models.PortRange{Start: 10, End: 16000},
		}},
		ModificationTag: &modTag,
	}
	err := desiredLRP.Validate()
	if err != nil {
		return nil, err
	}

	return desiredLRP, nil
}

func processResults(logger lager.Logger, errCh chan error) (int, error) {
	var totalResults int
	var errorResults int

	for err := range errCh {
		if err != nil {
			logger.Error("failed-seeding-desired-lrps", err)
			errorResults++
		}
		totalResults++
	}

	errorRate := float64(errorResults) / float64(totalResults)
	if errorRate > ERROR_TOLERANCE {
		err := fmt.Errorf("Error rate of %.3f exceeds tolerance of %.3f", errorRate, ERROR_TOLERANCE)
		logger.Error("failed", err)
		return 0, err
	}

	logger.Info("complete", lager.Data{
		"total-results": totalResults,
		"error-results": errorResults,
		"error-rate":    fmt.Sprintf("%.2f", errorRate),
	})

	return totalResults - errorResults, nil
}
