# AWS Terraform compatibility check

This stack checks AWS API compatibility through Terraform and boto3. It pins
Terraform's AWS provider to 6.66.0 and creates 13 resources with default tags.
It covers log retention, Lambda configuration, event mappings, S3 queue
notifications, OpenAPI import, API keys and usage plans. A generic message API
forwards requests to SQS.

Use an isolated Tarn server. The fixture uses example account `123456789012`,
region `eu-west-2`, resource names beginning with `tarn-compatibility` and port 14567.
The broad Go E2E suite uses port 14566.

From the repository root, build and start a server in a separate terminal:

```sh
make build
./build/tarn start --host 127.0.0.1 --port 14567 --region eu-west-2 \
  --data-dir "$(mktemp -d /tmp/tarn-compatibility.XXXXXX)" --persist --ui=false
```

Run the check from this directory. It needs Terraform, Docker, Python and boto3.
The smoke check invokes an ARM64 Node.js Lambda, so the Docker host must support
that platform. The server may need time to download the Lambda base image.

```sh
python3 -c 'import zipfile; z=zipfile.ZipFile("function.zip","w"); z.write("function.js","index.js"); z.close()'
terraform init
terraform apply -auto-approve
terraform plan -detailed-exitcode
python3 smoke.py
terraform destroy -auto-approve
```

The second plan must exit 0 with `No changes`. `smoke.py` checks a URL-encoded S3
object event arriving in SQS, rejection of missing and wrong API keys, an
authorised REST request arriving in SQS, and real ARM64 execution with an
environment variable. It removes its test messages and object before destroy.

For another local port, pass the same endpoint to both tools:

```sh
terraform apply -var='endpoint=http://127.0.0.1:14568' -auto-approve
python3 smoke.py --endpoint http://127.0.0.1:14568
terraform destroy -var='endpoint=http://127.0.0.1:14568' -auto-approve
```

The fixture covers resource lifecycle, configuration readback and message
delivery. IAM roles and CloudWatch log groups remain in memory across this
check. Restart recovery for those resources is outside this check's scope.
