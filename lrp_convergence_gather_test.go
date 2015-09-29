package benchmark_bbs_test

import (
	"crypto/rand"

	etcddb "github.com/cloudfoundry-incubator/bbs/db/etcd"
	"github.com/cloudfoundry-incubator/bbs/encryption"
	"github.com/cloudfoundry-incubator/bbs/format"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Gathering", func() {
	var etcdDB *etcddb.ETCDDB
	BeforeEach(func() {
		keyManager, err := encryptionFlags.Validate()
		Expect(err).NotTo(HaveOccurred())
		cryptor := encryption.NewCryptor(keyManager, rand.Reader)

		etcdDB = etcddb.NewETCD(format.ENCRYPTED_PROTO, cryptor, etcddb.NewStoreClient(etcdClient), nil, nil, nil, nil, nil)
	})

	Measure("data for convergence", func(b Benchmarker) {
		guids := map[string]struct{}{}

		b.Time("BBS' internal gathering of Actual LRPs", func() {
			_, err := etcdDB.GatherActualLRPs(logger, guids)
			Expect(err).NotTo(HaveOccurred())
		})

		b.Time("BBS' internal gathering of Desired LRPs", func() {
			_, err := etcdDB.GatherDesiredLRPs(logger, guids)
			Expect(err).NotTo(HaveOccurred())
		})
	}, 10)
})
