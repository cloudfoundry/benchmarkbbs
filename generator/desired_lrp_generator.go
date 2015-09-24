package generator

import (
	"fmt"

	"github.com/cloudfoundry-incubator/bbs"
	"github.com/cloudfoundry-incubator/bbs/models/test/model_helpers"
	"github.com/coreos/go-etcd/etcd"
	"github.com/nu7hatch/gouuid"
	"github.com/pivotal-golang/lager"
)

type desiredLRPGenerator struct {
	logger     lager.Logger
	bbsClient  bbs.Client
	etcdClient etcd.Client
}

func NewDesiredLRPGenerator(
	logger lager.Logger,
	bbsClient bbs.Client,
	etcdClient etcd.Client,
) Generator {
	logger = logger.Session("desired-lrp-generator")
	return &desiredLRPGenerator{
		logger:     logger,
		bbsClient:  bbsClient,
		etcdClient: etcdClient,
	}
}

func (g *desiredLRPGenerator) Generate(count int) error {
	g.logger.Info("starting-generate", lager.Data{"count": count})
	templateDesired := model_helpers.NewValidDesiredLRP("template-guid")
	g.bbsClient.DesireLRP(templateDesired)

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

	for i := 0; i < count-1; i++ {
		id, _ := uuid.NewV4()

		_, err := g.etcdClient.Set(fmt.Sprintf("/v1/desired_lrp/schedule/%s", id), templateSchedulingInfo.Node.Value, 0)
		if err != nil {
			g.logger.Error("failed-setting-scheduling-info", err, lager.Data{"scheduling-info-#": i, "uuid": id})
			return err
		}

		_, err = g.etcdClient.Set(fmt.Sprintf("/v1/desired_lrp/run/%s", id), templateRunInfo.Node.Value, 0)
		if err != nil {
			g.logger.Error("failed-setting-run-info", err, lager.Data{"run-info-#": i, "uuid": id})
			return err
		}
	}

	g.logger.Info("finished")
	return nil
}
