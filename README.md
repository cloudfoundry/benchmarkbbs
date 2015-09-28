# Diego BBS Benchmark Suite

## Usage

To run the suite against [BOSH Lite](https://github.com/cloudfoundry/bosh-lite):

First, download the client SSL certs and keys from diego-release:

```
cp ~/workspace/diego-release/manifest-generation/bosh-lite-stubs/bbs-certs/client.crt bbs-client.crt
cp ~/workspace/diego-release/manifest-generation/bosh-lite-stubs/bbs-certs/client.key bbs-client.key
cp ~/workspace/diego-release/manifest-generation/bosh-lite-stubs/etcd-certs/client.crt etcd-client.crt
cp ~/workspace/diego-release/manifest-generation/bosh-lite-stubs/etcd-certs/client.key etcd-client.key
```

Then run ginkgo:

```
ginkgo -- -desiredLRPs=5000 \
          -bbsAddress=https://10.244.16.130:8889 \
          -etcdCluster=https://10.244.16.130:4001 \
          -etcdCertFile=etcd-client.crt \
          -etcdKeyFile=etcd-client.key \
          -bbsClientKey=bbs-client.key \
          -bbsClientCert=bbs-client.crt
```
