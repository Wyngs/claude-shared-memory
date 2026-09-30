import { test } from 'node:test';
import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { once } from 'node:events';
import readline from 'node:readline';
import { createServer } from 'node:net';

async function freePort() {
  const server = createServer(); server.listen(0, '127.0.0.1'); await once(server, 'listening');
  const port = server.address().port; await new Promise(resolve => server.close(resolve)); return port;
}
function bridge(env) {
  const child = spawn(process.execPath, ['mcp/server.mjs'], { env: { ...process.env, ...env }, stdio: ['pipe', 'pipe', 'pipe'] });
  const pending = new Map(); let counter = 0;
  readline.createInterface({ input: child.stdout }).on('line', line => { const msg = JSON.parse(line); const resolve = pending.get(msg.id); if (resolve) { pending.delete(msg.id); resolve(msg); } });
  return { child, rpc(method, params) { const id = ++counter; return new Promise((resolve, reject) => {
    const timeout = setTimeout(() => { pending.delete(id); reject(new Error(`MCP timed out: ${method}`)); }, 5000);
    pending.set(id, msg => { clearTimeout(timeout); resolve(msg); }); child.stdin.write(JSON.stringify({ jsonrpc: '2.0', id, method, params }) + '\n');
  }); } };
}
test('two independent MCP sessions coordinate through the real HTTP server', async t => {
  const port = await freePort(); const url = `http://127.0.0.1:${port}`;
  const admin = 'integration-test-admin-token-32-characters';
  const server = spawn(process.env.COORDINATOR_BINARY || './coordinator', [], { env: { ...process.env, STORE: 'memory', DATABASE_URL: '', ADMIN_TOKEN: admin, LISTEN_ADDR: `127.0.0.1:${port}` }, stdio: 'ignore' });
  t.after(() => server.kill());
  let ready = false;
  for (let i = 0; i < 50; i++) { try { if ((await fetch(`${url}/healthz`)).ok) { ready = true; break; } } catch {} await new Promise(resolve => setTimeout(resolve, 20)); }
  assert.ok(ready, 'server starts');
  const response = await fetch(`${url}/v1/workspaces`, { method: 'POST', headers: { Authorization: `Bearer ${admin}`, 'Content-Type': 'application/json' }, body: JSON.stringify({ name: 'scale-out debugging' }) });
  assert.equal(response.status, 200); const workspace = await response.json();
  const env = { COORDINATOR_URL: url, WORKSPACE_ID: workspace.workspace_id, WORKSPACE_TOKEN: workspace.workspace_token };
  const a = bridge(env), b = bridge(env); t.after(() => { a.child.kill(); b.child.kill(); });
  for (const session of [a, b]) {
    const init = await session.rpc('initialize', { protocolVersion: '2025-11-25', capabilities: {}, clientInfo: { name: 'test', version: '1' } });
    assert.equal(init.result.protocolVersion, '2025-11-25');
    const list = await session.rpc('tools/list', {}); assert.equal(list.result.tools.length, 12);
  }
  const raw = (s, name, args = {}) => s.rpc('tools/call', { name, arguments: args });
  const call = async (s, name, args = {}) => { const r = await raw(s, name, args); assert.equal(r.result.isError, false, r.result.content[0].text); return JSON.parse(r.result.content[0].text); };
  const joined = await call(a, 'join_workspace', { name: 'logs' }); assert.ok(!('agent_token' in joined));
  await call(b, 'join_workspace', { name: 'config' });
  const task = await call(a, 'announce_work', { title: 'inspect stale connections' });
  const lease = await call(a, 'claim_task', { task_id: task.id });
  const conflict = await raw(b, 'claim_task', { task_id: task.id }); assert.equal(conflict.result.isError, true);
  const context = await call(b, 'get_workspace_context'); assert.equal(context.workspace.tasks[task.id].lease.token, '');
  const injection = await raw(b, 'heartbeat', { agent_id: joined.agent.id }); assert.equal(injection.result.isError, true);
  await call(a, 'publish_finding', { summary: 'connections retain old endpoints', evidence: 'reproducer: keep a connection open while scaling' });
  await call(a, 'complete_task', { task_id: task.id, lease_token: lease.lease.token, result: 'recycle connections to new replicas' });
  const updated = await call(b, 'get_workspace_context'); assert.equal(updated.workspace.findings.length, 1); assert.equal(updated.workspace.tasks[task.id].status, 'completed');
  const events = await call(b, 'poll_events'); assert.equal(events.events.length, 6);
  const tail = await call(b, 'poll_events', { after: events.cursor }); assert.deepEqual(tail.events, []);
});
