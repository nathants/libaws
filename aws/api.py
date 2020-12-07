from aws import client

def apis(name=None):
    for page in client('apigateway').get_paginator('get_rest_apis').paginate():
        for item in page['items']:
            if not name or item['name'] == name:
                yield item['name'], item['id'], ','.join(item['endpointConfiguration']['types']), item['createdDate']

def api_id(name):
    _apis = []
    for _, id, *_ in apis(name):
        _apis.append(id)
    assert len(_apis) == 1, f'didnt find exactly 1 api for name: {name}, {_apis}'
    return _apis[0]

def resource_id(rest_api_id, path):
    for page in client('apigateway').get_paginator('get_resources').paginate(restApiId=rest_api_id):
        for item in page['items']:
            if item['path'] == path:
                return item['id']
