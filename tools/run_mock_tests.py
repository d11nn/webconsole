#!/usr/bin/env python3
"""Build/run mock E2E in an owned ephemeral Mongo process, optionally a browser.
Never uses production MongoDB or systemd; retains evidence even after failure.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import secrets
import shutil
import socket
import subprocess
import tempfile
import time


def main():
    parser=argparse.ArgumentParser()
    parser.add_argument('--browser',action='store_true')
    parser.add_argument('--mongo-port',type=int,default=27019)
    parser.add_argument('--evidence',default='docs/rbac-evidence')
    args=parser.parse_args()
    root=Path(__file__).resolve().parents[1]
    for port in [args.mongo_port,5081]+([5082] if args.browser else []):
        try:
            with socket.socket() as check:
                check.setsockopt(socket.SOL_SOCKET,socket.SO_REUSEADDR,1)
                check.bind(('127.0.0.1',port))
        except OSError:raise SystemExit('Test port %s is occupied; existing service is untouched'%port)
    state=Path(tempfile.mkdtemp(prefix='free5gc-rbac-tests-'));state.chmod(0o700)
    go=os.environ.get('GO_BINARY',shutil.which('go') or '/usr/local/go/bin/go')
    for cmd in ('rbac-bootstrap','rbac-demo'):
        subprocess.run([go,'build','-o',str(state/cmd),'./cmd/'+cmd],cwd=root,check=True)
    data=state/'mongo';data.mkdir()
    mongo=subprocess.Popen(['mongod','--dbpath',str(data),'--bind_ip','127.0.0.1','--port',str(args.mongo_port),
                            '--logpath',str(state/'mongod.log')],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
    uri='mongodb://127.0.0.1:'+str(args.mongo_port)
    try:
        for _ in range(100):
            if mongo.poll() is not None:raise RuntimeError('Isolated Mongo failed; port must be unused; see retained mongod.log')
            try:
                with socket.create_connection(('127.0.0.1',args.mongo_port),timeout=.1):break
            except OSError:time.sleep(.1)
        else:raise RuntimeError('Isolated Mongo startup timeout')
        subprocess.run(['python3',str(root/'tools/test_mock_e2e.py'),'--mongo-uri',uri,'--bootstrap',str(state/'rbac-bootstrap'),'--demo',str(state/'rbac-demo')],check=True)
        if args.browser:
            evidence=(root/args.evidence).resolve();evidence.mkdir(parents=True,exist_ok=True)
            owner='browser-owner';password=secrets.token_urlsafe(24);database='rbac_browser_'+secrets.token_hex(5)
            subprocess.run([str(state/'rbac-bootstrap'),'--mongo-uri',uri,'--database',database],
                           input=json.dumps({'email':owner,'password':password}),text=True,check=True,capture_output=True)
            repo=root.parent
            env=dict(os.environ,FREE5GC_DEPLOYMENT_MODE='mock',FREE5GC_DEPLOYMENT_HOST='isolated-test-host',
                     FREE5GC_DEPLOYMENT_COMMIT=subprocess.check_output(['git','rev-parse','HEAD'],cwd=repo,text=True).strip(),
                     FREE5GC_DEPLOYMENT_SCRIPT_HASH=hashlib.sha256((repo/'quick-setup.sh').read_bytes()).hexdigest(),GIN_MODE='release')
            config=state/'browser.json';config.write_text(json.dumps({'url':'http://127.0.0.1:5082','owner':owner,'password':password,'state':str(state),'evidence':str(evidence)}));config.chmod(0o600)
            with (state/'browser-demo.log').open('w') as log:
                demo=subprocess.Popen([str(state/'rbac-demo'),'--mongo-uri',uri,'--database',database,'--addr','127.0.0.1:5082'],cwd=root,env=env,stdout=log,stderr=log)
                try:
                    for _ in range(100):
                        if demo.poll() is not None:raise RuntimeError('Mock demo failed; see retained log')
                        try:
                            with socket.create_connection(('127.0.0.1',5082),timeout=.1):break
                        except OSError:time.sleep(.1)
                    subprocess.run(['node',str(root/'tools/test_browser.cjs'),str(config)],env=env,check=True)
                finally:
                    demo.terminate();demo.wait(timeout=10)
    finally:
        if mongo.poll() is None:mongo.terminate();mongo.wait(timeout=10)
        print('Test artifacts retained:',state)

if __name__=='__main__':main()
