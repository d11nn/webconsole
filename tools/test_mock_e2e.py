#!/usr/bin/env python3
"""Real login + Mongo + Gin + stdio MCP with MOCK worker; no host installation.
Requires a dedicated Mongo on --mongo-uri (never the production database).
"""
import argparse
from concurrent.futures import ThreadPoolExecutor
from datetime import datetime, timedelta, timezone
import hashlib
import json
import os
from pathlib import Path
import secrets
import subprocess
import tempfile
import time
import urllib.error
import urllib.request
from deployment_mcp import Bridge


def main():
    parser=argparse.ArgumentParser()
    parser.add_argument('--mongo-uri',default='mongodb://127.0.0.1:27019')
    parser.add_argument('--bootstrap',default='/tmp/free5gc-rbac-bootstrap')
    parser.add_argument('--demo',default='/tmp/free5gc-rbac-demo')
    parser.add_argument('--port',type=int,default=5081)
    args=parser.parse_args()
    root=Path(__file__).resolve().parents[1]
    repo=root.parent
    database='rbac_test_'+secrets.token_hex(6)
    url='http://127.0.0.1:'+str(args.port)
    owner='test-owner-'+secrets.token_hex(4)
    password=secrets.token_urlsafe(24)
    credentials=json.dumps({'email':owner,'password':password})
    bootstrap=[args.bootstrap,'--mongo-uri',args.mongo_uri,'--database',database]
    subprocess.run(bootstrap,input=credentials,text=True,check=True,capture_output=True)
    env=dict(os.environ,FREE5GC_DEPLOYMENT_MODE='mock',FREE5GC_DEPLOYMENT_HOST='isolated-test-host',
             FREE5GC_DEPLOYMENT_COMMIT=subprocess.check_output(['git','rev-parse','HEAD'],cwd=repo,text=True).strip(),
             FREE5GC_DEPLOYMENT_SCRIPT_HASH=hashlib.sha256((repo/'quick-setup.sh').read_bytes()).hexdigest())
    log=tempfile.TemporaryFile(mode='w+')
    process=None
    def start():
        proc=subprocess.Popen([args.demo,'--mongo-uri',args.mongo_uri,'--database',database,'--addr','127.0.0.1:'+str(args.port),
                              '--frontend',str(root/'frontend/build')],cwd=root,env=env,stdout=log,stderr=log)
        for _ in range(100):
            try:
                with urllib.request.urlopen(url+'/login',timeout=1) as response:
                    if response.status==200:return proc
            except (urllib.error.URLError,TimeoutError):time.sleep(.1)
            if proc.poll() is not None:raise RuntimeError('Demo startup failed')
        proc.terminate();proc.wait();raise RuntimeError('Demo timeout')
    def api(path,body=None,token=None,agent=False,expected=200,method=None):
        headers={'Content-Type':'application/json'}
        if token:headers['Authorization' if agent else 'Token']=('Bearer ' if agent else '')+token
        request=urllib.request.Request(url+path,data=None if body is None else json.dumps(body).encode(),headers=headers,method=method)
        try:
            with urllib.request.urlopen(request,timeout=15) as response:
                status=response.status;result=json.load(response)
        except urllib.error.HTTPError as exc:
            status=exc.code;result=json.load(exc)
        assert status==expected,(path,status,expected,result)
        return result
    try:
        process=start()
        jwt=api('/api/login',{'username':owner,'password':password})['access_token']
        assert 'deployment:approve' in api('/api/rbac/me',token=jwt)['permissions']
        owner_identity=api('/api/rbac/me',token=jwt)
        owner_tenant=owner_identity['tenantId'];owner_id=owner_identity['userId']
        scoped_user=api('/api/tenant/'+owner_tenant+'/user',{'email':'scoped-'+database,'encryptedPassword':password},jwt)
        assert scoped_user['encryptedPassword']==''
        scoped_jwt=api('/api/login',{'username':'scoped-'+database,'password':password})['access_token']
        # The old same-tenant password-change path must not reset the owner.
        api('/api/change-password',{'email':owner,'encryptedPassword':password+'x'},scoped_jwt,expected=403)
        api('/api/roles',{'userId':scoped_user['userId'],'tenantId':owner_tenant,'role':'TenantAdmin'},jwt)
        foreign=api('/api/tenant',{'tenantName':'foreign-'+database},jwt)
        foreign_id=foreign['tenantId']
        scoped_tenants=api('/api/tenant',token=scoped_jwt)
        assert len(scoped_tenants)==1 and all(t['tenantId']==owner_tenant for t in scoped_tenants)
        api('/api/tenant/'+foreign_id+'/user',token=scoped_jwt,expected=403)
        owner_path='/api/tenant/'+owner_tenant+'/user/'+owner_id
        api(owner_path,{'encryptedPassword':password+'x'},scoped_jwt,expected=403,method='PUT')
        api('/api/roles',{'userId':scoped_user['userId'],'tenantId':owner_tenant,'role':'SubscriberAdmin'},jwt)
        api(owner_path,{'encryptedPassword':password+'x'},scoped_jwt,expected=403,method='PUT')
        api('/api/tenant/'+owner_tenant,{},scoped_jwt,expected=403,method='PUT')
        api('/api/roles',token=scoped_jwt,expected=403)
        api('/api/tenant',{'tenantId':owner_tenant,'tenantName':'takeover'},scoped_jwt,expected=403)
        # A known default password cannot be promoted into host approval.
        weak=api('/api/tenant/'+foreign_id+'/user',{'email':'weak-'+database,'encryptedPassword':'free5gc'},jwt)
        api('/api/roles',{'userId':weak['userId'],'tenantId':foreign_id,'role':'Admin'},jwt,expected=400)
        legacy=api('/api/tenant/'+foreign_id+'/user',{'email':'admin','encryptedPassword':'free5gc'},jwt)
        # Legacy CRUD cannot delete the owner behind role management's back.
        delete=urllib.request.Request(url+owner_path,method='DELETE',headers={'Token':jwt})
        try:urllib.request.urlopen(delete,timeout=10);raise AssertionError('Owner deletion accepted')
        except urllib.error.HTTPError as exc:assert exc.code==403
        created=api('/api/agent-access',{'name':'e2e Agent','host':'isolated-test-host','expiresAt':(datetime.now(timezone.utc)+timedelta(hours=1)).isoformat()},jwt,expected=201)
        token=created['token'];agent_id=created['agent']['id']
        bridge=Bridge(url,token)
        result=bridge.call('request_deployment',{'interface':'eth0'})
        rid=result['request']['id'];plan_hash=result['request']['planHash']
        assert result['request']['status']=='pending' and result['approvalUrl']==url+'/deployments/'+rid
        request_path='/api/deployment-requests/'+rid
        api(request_path+'/approve-and-start',{'planHash':plan_hash},token,True,403)
        api('/api/roles',{'userId':'evil','tenantId':'evil','role':'Admin'},token,True,403)
        api('/api/agent-access',token=token,agent=True,expected=403)
        api('/api/deployment-requests',{'interface':'eth0','role':'Admin','approved':True},token,True,400)
        api('/api/subscriber',token=token,agent=True,expected=403)
        api(request_path+'/approve-and-start',{'planHash':'changed'},jwt,expected=400)
        def approve(_):return api(request_path+'/approve-and-start',{'planHash':plan_hash},jwt)['id']
        with ThreadPoolExecutor(max_workers=8) as pool:jobs=list(pool.map(approve,range(8)))
        assert len(set(jobs))==1
        result=bridge.call('wait_for_job',{'request_id':rid,'timeout':20})
        assert result['request']['status']=='succeeded'
        job=result['job'];assert job['exitCode']==0 and len(job['logs'])>=5 and 'No installation' in job['verification']
        assert api(request_path+'/approve-and-start',{'planHash':plan_hash},jwt)['id']==job['id']
        logs=api('/api/deployment-jobs/'+job['id']+'/logs',token=token,agent=True)
        assert logs['logs']==job['logs']
        # MCP transport runs as a child with its own credential file.
        with tempfile.TemporaryDirectory() as tmp:
            credential=Path(tmp)/'agent.token';credential.write_text(token+'\n');credential.chmod(0o600)
            messages=[{'jsonrpc':'2.0','id':1,'method':'initialize','params':{'protocolVersion':'2025-06-18'}},
                      {'jsonrpc':'2.0','method':'notifications/initialized'},
                      {'jsonrpc':'2.0','id':2,'method':'tools/call','params':{'name':'get_deployment_status','arguments':{'request_id':rid}}}]
            rpc=subprocess.run(['python3',str(root/'tools/deployment_mcp.py'),'--url',url,'--token-file',str(credential)],
                               input='\n'.join(json.dumps(m) for m in messages)+'\n',text=True,capture_output=True,check=True)
            output=[json.loads(line) for line in rpc.stdout.splitlines()]
            assert not output[-1]['result']['isError']
            assert json.loads(output[-1]['result']['content'][0]['text'])['job']['id']==job['id']
            assert token not in rpc.stdout and token not in rpc.stderr
        # Different Agent/session on the same owner may not inspect this request/job.
        second=api('/api/agent-access',{'name':'other session','host':'isolated-test-host','expiresAt':(datetime.now(timezone.utc)+timedelta(hours=1)).isoformat()},jwt,expected=201)['token']
        api(request_path,token=second,agent=True,expected=404)
        api('/api/deployment-jobs/'+job['id']+'/logs',token=second,agent=True,expected=404)
        audit=api('/api/deployment-audit',token=jwt)
        assert sum(e['action']=='job.claim' for e in audit)==1
        assert token not in json.dumps(audit) and password not in json.dumps(audit)
        process.terminate();process.wait(timeout=10);process=start()
        jwt=api('/api/login',{'username':owner,'password':password})['access_token']
        assert 'deployment:approve' in api('/api/rbac/me',token=jwt)['permissions']
        legacy_jwt=api('/api/login',{'username':'admin','password':'free5gc'})['access_token']
        legacy_me=api('/api/rbac/me',token=legacy_jwt)
        assert legacy_me['userId']==legacy['userId'] and legacy_me['tenantId']==foreign_id
        assert 'deployment:approve' not in legacy_me['permissions'] and 'subscriber:manage' in legacy_me['permissions']
        assert bridge.call('get_deployment_status',{'request_id':rid})['job']['id']==job['id']
        repeat=subprocess.run(bootstrap,input=credentials,text=True,capture_output=True)
        assert repeat.returncode!=0 and 'already initialized' in repeat.stderr
        api('/api/agent-access/'+agent_id+'/revoke',{},jwt)
        api(request_path,token=token,agent=True,expected=401)
        log.seek(0);server_log=log.read()
        assert token not in server_log and password not in server_log and jwt not in server_log
        print('PASS: real login, Mongo persistence/restart, MCP stdio, automatic mock job, eight concurrent approvals, tenant isolation, legacy password/owner takeover denial, revocation and secret checks.')
        print('Isolated evidence database:',database,'(test records retained; no production DB changes)')
    finally:
        if process is not None and process.poll() is None:
            process.terminate();process.wait(timeout=10)
        log.close()

if __name__=='__main__':main()
