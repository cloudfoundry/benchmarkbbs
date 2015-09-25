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

type desiredLRPGenerator struct {
	logger     lager.Logger
	bbsClient  bbs.Client
	etcdClient etcd.Client
	workPool   *workpool.WorkPool
}

func NewDesiredLRPGenerator(
	logger lager.Logger,
	bbsClient bbs.Client,
	etcdClient etcd.Client,
) Generator {
	logger = logger.Session("desired-lrp-generator")
	workPool, _ := workpool.NewWorkPool(10)

	return &desiredLRPGenerator{
		logger:     logger,
		bbsClient:  bbsClient,
		etcdClient: etcdClient,

		workPool: workPool,
	}
}

func (g *desiredLRPGenerator) Generate(count int) error {
	g.logger.Info("generate", lager.Data{"count": count})

	templateDesired, _ := newDesiredLRP("template-guid")
	err := g.bbsClient.DesireLRP(templateDesired)
	if err != nil {
		g.logger.Error("failed-creating-template-desired-lrp", err)
		return err
	}

	templateSchedulingInfo, err := g.etcdClient.Get("/v1/desired_lrp/schedule/template-guid", false, false)
	if err != nil {
		g.logger.Error("failed-getting-template-scheduling-info", err)
		return err
	}

	templateRunInfo, err := g.etcdClient.Get("/v1/desired_lrp/run/template-guid", false, false)
	if err != nil {
		g.logger.Error("failed-getting-template-run-info", err)
		return err
	}

	var wg sync.WaitGroup
	errCh := make(chan error, count*2)
	for i := 0; i < count-1; i++ {
		id, _ := uuid.NewV4()

		wg.Add(1)
		g.workPool.Submit(func() {
			defer wg.Done()
			_, err := g.etcdClient.Set(fmt.Sprintf("/v1/desired_lrp/schedule/%s", id), templateSchedulingInfo.Node.Value, 0)
			errCh <- err
		})

		wg.Add(1)
		g.workPool.Submit(func() {
			defer wg.Done()
			_, err = g.etcdClient.Set(fmt.Sprintf("/v1/desired_lrp/run/%s", id), templateRunInfo.Node.Value, 0)
			errCh <- err
		})
	}

	doneCh := make(chan struct{})
	go func() {
		wg.Wait()
		close(doneCh)
	}()

	var result error
OUTER_LOOP:
	for {
		select {
		case err = <-errCh:
			result = err
			if err != nil {
				g.logger.Error("failed-seeding-desired-lrps", err)
				break OUTER_LOOP
			}
		case <-doneCh:
			break OUTER_LOOP
		default:
		}
	}

	if result == nil {
		g.logger.Info("succeeded", lager.Data{"count": count})
	}

	return result
}

func newDesiredLRP(guid string) (*models.DesiredLRP, error) {
	myRouterJSON := json.RawMessage(`{"foo":"bar"}`)
	modTag := models.NewModificationTag("epoch", 0)
	desiredLRP := &models.DesiredLRP{
		ProcessGuid:          guid,
		Domain:               "some-domain",
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
