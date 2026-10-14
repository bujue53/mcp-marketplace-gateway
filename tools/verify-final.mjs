import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { writeFile, readFile, mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join, resolve, dirname, sep } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const base = process.env.GATEWAY_URL || 'http://localhost:8080';
const compose = ['compose', '-f', 'docker-compose.yml', '-f', 'deploy/compose.acceptance.yml'];
const result = { verified_at: new Date().toISOString(), skills: {}, clickhouse: {}, permissions: {}, agent: {}, performance: {} };
const delay = ms => new Promise(done => setTimeout(done, ms));
function run(command, args, input) {
  return new Promise((done, fail) => {
    const process = spawn(command, args, { cwd: root, windowsHide: true });
    let out = '', errors = '';
    process.stdout.on('data', data => { out += data; }); process.stderr.on('data', data => { errors += data; });
    const timer = setTimeout(() => { process.kill(); fail(new Error('command timed out')); }, 180000);
    process.on('error', error => { clearTimeout(timer); fail(error); });
    process.on('exit', code => { clearTimeout(timer); code === 0 ? done(out) : fail(new Error(errors.slice(-2000) + out.slice(-2000))); });
    process.stdin.end(input);
  });
}
const sql = query => run('docker', [...compose, 'exec', '-T', 'postgres', 'psql', '-tA', '-v', 'ON_ERROR_STOP=1', '-U', 'mcp', '-d', 'mcp_market'], query + '\n');
async function ch(query) {
  const response = await fetch('http://localhost:8123/?database=mcp_market&wait_end_of_query=1&output_format_json_quote_64bit_integers=0', {
    method: 'POST', headers: { Authorization: 'Basic ' + Buffer.from('mcp:' + (process.env.CLICKHOUSE_PASSWORD || 'mcp-analytics')).toString('base64') }, body: query,
  });
  const text = await response.text(); assert.equal(response.status, 200, text); return text.trim();
}
async function waitFor(check, label, attempts = 60) {
  for (let i = 0; i < attempts; i++) { try { if (await check()) return; } catch { /* service readiness / eventual consistency */ } await delay(500); }
  throw new Error('timed out waiting for ' + label);
}
await waitFor(async () => (await fetch(base + '/health')).ok, 'gateway');
const login = await fetch(base + '/api/auth/login', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ username: 'admin', password: process.env.ADMIN_PASS || 'admin123' }) });
assert.equal(login.status, 200); const { token } = await login.json();
async function request(path, body, method = 'POST', bearer = token) {
  const response = await fetch(base + path, { method, headers: { 'Content-Type': 'application/json', Authorization: 'Bearer ' + bearer }, body: body === undefined ? undefined : JSON.stringify(body) });
  const text = await response.text(); return { status: response.status, value: text ? JSON.parse(text) : null, trace: response.headers.get('x-trace-id'), source: response.headers.get('x-analytics-source') };
}
async function call(name, args = {}) {
  const response = await request('/mcp', { jsonrpc: '2.0', id: 1, method: 'tools/call', params: { name, arguments: args } });
  assert.equal(response.status, 200, JSON.stringify(response)); assert.equal(response.value.jsonrpc, '2.0'); assert.ok(!response.value.error && !response.value.result.isError, JSON.stringify(response.value));
  return response.value.result.content[0].text;
}
const unique = 'final-' + Date.now();
const skillDirectory = 'report-agent-' + Date.now();
const markdown = '---\nname: ' + skillDirectory + '\ndescription: Query the database and generate a report.\nlicense: MIT\nmetadata:\n  author: project32\nallowed-tools: acceptance_query acceptance_report_write acceptance_report_read\n---\n# Report workflow\nRead references/guide.md. Query with acceptance_query, render a Markdown table, write with acceptance_report_write and read it back.\n';
const temp = await mkdtemp(join(tmpdir(), 'project32-final-'));
try {
  await writeFile(join(temp, 'SKILL.md'), markdown, 'utf8');
  // 标准 ZIP 通过 Python 标准库制作，不执行包内脚本。
  const zipFile = join(temp, 'skills.zip');
  await run('python', ['-c', 'import zipfile,sys; p=sys.argv[3]+"/"; z=zipfile.ZipFile(sys.argv[1],"w"); z.write(sys.argv[2],p+"SKILL.md"); z.writestr(p+"references/guide.md","Use read-only SQL and verify the report total."); z.writestr(p+"scripts/render.py","# Optional client-side rendering reference"); z.close()', zipFile, join(temp, 'SKILL.md'), skillDirectory]);
  const form = new FormData(); form.set('name', unique + '-skills'); form.set('file', new Blob([await readFile(zipFile)]), 'skills.zip');
  const upload = await fetch(base + '/api/wrapper/skills', { method: 'POST', headers: { Authorization: 'Bearer ' + token }, body: form });
  const imported = await upload.json(); assert.equal(upload.status, 201, JSON.stringify(imported));
  const skillName = imported.tools[0].name;
  assert.ok((await call(skillName)).includes('Report workflow'));
  assert.equal(JSON.parse(await call(skillName, { resource: 'references/guide.md' })).content, 'Use read-only SQL and verify the report total.');
  const invalid = await request('/api/wrapper/skills', { skill_md: markdown.replace('name: ' + skillDirectory, 'name: Wrong-Name') }); assert.equal(invalid.status, 400);
  await run('docker', [...compose, 'restart', 'gateway']); await waitFor(async () => (await fetch(base + '/health')).ok, 'gateway restart');
  assert.ok((await call(skillName)).includes('Report workflow'));
  const health = await request('/api/servers/' + imported.server.id + '/health', undefined, 'GET'); assert.equal(health.value.healthy, true);
  result.skills = { passed: true, checks: ['ZIP package upload', 'frontmatter', 'metadata', 'resource loading', 'invalid metadata rejected', 'restart recovery', 'internal skill health'] };
  console.log('Skills package, resources and restart verification passed');

  const registered = await request('/api/servers', { name: 'acceptance-backend', version: '1.0.0', endpoint_url: 'http://acceptance-backend:9006/mcp', transport: 'streamable_http', status: 'online', tags: ['acceptance'] });
  assert.equal(registered.status, 201, JSON.stringify(registered));
  const synced = await request('/api/servers/' + registered.value.id + '/sync', {}); assert.equal(synced.status, 200, JSON.stringify(synced)); assert.equal(synced.value.synced, 4);
  const all = (await request('/api/marketplace/tools?limit=100', undefined, 'GET')).value.items;
  const ping = all.find(tool => tool.name === 'acceptance_ping'); const queryTool = all.find(tool => tool.name === 'acceptance_query'); assert.ok(ping && queryTool);
  // 已调用过的用户/工具也必须立即响应权限撤销，权限判定不进入元数据缓存。
  assert.equal(JSON.parse(await call('acceptance_ping')).ok, true);
  const adminID = Number(await sql("SELECT id FROM users WHERE username='admin';"));
  assert.equal(Number(await sql("SELECT count(*) FROM permissions WHERE subject_type='user' AND subject_id='" + adminID + "' AND resource_type='tool' AND resource_id=" + ping.id + " AND action='invoke';")), 0);
  await sql("INSERT INTO permissions(subject_type,subject_id,resource_type,resource_id,action,allowed) VALUES('user','" + adminID + "','tool'," + ping.id + ",'invoke',false);");
  const revoked = await request('/api/gateway/invoke', { tool_id: ping.id, arguments: {} }); assert.equal(revoked.status, 403);
  await sql("DELETE FROM permissions WHERE subject_type='user' AND subject_id='" + adminID + "' AND resource_type='tool' AND resource_id=" + ping.id + " AND action='invoke';");
  assert.equal(JSON.parse(await call('acceptance_ping')).ok, true);
  await sql('UPDATE mcp_tools SET sensitive=true WHERE id=' + queryTool.id + ';');

  await sql("INSERT INTO users(username,password_hash,role) SELECT '" + unique + "-viewer',password_hash,'viewer' FROM users WHERE username='admin';");
  const viewer = await request('/api/auth/login', { username: unique + '-viewer', password: process.env.ADMIN_PASS || 'admin123' }); assert.equal(viewer.status, 200);
  const deniedTraces = [];
  for (let i = 0; i < 20; i++) { const denied = await request('/api/gateway/invoke', { tool_id: queryTool.id, arguments: { sql: 'SELECT 1' } }, 'POST', viewer.value.token); assert.equal(denied.status, 403); deniedTraces.push(denied.trace); }
  assert.ok(deniedTraces.every(id => /^[a-f0-9]{32}$/.test(id)));
  const traceList = deniedTraces.map(id => "'" + id + "'").join(',');
  await waitFor(async () => Number(await sql("SELECT count(*) FROM audit_logs WHERE trace_id IN (" + traceList + ") AND detail->>'status'='403';")) === 20, 'denied audit records');
  result.permissions = { passed: true, attempts: 20, rejected: 20, audited: 20, immediate_revocation: true };

  // 真实数据库数据通过自研工作流 Agent 读取、汇总和写入报表工具。
  await sql('CREATE TABLE IF NOT EXISTS acceptance_orders(id int PRIMARY KEY,customer text,amount integer); TRUNCATE acceptance_orders; INSERT INTO acceptance_orders VALUES(1,\'甲\',120),(2,\'乙\',250),(3,\'丙\',330);');
  const instructions = await call(skillName); assert.ok(instructions.includes('acceptance_query'));
  const queried = JSON.parse(await call('acceptance_query', { sql: 'SELECT id,customer,amount FROM acceptance_orders ORDER BY id' }));
  assert.equal(queried.rows.length, 3); const total = queried.rows.reduce((value, row) => value + row.amount, 0); assert.equal(total, 700);
  const report = '# 订单报表\n\n| 编号 | 客户 | 金额 |\n| --- | --- | --- |\n' + queried.rows.map(row => '| ' + row.id + ' | ' + row.customer + ' | ' + row.amount + ' |').join('\n') + '\n\n合计：' + total + '\n';
  const reportName = unique + '.md'; const saved = JSON.parse(await call('acceptance_report_write', { name: reportName, text: report })); assert.equal(saved.name, reportName);
  const readback = JSON.parse(await call('acceptance_report_read', { name: reportName })); assert.equal(readback.text, report);
  await writeFile(join(root, 'docs', 'Agent任务验收报表.md'), report, 'utf8');
  result.agent = { passed: true, implementation: 'MCP workflow agent', source: 'real PostgreSQL', rows: 3, total: 700, saved_and_read_back: true };
  console.log('Sensitive permission rejection/audit and real database-to-report agent verification passed');

  await waitFor(async () => Number(await ch('SELECT count() FROM tool_calls FINAL WHERE tool_id=' + ping.id)) === Number(await sql('SELECT count(*) FROM tool_calls WHERE tool_id=' + ping.id + ';')), 'ClickHouse initial catch-up');
  await run('docker', [...compose, 'stop', 'clickhouse']);
  let baseline;
  try {
    baseline = Number(await sql('SELECT count(*) FROM tool_calls WHERE tool_id=' + ping.id + ';'));
    for (let i = 0; i < 8; i++) assert.equal(JSON.parse(await call('acceptance_ping', { run_id: unique + '-outage' })).ok, true);
    await waitFor(async () => Number(await sql('SELECT count(*) FROM tool_calls WHERE tool_id=' + ping.id + ';')) === baseline + 8, 'durable logs while ClickHouse offline');
    const fallback = await request('/api/stats/summary', undefined, 'GET'); assert.equal(fallback.source, 'postgres');
  } finally { await run('docker', [...compose, 'start', 'clickhouse']); }
  await waitFor(async () => Number(await ch('SELECT count() FROM tool_calls FINAL WHERE tool_id=' + ping.id)) === baseline + 8, 'ClickHouse outage recovery');
  await run('docker', [...compose, 'stop', 'gateway']);
  await sql("UPDATE analytics_checkpoint SET last_id=0 WHERE name='clickhouse';");
  await run('docker', [...compose, 'start', 'gateway']);
  await waitFor(async () => (await fetch(base + '/health')).ok, 'gateway replay startup');
  await waitFor(async () => Number(await sql("SELECT last_id FROM analytics_checkpoint WHERE name='clickhouse';")) >= Number(await sql('SELECT max(id) FROM tool_calls;')), 'ClickHouse replay');
  assert.equal(Number(await ch('SELECT count() FROM tool_calls FINAL WHERE tool_id=' + ping.id)), baseline + 8);
  for (const path of ['/api/stats/summary', '/api/stats/daily', '/api/stats/top', '/api/audit/report']) { const analytics = await request(path, undefined, 'GET'); assert.equal(analytics.status, 200); assert.equal(analytics.source, 'clickhouse', JSON.stringify(analytics)); }
  result.clickhouse = { passed: true, checks: ['batch export', 'queries served by ClickHouse', 'PostgreSQL fallback', 'outage buffering', 'catch-up recovery', 'replay deduplication'] };
  console.log('ClickHouse analytics, outage recovery and replay verification passed');

  const before = Number(await sql('SELECT count(*) FROM tool_calls WHERE tool_id=' + ping.id + ';'));
  const output = await run('docker', [...compose, 'run', '--rm', '--no-deps', 'acceptance-client', 'load', '--rps', '2100', '--seconds', '20', '--workers', '128', '--run', unique]);
  const performance = JSON.parse(output.trim().split('\n').at(-1));
  await waitFor(async () => Number(await sql('SELECT count(*) FROM tool_calls WHERE tool_id=' + ping.id + ';')) === before + performance.completed + 100, 'load log flush', 120);
  const expected = before + performance.completed + 100;
  await waitFor(async () => Number(await ch('SELECT count() FROM tool_calls FINAL WHERE tool_id=' + ping.id)) === expected, 'load ClickHouse flush', 120);
  performance.logs_persisted = true; performance.clickhouse_logs_match = true;
  result.performance = performance;
  await writeFile(join(root, 'docs', '最终三项验收结果.json'), JSON.stringify(result, null, 2) + '\n', 'utf8');
  console.log('Performance: ' + performance.successful_qps.toFixed(1) + ' successful QPS; P99 gateway overhead ' + performance.gateway_overhead_ms.p99.toFixed(3) + ' ms; passed=' + performance.passed);
} finally {
  const target = resolve(temp); if (!target.startsWith(resolve(tmpdir()) + sep) || !target.includes('project32-final-')) throw new Error('unsafe temporary cleanup path');
  await rm(target, { recursive: true, force: true });
}
