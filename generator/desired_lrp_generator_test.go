package generator_test

import (
	"strings"

	"github.com/cloudfoundry-incubator/benchmark-bbs/generator"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("DesiredLrpGenerator", func() {
	Describe("Generate", func() {
		It("creates the desired number of desired lrps", func() {
			desiredLRPGenerator := generator.NewDesiredLRPGenerator(logger, bbsClient, *etcdClient)
			response, err := etcdClient.Get("/v1/desired_lrp/schedule", false, true)
			Expect(err).To(HaveOccurred())

			err = desiredLRPGenerator.Generate(3)
			Expect(err).ToNot(HaveOccurred())

			response, err = etcdClient.Get("/v1/desired_lrp/schedule", false, true)
			Expect(err).ToNot(HaveOccurred())
			Expect(response.Node.Nodes).To(HaveLen(3))

			for _, node := range response.Node.Nodes {
				guid := strings.TrimPrefix(node.Key, "/v1/desired_lrp/schedule/")
				_, err := bbsClient.DesiredLRPByProcessGuid(guid)
				Expect(err).ToNot(HaveOccurred())
			}
		})
	})
})
