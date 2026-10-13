import io
import json
import unittest
from contextlib import redirect_stdout
from unittest.mock import patch
import agent_team
from team_mcp import serve

class RuntimeStub:
    def delegate(self,*args):raise AssertionError('No delegation permitted in this test')

class TeamAuthorizationTests(unittest.TestCase):
    def setUp(self):self.broker=agent_team.Broker(RuntimeStub());self.output=io.StringIO()
    def exchange(self,token,method,params):
        with redirect_stdout(self.output):return self.broker.exchange({'credential':token,'method':method,'params':params})
    def test_each_role_discovers_its_own_tools(self):
        for role,expected in [('coordinator',{'delegate_task','list_team_capabilities'}),('install-agent',{'install_mongo','install_go','read_installation_guide','inspect_dependencies'}),('troubleshooting-agent',{'get_nf_status','check_mongodb_status','read_redacted_logs'})]:
            token=self.broker.issue(role);response=self.exchange(token,'tools/list',{})
            self.assertEqual({t['name'] for t in response['result']['tools']},expected)
            self.assertNotIn(token,self.output.getvalue())
    def test_cross_role_direct_calls_and_identity_spoofing_are_denied(self):
        token=self.broker.issue('troubleshooting-agent')
        self.assertFalse(self.exchange(token,'tools/call',{'name':'install_go','arguments':{'purpose':'test'}})['ok'])
        self.assertFalse(self.exchange('invalid','tools/list',{})['ok'])
        installer=self.broker.issue('install-agent')
        response=self.exchange(installer,'tools/call',{'name':'install_go','arguments':{'purpose':'test','role':'Admin','execute':True}})
        self.assertFalse(response['ok'])
        self.assertFalse(self.exchange(installer,'tools/call',{'name':'delegate_task','arguments':{'agent':'coordinator','task':'escalate','reason':'test'}})['ok'])
    def test_tool_validation_and_plan_mode(self):
        for tool in ('install_go','install_mongo'):
            agent_team.validate(agent_team.REGISTRY[tool],{'purpose':'a real plan'})
            with self.assertRaises(ValueError):agent_team.validate(agent_team.REGISTRY[tool],{'purpose':'x','command':'sudo anything'})
        with self.assertRaises(ValueError):agent_team.validate(agent_team.REGISTRY['read_redacted_logs'],{'component':'../../etc/shadow'})
    def test_stdio_lifecycle_and_readonly_annotations(self):
        token='private-test-token';source=io.StringIO('\n'.join(json.dumps(r) for r in [
            {'jsonrpc':'2.0','id':1,'method':'initialize'}, {'jsonrpc':'2.0','method':'notifications/initialized'},
            {'jsonrpc':'2.0','id':2,'method':'tools/list'}]))
        sink=io.StringIO()
        with patch('team_mcp.exchange',return_value={'ok':True,'result':{'tools':[{'name':'install_go','inputSchema':{'type':'object'}}]}}):serve('unused',token,source,sink)
        results=[json.loads(line) for line in sink.getvalue().splitlines()]
        self.assertTrue(results[-1]['result']['tools'][0]['annotations']['readOnlyHint'])
        self.assertNotIn(token,sink.getvalue())
    def test_sensitive_fields_are_redacted(self):
        self.assertEqual(agent_team.scrub({'password':'private','access_token':'private'})['password'],'[REDACTED]')
        self.assertNotIn('fgc_12345678901234567890',agent_team.scrub('fgc_12345678901234567890'))

if __name__=='__main__':unittest.main()
