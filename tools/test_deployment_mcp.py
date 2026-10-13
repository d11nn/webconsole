import io
import json
import unittest
from deployment_mcp import Bridge, serve, tools

class MCPTests(unittest.TestCase):
    def test_lifecycle_and_tools(self):
        source = io.StringIO('\n'.join(json.dumps(r) for r in [
            {'jsonrpc':'2.0','id':1,'method':'tools/list'},
            {'jsonrpc':'2.0','id':2,'method':'initialize','params':{'protocolVersion':'2025-06-18'}},
            {'jsonrpc':'2.0','method':'notifications/initialized'},
            {'jsonrpc':'2.0','id':3,'method':'tools/list'},
            {'jsonrpc':'2.0','id':4,'method':'tools/call','params':{'name':'approve','arguments':{'role':'Admin'}}},
        ]))
        sink = io.StringIO()
        serve(Bridge('http://127.0.0.1:5080','fgc_test_secret'),source,sink)
        results = [json.loads(line) for line in sink.getvalue().splitlines()]
        self.assertIn('error',results[0])
        self.assertEqual(results[1]['result']['protocolVersion'],'2025-06-18')
        self.assertEqual({t['name'] for t in results[2]['result']['tools']},{'request_deployment','get_deployment_status','wait_for_job'})
        self.assertTrue(results[3]['result']['isError'])
        self.assertNotIn('fgc_test_secret',sink.getvalue())

    def test_invalid_arguments_and_transport(self):
        bridge=Bridge('http://127.0.0.1:5080','fgc_secret')
        for name,args in [('approve',{}),('request_deployment',{'interface':'eth0','role':'Admin'}),('wait_for_job',{'request_id':'id','timeout':31})]:
            with self.assertRaises(ValueError):bridge.call(name,args)
        for url in ['http://remote.example','https://user:secret@example.org','file:///tmp/a']:
            with self.assertRaises(ValueError):Bridge(url,'secret')
        self.assertTrue(all(t['inputSchema']['additionalProperties'] is False for t in tools()))

if __name__=='__main__':unittest.main()
