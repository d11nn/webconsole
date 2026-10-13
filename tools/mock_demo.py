#!/usr/bin/env python3
"""Isolated, interactive mock demo; no existing services or databases are modified.
Requires installed mongod, Go and already-built frontend. Keeps demo evidence.
"""
import argparse
import getpass
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tempfile
import time


def main():
    parser=argparse.ArgumentParser()
    parser.add_argument('--port',type=int,default=5080)
    parser.add_argument('--mongo-port',type=int,default=27020)
    parser.add_argument('--state-dir',default=None)
    args=parser.parse_args()
    root=Path(__file__).resolve().parents[1]
    repo=root.parent
    state=Path(args.state_dir or tempfile.mkdtemp(prefix='free5gc-rbac-demo-'))
    state.mkdir(parents=True,exist_ok=True)
    state.chmod(0o700)
    owner_marker=state/'owner-initialized'
    email=password=None
    if not owner_marker.exists():
        email=input('Demo owner name: ').strip()
        password=getpass.getpass('Demo password (12+ characters): ')
        if password!=getpass.getpass('Repeat password: '):raise SystemExit('Passwords do not match')
    go=os.environ.get('GO_BINARY','/usr/local/go/bin/go')
    for command in ('rbac-bootstrap','rbac-demo'):
        subprocess.run([go,'build','-o',str(state/command),'./cmd/'+command],cwd=root,check=True)
    mongo_dir=state/'mongo';mongo_dir.mkdir(exist_ok=True)
    mongo=subprocess.Popen(['mongod','--dbpath',str(mongo_dir),'--bind_ip','127.0.0.1','--port',str(args.mongo_port),
                            '--logpath',str(state/'mongod.log')],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
    demo=None
    try:
        # Driver retry handles initial Mongo startup; dedicated database only.
        time.sleep(1)
        if mongo.poll() is not None: raise SystemExit('Isolated Mongo failed; check port and retained mongod.log')
        uri='mongodb://127.0.0.1:'+str(args.mongo_port)
        if email is not None:
            subprocess.run([str(state/'rbac-bootstrap'),'--mongo-uri',uri,'--database','mock_demo'],
                           input=json.dumps({'email':email,'password':password}),text=True,check=True)
            owner_marker.write_text(email+'\n')
        else:
            print('Existing demo owner:',owner_marker.read_text().strip())
        env=dict(os.environ,FREE5GC_DEPLOYMENT_MODE='mock',FREE5GC_DEPLOYMENT_HOST='mock-demo-host',
                 FREE5GC_DEPLOYMENT_COMMIT=subprocess.check_output(['git','rev-parse','HEAD'],cwd=repo,text=True).strip(),
                 FREE5GC_DEPLOYMENT_SCRIPT_HASH=hashlib.sha256((repo/'quick-setup.sh').read_bytes()).hexdigest(),GIN_MODE='release',FREE5GC_TEAM_MODE='codex',FREE5GC_TEAM_RUNTIME=str(root/'tools/agent_team.py'))
        with (state/'webconsole.log').open('a') as log:
            demo=subprocess.Popen([str(state/'rbac-demo'),'--mongo-uri',uri,'--database','mock_demo','--addr','127.0.0.1:'+str(args.port)],cwd=root,env=env,stdout=log,stderr=log)
            print('Webconsole: http://127.0.0.1:%d/agent-team (real Codex; installation is plan-only)'%args.port,flush=True)
            print('Agent target host: mock-demo-host; evidence retained in',state)
            print('Ctrl-C stops only this demo and its isolated MongoDB.')
            demo.wait()
    finally:
        for process in (demo,mongo):
            if process is not None and process.poll() is None:
                process.terminate()
                try:process.wait(timeout=10)
                except subprocess.TimeoutExpired:process.kill();process.wait()

if __name__=='__main__':
    try:main()
    except KeyboardInterrupt:pass
