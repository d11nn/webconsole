#!/usr/bin/env python3
"""MCP 2025-06-18 stdio tools over Webconsole's internal authenticated REST API.
This is not Streamable HTTP or an OAuth authorization server.
"""
import argparse
import json
import os
from pathlib import Path
import stat
import sys
import time
import urllib.error
import urllib.parse
import urllib.request


def tools():
    def schema(properties, required):
        return {'type': 'object', 'properties': properties, 'required': required, 'additionalProperties': False}
    rid = {'request_id': {'type': 'string'}}
    return [
        {'name': 'request_deployment', 'description': 'Submit an immutable MOCK plan; return the web approval link. A human approves once in Webconsole; backend starts automatically.',
         'inputSchema': schema({'interface': {'type': 'string', 'pattern': '^[a-zA-Z0-9_.:-]{1,15}$'}}, ['interface'])},
        {'name': 'get_deployment_status', 'description': 'Read your scoped request, job, logs and verification. No approval or start tool.',
         'inputSchema': schema(rid, ['request_id'])},
        {'name': 'wait_for_job', 'description': 'Wait at most 30 seconds for a terminal result, polling once per second. Does not wake the Agent after the conversation turn ends.',
         'inputSchema': schema({**rid, 'timeout': {'type': 'integer', 'minimum': 1, 'maximum': 30}}, ['request_id'])},
    ]


class Bridge:
    def __init__(self, base_url, token):
        parsed = urllib.parse.urlsplit(base_url)
        if parsed.scheme not in ('http', 'https') or parsed.username or parsed.password or parsed.query or parsed.fragment:
            raise ValueError('Invalid Webconsole URL')
        if parsed.scheme == 'http' and parsed.hostname not in ('127.0.0.1', 'localhost', '::1'):
            raise ValueError('Non-local connections require HTTPS')
        self.base_url = base_url.rstrip('/')
        self.token = token
        # Credentials must never follow a redirect to another server.
        class NoRedirect(urllib.request.HTTPRedirectHandler):
            def redirect_request(self, *args, **kwargs):
                return None
        self.opener = urllib.request.build_opener(NoRedirect())

    def api(self, path, body=None, timeout=10):
        request = urllib.request.Request(self.base_url + path,
            data=None if body is None else json.dumps(body).encode(),
            headers={'Authorization': 'Bearer ' + self.token, 'Content-Type': 'application/json'})
        try:
            with self.opener.open(request, timeout=timeout) as response:
                return json.load(response)
        except urllib.error.HTTPError as exc:
            raise ValueError('Webconsole denied or unavailable (HTTP %s)' % exc.code) from None
        except (urllib.error.URLError, TimeoutError):
            raise ValueError('Webconsole connection unavailable') from None

    def call(self, name, args):
        expected = {'request_deployment': {'interface'}, 'get_deployment_status': {'request_id'}, 'wait_for_job': {'request_id', 'timeout'}}
        if name not in expected or not isinstance(args, dict) or set(args) - expected[name]:
            raise ValueError('Unknown tool or arguments')
        if name == 'request_deployment':
            if not isinstance(args.get('interface'), str):
                raise ValueError('interface required')
            result = self.api('/api/deployment-requests', {'interface': args['interface']})
            result['approvalUrl'] = self.base_url + result['approvalUrl']
            return result
        if not isinstance(args.get('request_id'), str) or not args['request_id']:
            raise ValueError('request_id required')
        path = '/api/deployment-requests/' + urllib.parse.quote(args['request_id'], safe='')
        timeout = args.get('timeout', 20) if name == 'wait_for_job' else 0
        if type(timeout) is not int or not 0 <= timeout <= 30 or (name == 'wait_for_job' and timeout < 1):
            raise ValueError('timeout must be 1..30 seconds')
        deadline = time.monotonic() + timeout
        while True:
            remaining = max(0.1, deadline - time.monotonic()) if timeout else 10
            result = self.api(path, timeout=min(10, remaining))
            if result['request']['status'] in ('succeeded', 'failed', 'rejected', 'expired') or time.monotonic() >= deadline:
                return result
            time.sleep(min(1, max(0, deadline - time.monotonic())))


def serve(bridge, source=sys.stdin, sink=sys.stdout):
    initialized = False
    ready = False
    for line in source:
        req = None
        try:
            req = json.loads(line)
            if not isinstance(req, dict) or req.get('jsonrpc') != '2.0':
                raise ValueError('Invalid request')
            method = req.get('method')
            if 'id' not in req:
                if method == 'notifications/initialized' and initialized:
                    ready = True
                continue
            if method == 'initialize':
                if initialized:
                    raise ValueError('Already initialized')
                initialized = True
                result = {'protocolVersion': '2025-06-18', 'capabilities': {'tools': {}},
                          'serverInfo': {'name': 'free5gc-webconsole-deployment', 'version': '0.1.0'}}
            elif method == 'ping':
                result = {}
            elif not ready:
                raise ValueError('Initialize first')
            elif method == 'tools/list':
                result = {'tools': tools()}
            elif method == 'tools/call':
                params = req.get('params', {})
                try:
                    output = bridge.call(params.get('name'), params.get('arguments', {}))
                    result = {'content': [{'type': 'text', 'text': json.dumps(output, ensure_ascii=False)}], 'isError': False}
                except Exception:
                    # Never include token, headers or raw urllib exception text.
                    result = {'content': [{'type': 'text', 'text': 'Tool denied, invalid input, or Webconsole unavailable.'}], 'isError': True}
            else:
                print(json.dumps({'jsonrpc': '2.0', 'id': req['id'], 'error': {'code': -32601, 'message': 'Method not found'}}), file=sink, flush=True)
                continue
            print(json.dumps({'jsonrpc': '2.0', 'id': req['id'], 'result': result}), file=sink, flush=True)
        except Exception:
            print(json.dumps({'jsonrpc': '2.0', 'id': req.get('id') if isinstance(req, dict) else None,
                              'error': {'code': -32600, 'message': 'Invalid MCP request'}}), file=sink, flush=True)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--url', default='http://127.0.0.1:5000')
    parser.add_argument('--token-file', required=True)
    args = parser.parse_args()
    path = Path(args.token_file)
    info = path.stat()
    if info.st_uid != os.getuid() or stat.S_IMODE(info.st_mode) & 0o077:
        raise SystemExit('Agent token file must belong to this user with mode 0600')
    token = path.read_text().strip()
    if not token.startswith('fgc_'):
        raise SystemExit('Invalid Agent credential file')
    serve(Bridge(args.url, token))


if __name__ == '__main__':
    main()
