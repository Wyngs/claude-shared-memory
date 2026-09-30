#!/usr/bin/env python3
"""Run two simulated sessions against a running coordinator using only stdlib."""
import json
import os
import urllib.error
import urllib.request

base = os.environ.get('COORDINATOR_URL', 'http://127.0.0.1:8080').rstrip('/')
admin = os.environ['ADMIN_TOKEN']

def post(path, token, data):
    request = urllib.request.Request(base + path, json.dumps(data).encode(), {'Authorization': 'Bearer ' + token, 'Content-Type': 'application/json'})
    with urllib.request.urlopen(request, timeout=10) as response:
        return json.load(response)

workspace = post('/v1/workspaces', admin, {'name': 'two-session latency investigation'})
path = '/v1/workspaces/' + workspace['workspace_id'] + '/actions/'
token = workspace['workspace_token']

def join(name):
    session = post(path + 'join_workspace', token, {'name': name})
    def call(action, **args):
        return post(path + action, token, {**args, 'agent_id': session['agent']['id'], 'agent_token': session['agent_token']})
    return call

logs, config = join('logs investigator'), join('configuration investigator')
task = logs('announce_work', title='Check endpoint reuse during scale-out')
claim = logs('claim_task', task_id=task['id'])
try:
    config('claim_task', task_id=task['id'])
    raise AssertionError('duplicate claim was accepted')
except urllib.error.HTTPError as error:
    assert error.code == 409
    print('Second session sees the claim conflict and picks another investigation.')
second = config('announce_work', title='Inspect client connection lifetime configuration')
second_claim = config('claim_task', task_id=second['id'])
logs('publish_finding', summary='Existing connections keep using old replicas', evidence='Synthetic demonstration observation; replace with measured evidence')
config('publish_finding', summary='Connection lifetime is unlimited in this scenario', evidence='Synthetic demo configuration')
for client, task, lease, result in [(logs, task, claim, 'Connection recycling needs investigation'), (config, second, second_claim, 'Test a bounded client connection lifetime')]:
    client('complete_task', task_id=task['id'], lease_token=lease['lease']['token'], result=result)
context = logs('get_workspace_context')
assert len(context['workspace']['findings']) == 2
assert all(t['status'] == 'completed' for t in context['workspace']['tasks'].values())
print('Both sessions see two completed tasks and two shared findings.')
print('Event cursor:', context['cursor'])
