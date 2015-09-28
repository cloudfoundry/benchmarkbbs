package generator_test

import (
	"strings"

	"github.com/cloudfoundry-incubator/benchmark-bbs/generator"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = FDescribe("ActualLrpGenerator", func() {
	Describe("Generate", func() {
		It("creates the desired number of actual lrps", func() {
			actualLRPGenerator := generator.NewActualLRPGenerator(logger, bbsClient, *etcdClient)
			response, err := etcdClient.Get("/v1/actual/", false, true)
			Expect(err).To(HaveOccurred())

			err = actualLRPGenerator.Generate(3)
			Expect(err).ToNot(HaveOccurred())

			response, err = etcdClient.Get("/v1/actual/", false, true)
			Expect(err).ToNot(HaveOccurred())
			Expect(response.Node.Nodes).To(HaveLen(3))

			for _, node := range response.Node.Nodes {
				guid := strings.TrimPrefix(node.Key, "/v1/actual/")
				_, err := bbsClient.ActualLRPGroupsByProcessGuid(guid)
				Expect(err).ToNot(HaveOccurred())
			}
		})
	})
})
