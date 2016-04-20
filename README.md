## BBS Benchmark

### Running the Tests

To run the BBS benchmarks against [BOSH Lite](https://github.com/cloudfoundry/bosh-lite):

Before you run these tests, stop the brain vm:
```
bosh stop brain_z1 0
```

Run ginkgo:

```
ginkgo -- \
  -desiredLRPs=5000 \
  -numTrials=10 \
  -numReps=5 \
  -numPopulateWorkers=10 \
  -bbsAddress=https://10.244.16.2:8889 \
  -bbsClientHTTPTimeout=10s \
  -etcdCluster=https://10.244.16.2:4001 \
  -etcdCertFile=$GOPATH/manifest-generation/bosh-lite-stubs/etcd-certs/client.crt \
  -etcdKeyFile=$GOPATH/manifest-generation/bosh-lite-stubs/etcd-certs/client.key \
  -bbsClientCert=$GOPATH/manifest-generation/bosh-lite-stubs/bbs-certs/client.crt \
  -bbsClientKey=$GOPATH/manifest-generation/bosh-lite-stubs/bbs-certs/client.key \
  -encryptionKey="key1:a secure passphrase" \
  -activeKeyLabel=key1 \
  -logFilename=test-output.log \
  -logLevel=info
```

### Metrics

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

#### Rep Bulk Fetch Test

Information will be saved to $AWS_BUCKET_NAME/RepBulkFetching-RepBulkLoop/RFC3339TimeFormat.
This file will contain multiple JSON hashes denoted by field "MetricName".

1. The `RepBulkFetching` metric details how long each bulk fetch cycle took to perform per cell, as well
   as standard distrabution information.
1. The `RepBulkLoop` metric details how long it took all of the reps to finish for 1 trial.

#### Route Emitter Fetch Test

Information will be saved to $AWS_BUCKET_NAME/FetchActualLRPsAndSchedulingInfos/RFC3339TimeFormat.
This file should consist of a single JSON hash detailing the time it took to fetch the data
neccessary for a route emitter bulk loop.

#### LRP Convergence Test

Information will be saved to $AWS_BUCKET_NAME/ConvergenceGathering/RFC3339TimeFormat.
This file should consist of a single JSON hash detailing the time it took to fetch the data
neccessary for a BBS's convergence process.

#### Nsync Bulk Fetch Test

Information will be saved to $AWS_BUCKET_NAME/NsyncBulkerFetching/RFC3339TimeFormat.
This file should consist of a single JSON hash detailing the time it took to fetch the data
neccessary for a NSYNC bulk loop.

### Error Tolerance

If you'd like to change the error tolerance allowed, add the following flag:
```
-errorTolerance=0.025
```
