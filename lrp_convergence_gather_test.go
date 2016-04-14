package benchmark_bbs_test

import (
	"fmt"
	"time"

	"github.com/cloudfoundry-incubator/bbs/models"
	"github.com/cloudfoundry-incubator/benchmark-bbs/reporter"
	. "github.com/onsi/ginkgo"
)

const (
	ConvergenceGathering = "ConvergenceGathering"
)

var BenchmarkConvergenceGathering = func(numTrials int) {
	Describe("Gathering", func() {
		Measure("data for convergence", func(b Benchmarker) {
			cellSet := models.NewCellSet()
			for i := 0; i < numReps; i++ {
				cellID := fmt.Sprintf("cell-%d", i)
				presence := models.NewCellPresence(cellID, "earth", "north", models.CellCapacity{}, nil, nil)
				cellSet.Add(&presence)
			}

			b.Time("BBS' internal gathering of LRPs", func() {
				activeDB.ConvergeLRPs(logger, cellSet)
			}, reporter.ReporterInfo{
				MetricName: ConvergenceGathering,
			})
			time.Sleep(30 * time.Second)
		}, numTrials)
	})
}
