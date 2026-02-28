import json
import sys
import os
from datetime import datetime


def lambda_handler(event, context):
    print(f"Event: {json.dumps(event)}")
    print(f"Function: {context.function_name}, Version: {context.function_version}")

    return {
        "statusCode": 200,
        "body": json.dumps({
            "message": "Hello from OpenStack Lambda!",
            "input": event,
            "runtime": f"Python {sys.version}",
            "region": os.environ.get("AWS_REGION", "unknown"),
            "timestamp": datetime.now().isoformat(),
        }),
    }
