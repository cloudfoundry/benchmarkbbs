package generator

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/cloudfoundry-incubator/bbs"
	"github.com/cloudfoundry-incubator/bbs/models"
	"github.com/cloudfoundry/gunk/workpool"
	"github.com/pivotal-golang/lager"
	"github.com/zorkian/go-datadog-api"
)

type DesiredLRPGenerator struct {
	errorTolerance float64
	metricPrefix   string
	bbsClient      bbs.Client
	datadogClient  *datadog.Client
	workPool       *workpool.WorkPool
}

func NewDesiredLRPGenerator(
	errTolerance float64,
	metricPrefix string,
	workpoolSize int,
	bbsClient bbs.Client,
	datadogClient *datadog.Client,
) *DesiredLRPGenerator {
	workPool, err := workpool.NewWorkPool(workpoolSize)
	if err != nil {
		panic(err)
	}
	return &DesiredLRPGenerator{
		errorTolerance: errTolerance,
		metricPrefix:   metricPrefix,
		bbsClient:      bbsClient,
		workPool:       workPool,
		datadogClient:  datadogClient,
	}
}

type stampedError struct {
	error
	guid string
	time.Time
}

func newStampedError(err error, guid string) *stampedError {
	if err != nil {
		return &stampedError{err, guid, time.Now()}
	}
	return nil
}

func newErrDataPoint(err *stampedError) datadog.DataPoint {
	return datadog.DataPoint{float64(err.Unix()), 1}
}

func (g *DesiredLRPGenerator) Generate(logger lager.Logger, count int) (int, error) {
	logger = logger.Session("generate-desired-lrp", lager.Data{"count": count})
	start := time.Now()
	var wg sync.WaitGroup
	errCh := make(chan *stampedError, count)

	logger.Info("queing-started")
	for i := 0; i < count; i++ {
		wg.Add(1)
		id := fmt.Sprintf("BENCHMARK-BBS-GUID-%06d", i)
		g.workPool.Submit(func() {
			defer wg.Done()
			desired, err := newDesiredLRP(id)
			if err != nil {
				errCh <- newStampedError(err, id)
				return
			}
			errCh <- newStampedError(g.bbsClient.DesireLRP(desired), id)
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

	return g.processResults(logger, errCh)
}

func (g *DesiredLRPGenerator) processResults(logger lager.Logger, errCh chan *stampedError) (int, error) {
	var totalResults int
	var errorResults int

	for err := range errCh {
		if err != nil {
			newErr := fmt.Errorf("Error %v GUID %s", err, err.guid)
			logger.Error("failed-seeding-desired-lrps", newErr)
			errorResults++
		}
		totalResults++
	}

	errorRate := float64(errorResults) / float64(totalResults)
	if errorRate > g.errorTolerance {
		err := fmt.Errorf("Error rate of %.3f exceeds tolerance of %.3f", errorRate, g.errorTolerance)
		logger.Error("failed", err)
		return 0, err
	}

	logger.Info("complete", lager.Data{
		"total-results": totalResults,
		"error-results": errorResults,
		"error-rate":    fmt.Sprintf("%.2f", errorRate),
	})

	if g.datadogClient != nil {
		logger.Info("posting-datadog-metrics")
		timestamp := float64(time.Now().Unix())
		err := g.datadogClient.PostMetrics([]datadog.Metric{
			{
				Metric: fmt.Sprintf("%s.failed-bbs-requests", g.metricPrefix),
				Points: []datadog.DataPoint{
					{timestamp, float64(errorResults)},
				},
			},
		})
		if err != nil {
			logger.Error("failed-posting-datadog-metrics", err)
		} else {
			logger.Info("posting-datadog-metrics-complete")
		}
	} else {
		logger.Info("skipping-datadog-metrics")
	}
	return totalResults - errorResults, nil
}

func newDesiredLRP(guid string) (*models.DesiredLRP, error) {
	myRouterJSON := json.RawMessage(`[{"hostnames":["dora.bosh-lite.com"],"port":8080}]`)
	myRouterJSON2 := json.RawMessage(`{"container_port":2222,"host_fingerprint":"44:00:2b:21:19:1a:42:ab:54:2f:c3:9d:97:d6:c8:0f","private_key":"-----BEGIN RSA PRIVATE KEY-----\nMIICXQIBAAKBgQCu4BiQh96+AvbYHDxRhfK9Scsl5diUkb/LIbe7Hx7DZg8iTxvr\nkw+de3i1TZG3wH02bdReBnCXrN/u59q0qqsz8ge71BFqnSF0dJaSmXhWizN0NQEy\n5u4WyqM4WJTzUGFnofJxnwFArHBT6QEtDjqCJxyggjuBrF60x3HtSfp4gQIDAQAB\nAoGBAJp/SbSHFXbxz3tmlrO/j5FEHMJCqnG3wqaIB3a+K8Od60j4c0ZRCr6rUx16\nhn69BOKNbc4UCm02QjEjjcmH7u/jLflvKLR/EeEXpGpAd7i3b5bqNn98PP+KwnbS\nPxbot37KErdwLnlF8QYFZMeqHiXQG8nO1nqroiX+fVUDtipBAkEAx8nDxLet6ObJ\nWzdR/8dSQ5qeCqXlfX9PFN6JHtw/OBZjRP5jc2cfGXAAB2h7w5XBy0tak1+76v+Y\nTrdq/rqAdQJBAOAT7W0FpLAZEJusY4sXkhZJvGO0e9MaOdYx53Z2m2gUgxLuowkS\nOmKn/Oj+jqr8r1FAhnTYBDY3k5lzM9p41l0CQEXQ9j6qSXXYIIllvZv6lX7Wa2Ah\nNR8z+/i5A4XrRZReDnavxyUu5ilHgFsWYhmpHb3jKVXS4KJwi1MGubcmiXkCQQDH\nWrNG5Vhpm0MdXLeLDcNYtO04P2BSpeiC2g81Y7xLUsRyWYEPFvp+vznRCHhhQ0Gu\npht5ZJ4KplNYmBev7QW5AkA2PuQ8n7APgIhi8xBwlZW3jufnSHT8dP6JUCgvvon1\nDvUM22k/ZWRo0mUB4BdGctIqRFiGwB8Hd0WSl7gSb5oF\n-----END RSA PRIVATE KEY-----\n"}`)
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
		DiskMb:    512,
		MemoryMb:  1024,
		CpuWeight: 42,
		Routes: &models.Routes{"my-router": &myRouterJSON,
			"diego-ssh": &myRouterJSON2},
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
