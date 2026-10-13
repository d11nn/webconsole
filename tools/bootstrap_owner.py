#!/usr/bin/env python3
"""Explicit local bootstrap. Password goes only to the child process stdin."""
import getpass
import json
from pathlib import Path
import subprocess
import sys

binary = Path(__file__).resolve().parents[1] / 'bin' / 'rbac-bootstrap'
email = input('Initial host owner login name: ').strip()
password = getpass.getpass('Password (at least 12 characters): ')
if password != getpass.getpass('Repeat password: '):
    raise SystemExit('Passwords do not match')
result = subprocess.run([str(binary), *sys.argv[1:]], input=json.dumps({'email': email, 'password': password}), text=True)
raise SystemExit(result.returncode)
