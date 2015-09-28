package generator

import (
	"fmt"
	"sync"

	"github.com/cloudfoundry-incubator/bbs"
	"github.com/cloudfoundry/gunk/workpool"
	"github.com/coreos/go-etcd/etcd"
	"github.com/nu7hatch/gouuid"
	"github.com/pivotal-golang/lager"
)

type actualLRPGenerator struct {
	logger     lager.Logger
	bbsClient  bbs.Client
	etcdClient etcd.Client
	workPool   *workpool.WorkPool
}

func NewActualLRPGenerator(
	logger lager.Logger,
	bbsClient bbs.Client,
	etcdClient etcd.Client,
) Generator {
	logger = logger.Session("actual-lrp-generator")
	workPool, _ := workpool.NewWorkPool(10)

	return &actualLRPGenerator{
		logger:     logger,
		bbsClient:  bbsClient,
		etcdClient: etcdClient,

		workPool: workPool,
	}
}

func (g *actualLRPGenerator) Generate(count int) error {
	g.logger.Info("generate", lager.Data{"count": count})

	templateDesired, _ := newDesiredLRP("template-guid-for-actuals")
	err := g.bbsClient.DesireLRP(templateDesired)
	if err != nil {
		g.logger.Error("failed-creating-template-desired-lrp", err)
		return err
	}

	templateActual, err := g.etcdClient.Get("/v1/actual/template-guid-for-actuals/0/instance", false, false)
	if err != nil {
		g.logger.Error("failed-getting-template-actual", err)
		return err
	}

	var wg sync.WaitGroup
	errCh := make(chan error, count*2)
	for i := 0; i < count-1; i++ {
		id, _ := uuid.NewV4()

		wg.Add(1)
		g.workPool.Submit(func() {
			defer wg.Done()
			_, err := g.etcdClient.Set(fmt.Sprintf("/v1/actual/%s/0/instance", id), templateActual.Node.Value, 0)
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
				g.logger.Error("failed-seeding-actual-lrps", err)
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

	return nil
}
