# SQS -> Lambda Example (Local Bridge)

This example provides a local SQS-to-Lambda bridge for Tarn.

Tarn does not yet include native SQS event source mappings, so `pump.sh` continuously polls an SQS queue and invokes a Lambda function with an SQS-style `Records` payload.

## Files

- `lambda/index.js` - sample Lambda handler for queue messages
- `setup.sh` - creates/updates the queue and Lambda
- `pump.sh` - polling bridge (queue -> Lambda invoke)
- `send.sh` - sends test messages to the queue

## Prerequisites

- Tarn server running at `http://localhost:4566`
- Tarn CLI binary (default: `./build/tarn` or `tarn` in `PATH`)
- `python3`, `zip`, `curl`

## Usage

```bash
# 1) Setup queue + lambda
./examples/sqs-lambda/setup.sh

# 2) In terminal A: start the queue pump
./examples/sqs-lambda/pump.sh

# 3) In terminal B: send one or more messages
./examples/sqs-lambda/send.sh
./examples/sqs-lambda/send.sh '{"orderId":"1002","status":"PAID"}'
```

You should see `pump.sh` invoke the Lambda and then delete each processed message.

The scripts use the Tarn CLI for queue/function operations. The only direct API call is `DeleteMessage` (via `curl`) because that action is not exposed by the current CLI yet.

## Configurable env vars

- `TARN_ENDPOINT` (default: `http://localhost:4566`)
- `TARN_ACCOUNT_ID` (default: `000000000000`)
- `AWS_DEFAULT_REGION` (default: `us-east-1`)
- `QUEUE_NAME` (default: `orders-queue`)
- `FUNCTION_NAME` (default: `orders-consumer`)
- `TARN_BIN` (optional path to `tarn` CLI)
- `WAIT_SECONDS` (default: `10`, used by `pump.sh`)
