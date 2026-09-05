import json


def main(event, context):
    print(json.dumps(event))
    return {"statusCode": 200, "body": "owned-set"}
