import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { spawn } from 'node:child_process';
import { once } from 'node:events';
import { mkdtemp, mkdir, writeFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join, resolve, sep, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const gateway = process.env.GATEWAY_URL || 'http://localhost:8080';
const temporary = await mkdtemp(join(tmpdir(), 'project32-skeleton-'));
const auditTraceIDs = [];
const registeredServerIDs = [];
const gatewayChecks = [];
const results = [];
let mock;

async function run(command, args, cwd, input) {
  return new Promise((done, fail) => {
    const child = spawn(command, args, { cwd, windowsHide: true, env: { ...process.env, GOCACHE: join(tmpdir(), 'project32-go-cache') } });
    let output = '';
    child.stdout.on('data', data => { output += data; });
    child.stderr.on('data', data => { output += data; });
    const timer = setTimeout(() => { child.kill(); fail(new Error(command + ' timed out')); }, 120000);
    child.on('error', error => { clearTimeout(timer); fail(error); });
    child.on('exit', code => {
      clearTimeout(timer);
      if (code === 0) done(output);
      else fail(new Error(command + ' exited ' + code + ': ' + output.slice(-4000)));
    });
    if (input !== undefined) child.stdin.end(input);
    else child.stdin.end();
  });
}

function npm(args, cwd) {
  return process.platform === 'win32'
    ? run('cmd.exe', ['/d', '/s', '/c', 'npm ' + args.join(' ')], cwd)
    : run('npm', args, cwd);
}

async function start(command, args, cwd, baseURL) {
  const child = spawn(command, args, {
    cwd, windowsHide: true,
    env: { ...process.env, REST_BASE_URL: baseURL, PORT: '0', HOST: '127.0.0.1', MCP_BEARER_TOKEN: 'qa-mcp-token', REST_BEARER_TOKEN: 'qa-rest-token' },
  });
  try {
    const url = await new Promise((done, fail) => {
      let output = '';
      let errors = '';
      const timer = setTimeout(() => fail(new Error('generated server startup timed out')), 15000);
      child.stdout.on('data', data => {
        output += data;
        const match = output.match(/MCP_URL=(http:\/\/[^\s]+)/);
        if (match) { clearTimeout(timer); done(match[1]); }
      });
      child.stderr.on('data', data => { errors += data; });
      child.on('error', error => { clearTimeout(timer); fail(error); });
      child.on('exit', code => { clearTimeout(timer); fail(new Error('generated server exited ' + code + ': ' + errors)); });
    });
    return { child, url };
  } catch (error) { await stop(child); throw error; }
}

async function stop(child) {
  if (child.exitCode !== null || child.signalCode !== null) return;
  const exited = once(child, 'exit');
  child.kill();
  await exited;
}

try {
  mock = createServer(async (request, response) => {
    const url = new URL(request.url, 'http://localhost');
    response.setHeader('Content-Type', 'application/json');
    if (url.pathname === '/api/failure') { response.writeHead(503); response.end(JSON.stringify({ error: 'temporary failure' })); return; }
    let raw = '';
    for await (const chunk of request) raw += chunk;
    response.end(JSON.stringify({
      method: request.method, id: decodeURIComponent(url.pathname.slice('/api/items/'.length)),
      query: url.searchParams.get('q'), queries: Object.fromEntries([...new Set(url.searchParams.keys())].map(key => [key, url.searchParams.getAll(key)])),
      body: raw ? request.headers['content-type']?.startsWith('application/json') ? JSON.parse(raw) : raw : null,
      tenant: request.headers['x-tenant'], flags: request.headers['x-flags'], apiKey: request.headers['x-api-key'], cookie: request.headers.cookie,
      contentType: request.headers['content-type'],
      authenticated: request.headers.authorization === 'Bearer qa-rest-token', traceparent: request.headers.traceparent,
    }));
  });
  mock.listen(0, '0.0.0.0');
  await once(mock, 'listening');
  const restURL = 'http://127.0.0.1:' + mock.address().port + '/api';
  const login = await fetch(gateway + '/api/auth/login', {
    method: 'POST', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username: process.env.MCP_USERNAME || 'admin', password: process.env.MCP_PASSWORD || 'admin123' }),
  });
  assert.equal(login.status, 200, 'gateway login failed');
  const { token } = await login.json();
  const schema = { type: 'string' };
  const idParameter = { name: 'id', in: 'path', required: true, schema };
  const spec = {
    openapi: '3.0.3', info: { title: 'Generated "service"\n中文', version: '1.0.0' }, servers: [{ url: restURL }],
    components: {
      schemas: { Payload: { type: 'object', properties: { text: { type: 'string' }, child: { $ref: '#/components/schemas/Payload' } } } },
      parameters: { ID: idParameter },
      requestBodies: { Payload: { required: true, content: { 'application/json': { schema: { $ref: '#/components/schemas/Payload' } } } } },
      responses: { Result: { description: 'OK', content: { 'application/json': { schema: { $ref: '#/components/schemas/Payload' } } } } },
      securitySchemes: { token: { type: 'http', scheme: 'bearer' }, key: { type: 'apiKey', in: 'header', name: 'X-API-Key' }, queryKey: { type: 'apiKey', in: 'query', name: 'secret' }, cookieKey: { type: 'apiKey', in: 'cookie', name: 'sessionKey' } },
    },
    security: [{ token: [] }],
    paths: {
      '/items/{id}': {
        parameters: [{ $ref: '#/components/parameters/ID' }, { name: 'q', in: 'query', required: false, schema }],
        get: { operationId: 'item_read', security: [{ key: [] }, { token: [] }], responses: { '200': { $ref: '#/components/responses/Result' } }, parameters: [
          { name: 'q', in: 'query', required: true, schema }, { name: 'X-Tenant', in: 'header', required: true, schema },
          { name: 'tags', in: 'query', schema: { type: 'array', items: schema } }, { name: 'filter', in: 'query', style: 'deepObject', explode: true, schema: { type: 'object' } },
          { name: 'X-Flags', in: 'header', schema: { type: 'array', items: schema } }, { name: 'session', in: 'cookie', schema },
        ] },
        post: { operationId: 'item_write', requestBody: { $ref: '#/components/requestBodies/Payload' } },
      },
      '/failure': { get: { operationId: 'rest_failure' } },
      '/form': { post: { operationId: 'form_write', security: [], requestBody: { required: true, content: { 'application/x-www-form-urlencoded': { schema: { type: 'object' } } } } } },
      '/text': { put: { operationId: 'text_write', security: [{ queryKey: [], cookieKey: [] }], requestBody: { required: true, content: { 'text/plain': { schema } } } } },
      '/health': { head: { operationId: 'head_check', security: [] }, options: { operationId: 'options_check', security: [] } },
    },
  };
  async function generate(lang) {
    const yaml = Object.entries(spec).map(([key, value]) => key + ': ' + JSON.stringify(value)).join('\n');
    const response = await fetch(gateway + '/api/wrapper/skeleton', {
      method: 'POST', headers: { 'Content-Type': 'application/json', Authorization: 'Bearer ' + token },
      body: JSON.stringify({ lang, spec: lang === 'ts' ? yaml : spec, base_url: restURL }),
    });
    const traceID = response.headers.get('x-trace-id');
    if (traceID && /^[a-f0-9]{32}$/.test(traceID)) auditTraceIDs.push(traceID);
    return { status: response.status, project: await response.json() };
  }
  async function gatewayRequest(path, payload) {
    const response = await fetch(gateway + path, {
      method: 'POST', headers: { 'Content-Type': 'application/json', Authorization: 'Bearer ' + token }, body: JSON.stringify(payload),
    });
    const traceID = response.headers.get('x-trace-id');
    if (traceID && /^[a-f0-9]{32}$/.test(traceID)) auditTraceIDs.push(traceID);
    return { status: response.status, value: await response.json(), traceID };
  }
  const unique = 'openapi_qa_' + Date.now();
  const gatewaySpec = structuredClone(spec);
  gatewaySpec.info.title = unique;
  gatewaySpec.servers = [{ url: 'http://host.docker.internal:' + mock.address().port + '/api' }];
  for (const item of Object.values(gatewaySpec.paths)) {
    for (const method of ['get', 'post', 'put', 'head', 'options']) if (item[method]) item[method].operationId = unique + '_' + item[method].operationId;
  }
  const registered = await gatewayRequest('/api/servers/generate', { spec: gatewaySpec });
  assert.equal(registered.status, 201, JSON.stringify(registered.value));
  assert.ok(Number.isSafeInteger(registered.value.server.id) && registered.value.server.id > 0);
  registeredServerIDs.push(registered.value.server.id);
  const readTool = registered.value.tools.find(tool => tool.name === unique + '_item_read');
  assert.equal(readTool.output_schema.$ref, '#/$defs/Payload');
  assert.equal(readTool.output_schema.$defs.Payload.properties.child.$ref, '#/$defs/Payload');
  const args = { id: '用户 /?#汉', q: 'a&b=中文+', tags: ['a&b', '中文'], filter: { active: true }, headers: { 'X-Tenant': 'tenant', 'X-Flags': ['one', 'two'] }, cookies: { session: '中文 value' }, auth: { key: 'qa-gateway-secret' } };
  const invoked = await gatewayRequest('/api/gateway/invoke', { tool_id: readTool.id, arguments: args });
  assert.equal(invoked.status, 200, JSON.stringify(invoked.value));
  const echoed = JSON.parse(invoked.value.result.content[0].text);
  assert.equal(echoed.id, args.id); assert.equal(echoed.query, args.q); assert.deepEqual(echoed.queries.tags, args.tags);
  assert.equal(echoed.apiKey, args.auth.key); assert.equal(echoed.flags, 'one,two'); assert.equal(echoed.tenant, 'tenant');
  assert.ok(echoed.traceparent.includes(invoked.value.trace_id));
  const mcp = await gatewayRequest('/mcp', { jsonrpc: '2.0', id: 1, method: 'tools/call', params: { name: readTool.name, arguments: args } });
  assert.equal(mcp.status, 200, JSON.stringify(mcp.value)); assert.ok(!mcp.value.result.isError);
  assert.equal(JSON.parse(mcp.value.result.content[0].text).apiKey, args.auth.key);
  const invalidSpec = structuredClone(gatewaySpec);
  invalidSpec.info.title += '_invalid';
  invalidSpec.paths['/form'].post.requestBody.content = { 'multipart/form-data': { schema: { type: 'object' } } };
  assert.equal((await gatewayRequest('/api/servers/generate', { spec: invalidSpec })).status, 400);
  await run('docker', ['compose', 'restart', 'gateway'], root);
  let ready = false;
  for (let attempt = 0; attempt < 60; attempt++) {
    try { if ((await fetch(gateway + '/health', { signal: AbortSignal.timeout(1000) })).ok) { ready = true; break; } } catch { /* wait for startup */ }
    await new Promise(done => setTimeout(done, 500));
  }
  assert.ok(ready, 'gateway did not recover after restart');
  const restored = await gatewayRequest('/api/gateway/invoke', { tool_id: readTool.id, arguments: args });
  assert.equal(restored.status, 200, JSON.stringify(restored.value));
  assert.equal(JSON.parse(restored.value.result.content[0].text).apiKey, args.auth.key);
  let summary = '';
  for (let attempt = 0; attempt < 20; attempt++) {
    summary = await run('docker', ['compose', 'exec', '-T', 'postgres', 'psql', '-tA', '-v', 'ON_ERROR_STOP=1', '-U', 'mcp', '-d', 'mcp_market'], root,
      "SELECT argument_summary FROM tool_calls WHERE trace_id='" + restored.traceID + "';\n");
    if (summary.includes('[redacted]')) break;
    await new Promise(done => setTimeout(done, 100));
  }
  assert.ok(summary.includes('[redacted]')); assert.ok(!summary.includes(args.auth.key)); assert.ok(!summary.includes('session'));
  gatewayChecks.push('registration without base_url', 'response/recursive schemas persisted', 'REST invocation', 'MCP invocation', 'unsupported media rejected', 'bindings restored after gateway restart', 'credential summary redaction');
  console.log('gateway: registration, real REST/MCP calls, restart and redaction checks passed');
  assert.equal((await generate('python')).status, 400, 'unsupported language was silently accepted');
  for (const lang of ['go', 'ts']) {
    const { status, project } = await generate(lang);
    assert.equal(status, 200, JSON.stringify(project));
    assert.equal(project.lang, lang);
    const directory = join(temporary, lang);
    await mkdir(directory);
    for (const [name, contents] of Object.entries(project.files)) {
      assert.ok(name === 'main.go' || name === 'go.mod' || name === 'server.ts' || name === 'package.json' || name === 'tsconfig.json');
      await writeFile(join(directory, name), contents, 'utf8');
    }
    let command, args;
    if (lang === 'go') {
      const filename = process.platform === 'win32' ? 'mcp-server.exe' : 'mcp-server';
      await run('go', ['build', '-o', filename, '.'], directory);
      command = join(directory, filename); args = [];
    } else {
      await npm(['install', '--no-audit', '--no-fund'], directory);
      await npm(['run', 'build'], directory);
      command = process.execPath; args = ['dist/server.js'];
    }
    const running = await start(command, args, directory, restURL);
    try {
      const headers = { 'Content-Type': 'application/json', Accept: 'application/json, text/event-stream', Authorization: 'Bearer qa-mcp-token', traceparent: '00-1234567890abcdef1234567890abcdef-1234567890abcdef-01' };
      async function rpc(method, params = {}, id = 1) {
        const response = await fetch(running.url, { method: 'POST', headers, body: JSON.stringify({ jsonrpc: '2.0', id, method, params }) });
        assert.equal(response.status, 200);
        const value = await response.json();
        assert.equal(value.jsonrpc, '2.0'); assert.equal(value.id, id);
        return value;
      }
      assert.equal((await rpc('initialize')).result.serverInfo.name, spec.info.title);
      const listed = (await rpc('tools/list')).result.tools;
      assert.deepEqual(listed.map(tool => tool.name), ['form_write', 'head_check', 'item_read', 'item_write', 'options_check', 'rest_failure', 'text_write']);
      assert.equal(listed.find(tool => tool.name === 'item_write').inputSchema.properties.body.$ref, '#/$defs/Payload');
      assert.equal(listed.find(tool => tool.name === 'item_read').inputSchema.$defs.Payload.properties.child.$ref, '#/$defs/Payload');
      assert.deepEqual((await rpc('ping')).result, {});
      const notification = await fetch(running.url, { method: 'POST', headers, body: JSON.stringify({ jsonrpc: '2.0', method: 'notifications/initialized' }) });
      assert.equal(notification.status, 202); assert.equal(await notification.text(), '');
      const id = '用户 /?#汉';
      const query = 'a&b=中文+';
      const readArgs = { id, q: query, tags: ['a&b', '中文'], filter: { active: true, name: 'x+y' }, headers: { 'X-Tenant': 'tenant', 'X-Flags': ['one', 'two'] }, cookies: { session: '中文 value' }, auth: { key: 'qa-key' } };
      const read = await rpc('tools/call', { name: 'item_read', arguments: readArgs }, 'read-id');
      assert.ok(!read.result.isError);
      const echoed = JSON.parse(read.result.content[0].text);
      assert.equal(echoed.id, id); assert.equal(echoed.query, query); assert.equal(echoed.method, 'GET');
      assert.equal(echoed.authenticated, true); assert.equal(echoed.traceparent, headers.traceparent);
      assert.deepEqual(echoed.queries.tags, readArgs.tags); assert.equal(echoed.queries['filter[active]'][0], 'true');
      assert.equal(echoed.tenant, 'tenant'); assert.equal(echoed.flags, 'one,two'); assert.equal(echoed.apiKey, 'qa-key');
      assert.ok(echoed.cookie.includes('session=%E4%B8%AD%E6%96%87+value'));
      const bearer = await rpc('tools/call', { name: 'item_read', arguments: { ...readArgs, auth: { token: 'qa-rest-token' } } });
      assert.equal(JSON.parse(bearer.result.content[0].text).apiKey, undefined);
      assert.equal((await rpc('tools/call', { name: 'item_read', arguments: { ...readArgs, headers: { 'X-Flags': ['one'] } } })).result.isError, true);
      const body = { text: '真实请求体', quoted: '"value"' };
      const write = await rpc('tools/call', { name: 'item_write', arguments: { id, body } });
      const written = JSON.parse(write.result.content[0].text);
      assert.deepEqual(written.body, body); assert.equal(written.method, 'POST');
      const form = await rpc('tools/call', { name: 'form_write', arguments: { body: { tags: ['a&b', '中文'], enabled: true } } });
      const formEcho = JSON.parse(form.result.content[0].text);
      assert.equal(formEcho.contentType, 'application/x-www-form-urlencoded'); assert.deepEqual(new URLSearchParams(formEcho.body).getAll('tags'), ['a&b', '中文']);
      const text = await rpc('tools/call', { name: 'text_write', arguments: { body: 'plain 中文', auth: { queryKey: 'key+value', cookieKey: 'cookie value' } } });
      const textEcho = JSON.parse(text.result.content[0].text);
      assert.equal(textEcho.contentType, 'text/plain'); assert.equal(textEcho.body, 'plain 中文');
      assert.equal(textEcho.queries.secret[0], 'key+value'); assert.ok(textEcho.cookie.includes('sessionKey=cookie+value'));
      assert.equal((await rpc('tools/call', { name: 'text_write', arguments: { body: 'text', auth: { queryKey: 'only-one' } } })).result.isError, true);
      for (const name of ['head_check', 'options_check']) assert.ok(!(await rpc('tools/call', { name, arguments: {} })).result.isError);
      assert.equal((await rpc('tools/call', { name: 'rest_failure', arguments: {} })).result.isError, true);
      assert.equal((await rpc('tools/call', { name: 'missing', arguments: {} })).error.code, -32602);
      assert.equal((await rpc('tools/call', { name: 'item_read', arguments: { id } })).error.code, -32602);
      assert.equal((await rpc('missing_method')).error.code, -32601);
      assert.equal((await fetch(running.url, { headers })).status, 405);
      assert.equal((await fetch(running.url, { method: 'POST', headers, body: 'invalid-json' })).status, 400);
      assert.equal((await fetch(running.url, { method: 'POST', headers: { ...headers, Origin: 'https://evil.example' }, body: '{}' })).status, 403);
      assert.equal((await fetch(running.url, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{}' })).status, 401);
      results.push({ lang, compiled: true, started: true, passed: true, checks: ['initialize', 'tools/list', 'ping', 'notification', 'component/recursive references', 'common parameter override', 'path/query escaping', 'query arrays/deepObject', 'header arrays/cookie', 'JSON/form/text body', 'bearer/API key OR/AND security', 'HEAD/OPTIONS', 'traceparent forwarding', 'REST 503', 'unknown tool/method', 'nested required arguments', 'invalid JSON', 'GET 405', 'Origin 403', 'authentication 401'] });
      console.log(lang + ': compile, startup and protocol/REST checks passed');
    } finally { await stop(running.child); }
  }
  const evidence = { verified_at: new Date().toISOString(), generated_projects: results, gateway: { passed: true, checks: gatewayChecks }, unsupported_language_status: 400 };
  await writeFile(join(root, 'docs', '骨架代码联调结果.json'), JSON.stringify(evidence, null, 2) + '\n', 'utf8');
  await writeFile(join(root, 'docs', 'OpenAPI兼容性联调结果.json'), JSON.stringify(evidence, null, 2) + '\n', 'utf8');
} finally {
  try {
    if (mock) { mock.closeAllConnections(); mock.close(); }
    if (auditTraceIDs.length || registeredServerIDs.length) {
      const values = auditTraceIDs.map(id => "'" + id + "'").join(',');
      const ids = registeredServerIDs.join(',');
      const cleanup = [
        values ? 'DELETE FROM tool_calls WHERE trace_id IN (' + values + ');' : '',
        values ? 'DELETE FROM audit_logs WHERE trace_id IN (' + values + ');' : '',
        ids ? 'DELETE FROM tool_calls WHERE tool_id IN (SELECT id FROM mcp_tools WHERE server_id IN (' + ids + '));' : '',
        ids ? 'DELETE FROM mcp_servers WHERE id IN (' + ids + ');' : '',
      ].filter(Boolean).join('\n');
      await run('docker', ['compose', 'exec', '-T', 'postgres', 'psql', '-q', '-v', 'ON_ERROR_STOP=1', '-U', 'mcp', '-d', 'mcp_market'], root, 'BEGIN;\n' + cleanup + '\nCOMMIT;\n');
    }
  } finally {
    const target = resolve(temporary);
    const allowedRoot = resolve(tmpdir()) + sep;
    if (!target.startsWith(allowedRoot) || !target.includes('project32-skeleton-')) throw new Error('unsafe temporary cleanup path');
    await rm(target, { recursive: true, force: true });
  }
}
