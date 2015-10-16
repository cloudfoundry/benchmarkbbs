## BBS Benchmark

To run the BBS benchmarks against [BOSH Lite](https://github.com/cloudfoundry/bosh-lite):

Run ginkgo:

```
ginkgo -- \
  -desiredLRPs=5000 \
  -numTrials=10 \
  -numPopulateWorkers=10 \
  -bbsAddress=https://10.244.16.130:8889 \
  -bbsClientHTTPTimeout=10s \
  -etcdCluster=https://10.244.16.130:4001 \
  -etcdCertFile=$GOPATH/manifest-generation/bosh-lite-stubs/etcd-certs/client.crt \
  -etcdKeyFile=$GOPATH/manifest-generation/bosh-lite-stubs/etcd-certs/client.key \
  -bbsClientCert=$GOPATH/manifest-generation/bosh-lite-stubs/bbs-certs/client.crt \
  -bbsClientKey=$GOPATH/manifest-generation/bosh-lite-stubs/bbs-certs/client.key \
  -encryptionKey="key1:a secure passphrase" \
  -activeKeyLabel=key1 \
  -logLevel=info
```

If you'd like to have metrics emitted to DataDog, add the following flags:

```
-dataDogAPIKey=$DATADOG_API_KEY \
-dataDogAppKey=$DATADOG_APP_KEY \
-metricPrefix=$METRIC_PREFIX
```

If you'd like to have metrics saved to an S3 bucket, add the following flags:

```
-awsAccessKeyID=$AWS_ACCESS_KEY_ID \
-awsSecretAccessKey=$AWS_SECRET_ACCESS_KEY \
-awsBucketName=$AWS_BUCKET_NAME \
-awsRegion=$AWS_REGION # defaults to us-west-1
```

