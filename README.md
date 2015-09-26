# Diego BBS Benchmark Suite

## Usage:

To run the suite against [BOSH Lite](https://github.com/cloudfoundry/bosh-lite):

First, download the bbs-certs from diego-release: https://github.com/cloudfoundry-incubator/diego-release/tree/develop/manifest-generation/bosh-lite-stubs/bbs-certs

```
cp ~/workspace/diego-release/manifest-generation/bosh-lite-stubs/bbs-certs/client.crt bbs-client.crt
cp ~/workspace/diego-release/manifest-generation/bosh-lite-stubs/bbs-certs/client.key bbs-client.key
cp ~/workspace/diego-release/manifest-generation/bosh-lite-stubs/etcd-certs/client.crt etcd-client.crt
cp ~/workspace/diego-release/manifest-generation/bosh-lite-stubs/etcd-certs/client.key etcd-client.key
cp ~/workspace/diego-release/manifest-generation/bosh-lite-stubs/etcd-certs/ca.crt etcd-ca.crt
```

Then run ginkgo:

```
ginkgo -- -desiredLRPs=5000 \
          -bbsAddress=https://10.244.16.130:8889 \
          -etcdCluster=https://etcd.service.cf.internal:4001 \
          -etcdCertFile=etcd-client.crt \
          -etcdKeyFile=etcd-client.key \
          -etcdCaFile=etcd-ca.crt \
          -bbsClientKey=bbs-client.key \
          -bbsClientCert=bbs-client.crt
```
