// One-time, loopback-only login handoff. Stages credentials; never activates them.
import { createServer } from 'node:http';
import { randomBytes } from 'node:crypto';
import { spawn } from 'node:child_process';

const port = 18765;
const nonce = randomBytes(24).toString('hex');
const callbackPath = `/openmaic/account-callback/${nonce}`;
const callback = `http://127.0.0.1:${port}${callbackPath}`;
const login = `https://open.maic.chat/api/auth/login?from=${encodeURIComponent(callback)}`;
let busy = false;
let complete = false;
let status = '等待账号登录。';

// Authentication is performed by the official login page. The browser returns
// its newly issued token directly to this callback; no browser storage is read.
const verifier = String.raw`
const { writeFile } = await import('node:fs/promises');
const { updateCookies } = await import('./dist/service-cookies.js');
let raw = '';
for await (const chunk of process.stdin) raw += chunk;
try {
  const { token } = JSON.parse(raw);
  if (typeof token !== 'string' || token.length > 4096 ||
      !/^[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$/.test(token)) throw Error();
  const jar = new Map([['auth_token', encodeURIComponent(token)]]);
  const response = await fetch('https://open.maic.chat/api/credits', {
    headers: {cookie: 'auth_token=' + encodeURIComponent(token)},
    redirect: 'error', signal: AbortSignal.timeout(15000),
  });
  const credits = await response.json();
  if (!response.ok || credits.success === false ||
      typeof credits.subjectKey !== 'string' || !credits.subjectKey.startsWith('user:')) throw Error();
  updateCookies(jar, response.headers);
  const path = process.env.OPENMAIC_JOB_JOURNAL + '.identity.account-pending';
  // Exclusive creation protects a previous staged account from being overwritten.
  await writeFile(path, JSON.stringify([...jar]), {flag: 'wx', mode: 0o600});
  console.log(JSON.stringify({success: true, authenticated: true,
    freeRemaining: credits.freeRemaining, paidBalance: credits.paidBalance,
    gateBalance: credits.gateBalance}));
} catch {
  console.log(JSON.stringify({success: false, error: 'account_verification_or_staging_failed'}));
  process.exitCode = 1;
}
`;

function verify(token) {
  return new Promise((resolve, reject) => {
    const child = spawn('docker', ['exec', '-i', 'vidsage-openmaic-mcp',
      'node', '--input-type=module', '-e', verifier], {stdio: ['pipe', 'pipe', 'pipe']});
    let output = '';
    child.stdout.on('data', chunk => { output += chunk; });
    // Never forward remote response bodies, exception messages, or credentials.
    child.stderr.resume();
    child.on('error', () => reject(new Error('verification_failed')));
    child.on('close', code => {
      try {
        const result = JSON.parse(output);
        if (code !== 0 || !result.success) throw new Error();
        resolve(result);
      } catch { reject(new Error('verification_failed')); }
    });
    child.stdin.on('error', () => {});
    child.stdin.end(JSON.stringify({token}));
  });
}

const server = createServer(async (request, response) => {
  response.setHeader('Cache-Control', 'no-store');
  response.setHeader('Referrer-Policy', 'no-referrer');
  response.setHeader('Content-Security-Policy', "default-src 'none'; frame-ancestors 'none'");
  if (request.headers.host !== `127.0.0.1:${port}` || request.method !== 'GET' ||
      (request.url?.length ?? 0) > 8192) {
    response.writeHead(400).end(); return;
  }
  const url = new URL(request.url, callback);
  if (url.pathname === '/status') {
    response.setHeader('Content-Type', 'text/html; charset=utf-8');
    response.end(`<!doctype html><html lang="zh-CN"><meta charset="utf-8"><title>OpenMAIC 账号连接</title><h1>OpenMAIC 账号连接</h1><p>${status}</p></html>`);
    return;
  }
  if (url.pathname !== callbackPath || complete || busy) {
    response.writeHead(404).end(); return;
  }
  // MAIC appends ?token= even when the callback already has auth_cb=1.
  const token = url.searchParams.get('token') ??
    url.searchParams.get('auth_cb')?.match(/^1\?token=(.+)$/)?.[1];
  if (!token) { response.writeHead(400).end(); return; }
  busy = true;
  try {
    const result = await verify(token);
    complete = true;
    status = `登录账号验证成功。每日额度：${Number(result.freeRemaining) || 0}，额外额度：${Number(result.paidBalance) || 0}，访问额度：${Number(result.gateBalance) || 0}。账号连接已准备，等待后台切换生效。`;
    console.log(JSON.stringify({event: 'account_staged', ...result}));
  } catch {
    status = '账号验证未通过，后台账号保持原状。';
    console.log(JSON.stringify({event: 'account_staging_failed'}));
  } finally { busy = false; }
  // Strip the token from the URL before any visible page is returned.
  response.writeHead(303, {Location: '/status'}).end();
});
server.requestTimeout = 30000;
server.listen(port, '127.0.0.1', () => console.log(JSON.stringify({event: 'login_ready', login})));
setTimeout(() => server.close(() => process.exit(0)), 30 * 60 * 1000).unref();
