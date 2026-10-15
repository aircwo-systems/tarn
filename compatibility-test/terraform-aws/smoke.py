"""Exercise the Terraform fixture through boto3 and its REST invoke URL."""

import argparse
import json
import urllib.error
import urllib.request

import boto3


def check(endpoint):
    credentials = {
        "endpoint_url": endpoint,
        "region_name": "eu-west-2",
        "aws_access_key_id": "123456789012",
        "aws_secret_access_key": "test",
    }
    s3 = boto3.client("s3", **credentials)
    sqs = boto3.client("sqs", **credentials)
    functions = boto3.client("lambda", **credentials)
    gateway = boto3.client("apigateway", **credentials)
    queue_url = sqs.get_queue_url(QueueName="tarn-compatibility")["QueueUrl"]

    object_key = "objects/test +file.json"
    s3.put_object(Bucket="tarn-compatibility", Key=object_key, Body=b'{"item":1}')
    try:
        messages = sqs.receive_message(
            QueueUrl=queue_url, MaxNumberOfMessages=10, WaitTimeSeconds=2
        ).get("Messages", [])
        assert len(messages) == 1, "Expected one S3 notification"
        record = json.loads(messages[0]["Body"])["Records"][0]
        assert record["eventName"] == "ObjectCreated:Put", record
        assert record["s3"]["object"]["key"] == "objects%2Ftest+%2Bfile.json", record
        sqs.delete_message(
            QueueUrl=queue_url, ReceiptHandle=messages[0]["ReceiptHandle"]
        )
    finally:
        s3.delete_object(Bucket="tarn-compatibility", Key=object_key)
    print("PASS S3 object -> SQS event, decoded with boto3")

    api = next(
        item for item in gateway.get_rest_apis()["items"]
        if item["name"] == "tarn-compatibility"
    )
    key = next(
        item for item in gateway.get_api_keys(includeValues=True)["items"]
        if item["name"] == "tarn-compatibility"
    )
    invoke_url = (
        f"{endpoint}/restapis/{api['id']}/v1/_user_request_/messages/enqueue"
    )
    for key_value, expected_status in [("", 403), ("wrong", 403), (key["value"], 200)]:
        # Tarn uses the account in this local SigV4-shaped header for routing.
        request = urllib.request.Request(
            invoke_url,
            data=b'{"message":"hello"}',
            headers={
                "Content-Type": "application/json",
                "x-api-key": key_value,
                "Authorization": (
                    "AWS4-HMAC-SHA256 Credential=123456789012/20261015/"
                    "eu-west-2/execute-api/aws4_request, SignedHeaders=host, Signature=0"
                ),
            },
        )
        try:
            with urllib.request.urlopen(request, timeout=30) as response:
                status = response.status
        except urllib.error.HTTPError as error:
            status = error.code
        assert status == expected_status, (status, expected_status)

    messages = sqs.receive_message(
        QueueUrl=queue_url, MaxNumberOfMessages=10, WaitTimeSeconds=2
    ).get("Messages", [])
    assert len(messages) == 1, "Expected one authorised API message"
    assert json.loads(messages[0]["Body"]) == {"message": "hello"}
    sqs.delete_message(QueueUrl=queue_url, ReceiptHandle=messages[0]["ReceiptHandle"])
    print("PASS key rejection, authorised REST request -> SQS")

    result = functions.invoke(FunctionName="tarn-compatibility", Payload=b'{"probe":1}')
    payload = json.load(result["Payload"])
    assert not result.get("FunctionError"), payload
    assert payload["arch"] == "arm64", payload
    assert payload["mode"] == "test", payload
    print("PASS Lambda actually executes arm64 and retains environment variables")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--endpoint", default="http://127.0.0.1:14567")
    check(parser.parse_args().endpoint.rstrip("/"))
