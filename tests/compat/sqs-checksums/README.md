# SQS attribute checksum compatibility

Run against a disposable Tarn instance. Tests create and delete uniquely named queues and topics. Java 21 and Maven are required. Checksums remain enabled on all clients.

```sh
go run ./cmd/tarn start --host 127.0.0.1 --port 24566 --region eu-west-2 --data-dir /tmp/tarn-checksum-test --persist=false
```

In another terminal, from the repository root:

```sh
mvn -q -f tests/compat/sqs-checksums/pom.xml test -Dtarn.endpoint=http://127.0.0.1:24566
```

The Go regressions run without a server or Java:

```sh
go test -race ./internal/api/sqs ./internal/sqs ./internal/sns
```

For repository-wide race tests, build the Lambda helper binaries and run the existing secrets-extension test separately with its required proxy enabled. The image-function tests require the default proxy setting:

```sh
make secrets-proxy db-proxy
go test ./... -race -count=1 -short -skip '^TestLambdaSecretsExtension$'
TARN_EXPOSE_SECRETS_PROXY=true go test ./test -race -count=1 -short -run '^TestLambdaSecretsExtension$'
go vet ./...
```

## Evidence, 28 October 2026

Baseline HEAD `081fa7ee842c3380c1ea17ea56d2020faba2b887`, tagged `v0.6.5-beta`, fails the receive-checksum regression. This was verified against a clean `git archive HEAD` export with only the regression assertion added. Command:

```sh
go test ./internal/api/sqs -run TestJSONReceiveMessage_ReturnsMessageAttributes -count=1
```

The response contains `MessageAttributes.Author={DataType:String,StringValue:Alice}` and `MD5OfBody=e9d1c729518a2d67824141eba1ee5d8f`, but omits `MD5OfMessageAttributes`. This confirms applicability beyond the older development build reported in the PRD. A published binary was not separately tested.

The handlers omitted checksums on send, batch send and JSON receive. JSON receive also ignored selection; Query receive omitted user attributes altogether. SNS adds `correlationId` to fanout messages even when the publisher supplies no explicit attributes. The fix calculates checksums at response time, after receive selection, using AWS's sorted, length-prefixed byte encoding. Existing stored messages need no migration. SNS delivery contents and system attributes are unchanged.

Release-candidate results for `v0.6.6-beta`:

- Go SQS API, SQS storage/service and SNS suites pass, including the race detector. Fixtures cover String, Number, Binary, custom suffixes and Unicode, with independently calculated literal digests. Protocol tests cover send, batch, receive, filtering, omission, body integrity, deletion and draining.
- Java 21.0.6, AWS SDK v2 2.31.78, SDK v1 1.12.797 and AWSpring Cloud SQS 3.4.2: four integration tests, zero failures or errors.
- SDK v2 sends and receives Unicode and binary attributes and validates batch results. SDK v1 validates Query send and receive.
- SNS fanout passes through SDK v2 and an actual AWSpring listener for envelope and raw delivery. `SqsTemplate.send` with an attribute succeeds. The listener records each synthetic event in a blocking queue; successful processing acknowledges and drains SQS within ten seconds.

Representative Java 21 trace:

```text
SNS -> SQS raw=false attributes=[correlationId] checksum=ed53db65e2974715b94ab20ee762649f acknowledged; queue drained
SNS -> SQS raw=true attributes=[correlationId] checksum=11394c64790ccd824e3e435f81835d1b acknowledged; queue drained
AWSpring listener raw=false business effect recorded; checksum validation enabled
AWSpring listener raw=true business effect recorded; checksum validation enabled
```

Fanout digests change because correlation IDs are generated per publication.

The release candidate was built and tested from a clean HEAD export with only the checksum fix overlaid. Unrelated local changes were excluded. The consuming application's PostgreSQL expiry effect, full Spring Boot configuration, mixed v1 SNS publisher/v2 listener combination and full outbound notification flow still need application-level verification. The test uses a programmatic AWSpring listener, not the application's annotated listener. SDK v1 batch is covered at the Query wire level by Go tests, rather than a Java v1 batch test.

Encoding reference: [AWS message metadata guide](https://docs.aws.amazon.com/AWSSimpleQueueService/latest/SQSDeveloperGuide/sqs-message-metadata.html). Selection reference: [ReceiveMessage API](https://docs.aws.amazon.com/AWSSimpleQueueService/latest/APIReference/API_ReceiveMessage.html).
