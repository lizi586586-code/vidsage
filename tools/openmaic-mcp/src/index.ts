import { randomUUID } from 'node:crypto';
import { mkdir, readFile, writeFile, rename } from 'node:fs/promises';
import { dirname } from 'node:path';
import { ClassroomWorkflow } from './classroom-workflow.js';
import { updateCookies } from './service-cookies.js';
import { AccountBoundRemote } from './service-account.js';

import express, { type Request, type Response } from 'express';
import { Server } from '@modelcontextprotocol/sdk/server/index.js';
import { StreamableHTTPServerTransport } from '@modelcontextprotocol/sdk/server/streamableHttp.js';
import {
  CallToolRequestSchema,
  ListToolsRequestSchema,
  type Tool,
} from '@modelcontextprotocol/sdk/types.js';

const MAX_MATERIAL_BYTES = 30 * 1024 * 1024;
const MAX_REQUIREMENT_LENGTH = 8_000;
const MAX_MATERIAL_IDS = 5;
const ALLOWED_MIME_TYPES = new Set([
  'application/pdf',
  'text/plain',
  'text/markdown',
  'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
]);
const ID_PATTERN = /^[A-Za-z0-9_-]{1,128}$/;
const EMAIL_PATTERN = /\b[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}\b/i;

const port = parsePort(process.env.PORT, 3000);
const baseUrl = resolveBaseUrl(process.env.OPENMAIC_BASE_URL ?? 'https://open.maic.chat');
const accessCode = process.env.OPENMAIC_ACCESS_CODE?.trim() ?? '';

const cookieJar = new Map<string, string>();
const cookiePath = `${process.env.OPENMAIC_JOB_JOURNAL ?? '/data/openmaic-jobs.json'}.identity`;
const identityReady = readFile(cookiePath, 'utf8').then(raw => {
  const entries: unknown = JSON.parse(raw);
  if (!Array.isArray(entries) || entries.some(entry => !Array.isArray(entry) || entry.length !== 2 || entry.some(value => typeof value !== 'string' || /[\r\n;]/.test(value)))) throw new Error('invalid_service_identity');
  for (const [name, value] of entries) cookieJar.set(name, value);
}).catch(error => { if (error.code !== 'ENOENT') throw new Error('service_identity_unavailable'); });
let identityWrites = Promise.resolve();
let accessVerified = false;

function parsePort(raw: string | undefined, fallback: number): number {
  if (!raw) return fallback;
  const parsed = Number(raw);
  return Number.isInteger(parsed) && parsed > 0 && parsed < 65_536 ? parsed : fallback;
}

function resolveBaseUrl(raw: string): URL {
  let parsed: URL;
  try {
    parsed = new URL(raw);
  } catch {
    throw new Error('OPENMAIC_BASE_URL must be a valid HTTPS URL');
  }
  if (parsed.protocol !== 'https:' || parsed.hostname !== 'open.maic.chat' || parsed.pathname !== '/') {
    throw new Error('OPENMAIC_BASE_URL must be exactly https://open.maic.chat');
  }
  return parsed;
}

function serviceUrl(path: string): URL {
  return new URL(path, baseUrl);
}

function cookieHeader(): string | undefined {
  if (cookieJar.size === 0) return undefined;
  return [...cookieJar.entries()].map(([name, value]) => `${name}=${value}`).join('; ');
}

function rememberCookies(response: globalThis.Response): void {
  updateCookies(cookieJar, response.headers);
}

async function serviceFetch(path: string, init: RequestInit = {}): Promise<globalThis.Response> {
  await identityReady;
  const headers = new Headers(init.headers);
  const cookie = cookieHeader();
  if (cookie) headers.set('cookie', cookie);
  const response = await fetch(serviceUrl(path), {
    ...init,
    headers,
    redirect: 'error',
    signal: AbortSignal.timeout(45_000),
  });
  rememberCookies(response);
  identityWrites = identityWrites.then(async () => {
    await mkdir(dirname(cookiePath), {recursive:true, mode:0o700});
    await writeFile(`${cookiePath}.tmp`, JSON.stringify([...cookieJar]), {mode:0o600});
    await rename(`${cookiePath}.tmp`, cookiePath);
  });
  await identityWrites;
  return response;
}

const accountRemote = new AccountBoundRemote(serviceFetch, async () => {
  await identityReady;
  if (!cookieJar.get('auth_token')) throw new Error('service_account_login_required');
  try {
    const account = JSON.parse(await readFile(`${cookiePath.replace(/\.identity$/, '')}.account`, 'utf8'));
    return typeof account.subject_key === 'string' ? account.subject_key : '';
  } catch { throw new Error('service_account_not_configured'); }
});
const openmaicFetch = (path: string, init?: RequestInit) => accountRemote.fetch(path, init);

async function ensureAccess(): Promise<void> {
  if (accessVerified || !accessCode) return;
  const response = await openmaicFetch('/api/access-code/verify', {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ code: accessCode }),
  });
  if (!response.ok) {
    throw new Error('OpenMAIC access-code verification failed. Check OPENMAIC_ACCESS_CODE in the local deployment environment.');
  }
  accessVerified = true;
}

async function readApiResponse(response: globalThis.Response): Promise<Record<string, unknown>> {
  const contentType = response.headers.get('content-type') ?? '';
  const body = contentType.includes('application/json')
    ? await response.json().catch(() => null)
    : await response.text().catch(() => '');
  const safeBody = typeof body === 'object' && body !== null
    ? body as Record<string, unknown>
    : { message: String(body).slice(0, 500) };
  if (!response.ok) {
    const detail = typeof safeBody.error === 'string' ? safeBody.error : `HTTP ${response.status}`;
    throw new Error(`OpenMAIC request failed: ${detail}`);
  }
  return safeBody;
}

function textResult(value: Record<string, unknown>) {
  return { content: [{ type: 'text' as const, text: JSON.stringify(value) }] };
}

function errorResult(error: unknown) {
  const raw = error instanceof Error ? error.message : '';
  const message = /^[a-z0-9_]{1,64}$/.test(raw) ? raw : 'openmaic_request_failed';
  console.error(JSON.stringify({ event: 'openmaic_tool_failed', message }));
  return { content: [{ type: 'text' as const, text: JSON.stringify({ success: false, error: message }) }], isError: true };
}

function requireObject(args: unknown): Record<string, unknown> {
  if (!args || typeof args !== 'object' || Array.isArray(args)) throw new Error('Arguments must be an object.');
  return args as Record<string, unknown>;
}

function requireString(args: Record<string, unknown>, field: string, maxLength: number): string {
  const value = args[field];
  if (typeof value !== 'string') throw new Error(`${field} must be a string.`);
  const trimmed = value.trim();
  if (!trimmed || trimmed.length > maxLength) throw new Error(`${field} must contain 1-${maxLength} characters.`);
  return trimmed;
}

function validateLearnerSafeRequirement(requirement: string): void {
  if (EMAIL_PATTERN.test(requirement)) {
    throw new Error('requirement must use a learning preference summary and must not include an email address.');
  }
}

function decodeBase64(value: string): Buffer {
  if (!/^[A-Za-z0-9+/]+={0,2}$/.test(value) || value.length % 4 !== 0) {
    throw new Error('content_base64 must be valid standard Base64 data.');
  }
  const decoded = Buffer.from(value, 'base64');
  if (decoded.length === 0 || decoded.length > MAX_MATERIAL_BYTES) {
    throw new Error(`Decoded material must be between 1 byte and ${MAX_MATERIAL_BYTES} bytes.`);
  }
  return decoded;
}

const tools: Tool[] = [
  {
    name: 'openmaic_start_learning_course',
    description: 'Claim one user-authorized WeKnora course request. Reads prepared original materials and learner preferences; creates the course asynchronously and publishes a verified URL automatically. Learning completion is unavailable until hosted learner authorization is configured.',
    inputSchema: {type:'object',properties:{request_id:{type:'string',description:'Exact request ID from the authenticated reinforcement action.'}},required:['request_id'],additionalProperties:false},
    annotations:{readOnlyHint:false,destructiveHint:false,idempotentHint:true,openWorldHint:true},
  },
  {
    name: 'openmaic_get_health',
    description: 'Read the fixed OpenMAIC hosted service health and available capabilities.',
    inputSchema: { type: 'object', properties: {}, additionalProperties: false },
  },
  {
    name: 'openmaic_upload_material',
    description: 'Upload a PDF or approved text document to the fixed OpenMAIC service for classroom generation.',
    inputSchema: {
      type: 'object',
      properties: {
        file_name: { type: 'string', description: 'Original file name, 1-255 characters.' },
        mime_type: { type: 'string', enum: [...ALLOWED_MIME_TYPES] },
        content_base64: { type: 'string', description: 'Standard Base64 encoded file bytes, up to 30 MiB decoded.' },
      },
      required: ['file_name', 'mime_type', 'content_base64'],
      additionalProperties: false,
    },
  },
  {
    name: 'openmaic_get_material_status',
    description: 'Read the extraction status of one previously uploaded OpenMAIC material.',
    inputSchema: {
      type: 'object',
      properties: { material_id: { type: 'string', description: 'Material ID returned by openmaic_upload_material.' } },
      required: ['material_id'],
      additionalProperties: false,
    },
  },
  {
    name: 'openmaic_create_classroom',
    description: 'Create an asynchronous OpenMAIC interactive classroom from a teaching requirement and ready material IDs.',
    inputSchema: {
      type: 'object',
      properties: {
        requirement: { type: 'string', description: 'Teaching requirement and learner-safe preference summary, 1-8000 characters. Do not include emails or credentials.' },
        material_ids: { type: 'array', items: { type: 'string' }, maxItems: MAX_MATERIAL_IDS, description: 'Up to five ready material IDs.' },
      },
      required: ['requirement'],
      additionalProperties: false,
    },
  },
  {
    name: 'openmaic_get_generation_status',
    description: 'Read the status and classroom URL of one OpenMAIC generation job.',
    inputSchema: {
      type: 'object',
      properties: { job_id: { type: 'string', description: 'Job ID returned by openmaic_create_classroom.' } },
      required: ['job_id'],
      additionalProperties: false,
    },
  },
];

function buildServer(): Server {
  const server = new Server({ name: 'openmaic-classroom', version: '0.1.0' }, { capabilities: { tools: {} } });
  server.setRequestHandler(ListToolsRequestSchema, async () => ({ tools }));
  server.setRequestHandler(CallToolRequestSchema, async (request) => {
    try {
      const args = requireObject(request.params.arguments ?? {});
      const definition=tools.find(tool=>tool.name===request.params.name);
      if (!definition || Object.keys(args).some(key=>!(key in (definition.inputSchema.properties ?? {})))) throw new Error('unknown_tool_argument');
      switch (request.params.name) {
        case 'openmaic_start_learning_course': return textResult(await workflow.start(requireString(args,'request_id',128)));
        case 'openmaic_get_health': {
          const response = await openmaicFetch('/api/health');
          return textResult(await readApiResponse(response));
        }
        case 'openmaic_upload_material': {
          await ensureAccess();
          const fileName = requireString(args, 'file_name', 255).replace(/[\\/]/g, '_');
          const mimeType = requireString(args, 'mime_type', 128).toLowerCase();
          if (!ALLOWED_MIME_TYPES.has(mimeType)) throw new Error('mime_type is not allowed for classroom material upload.');
          const content = decodeBase64(requireString(args, 'content_base64', Math.ceil(MAX_MATERIAL_BYTES * 4 / 3) + 4));
          const response = await openmaicFetch('/api/materials', {
            method: 'POST',
            headers: { 'content-type': mimeType, 'x-material-filename': fileName, 'x-request-id': randomUUID() },
            body: content,
          });
          return textResult(await readApiResponse(response));
        }
        case 'openmaic_get_material_status': {
          await ensureAccess();
          const materialId = requireString(args, 'material_id', 128);
          if (!ID_PATTERN.test(materialId)) throw new Error('material_id has an invalid format.');
          const response = await openmaicFetch(`/api/materials/${encodeURIComponent(materialId)}`);
          return textResult(await readApiResponse(response));
        }
        case 'openmaic_create_classroom': {
          await ensureAccess();
          const requirement = requireString(args, 'requirement', MAX_REQUIREMENT_LENGTH);
          validateLearnerSafeRequirement(requirement);
          const rawMaterialIds = args.material_ids;
          let materialIds: string[] | undefined;
          if (rawMaterialIds !== undefined) {
            if (!Array.isArray(rawMaterialIds) || rawMaterialIds.length > MAX_MATERIAL_IDS || !rawMaterialIds.every((id) => typeof id === 'string' && ID_PATTERN.test(id))) {
              throw new Error('material_ids must contain at most five valid material IDs.');
            }
            materialIds = rawMaterialIds;
          }
          const response = await openmaicFetch('/api/generate-classroom', {
            method: 'POST',
            headers: { 'content-type': 'application/json' },
            body: JSON.stringify({ requirement, ...(materialIds ? { materialIds } : {}) }),
          });
          return textResult(await readApiResponse(response));
        }
        case 'openmaic_get_generation_status': {
          await ensureAccess();
          const jobId = requireString(args, 'job_id', 128);
          if (!ID_PATTERN.test(jobId)) throw new Error('job_id has an invalid format.');
          const response = await openmaicFetch(`/api/generate-classroom/${encodeURIComponent(jobId)}`);
          return textResult(await readApiResponse(response));
        }
        default:
          throw new Error('Unknown OpenMAIC tool.');
      }
    } catch (error) {
      return errorResult(error);
    }
  });
  return server;
}

const transports: Record<string, StreamableHTTPServerTransport> = {};
const workflow = new ClassroomWorkflow(openmaicFetch,ensureAccess,process.env.WEKNORA_CUSTOM_BASE_URL ?? '',process.env.OPENMAIC_BRIDGE_SECRET ?? '',process.env.OPENMAIC_JOB_JOURNAL ?? '/data/openmaic-jobs.json');
if (process.env.WEKNORA_CUSTOM_BASE_URL && process.env.OPENMAIC_BRIDGE_SECRET) {
  void workflow.run().catch(()=>console.error(JSON.stringify({event:'classroom_worker_unavailable',code:'job_journal_unavailable'})));
}
const app = express();
app.disable('x-powered-by');
app.use(express.json({ limit: '45mb', type: 'application/json' }));
app.get('/healthz', (_request, response) => response.status(200).json({ status: 'ok' }));

function isInitializeRequest(body: unknown): boolean {
  return typeof body === 'object' && body !== null && (body as { method?: unknown }).method === 'initialize';
}

async function handlePost(request: Request, response: Response): Promise<void> {
  const sessionId = request.header('mcp-session-id');
  try {
    let transport: StreamableHTTPServerTransport | undefined = sessionId ? transports[sessionId] : undefined;
    if (!transport && !sessionId && isInitializeRequest(request.body)) {
      transport = new StreamableHTTPServerTransport({
        sessionIdGenerator: randomUUID,
        onsessioninitialized: (id) => { transports[id] = transport!; },
      });
      transport.onclose = () => {
        const id = transport!.sessionId;
        if (id) delete transports[id];
      };
      await buildServer().connect(transport);
    }
    if (!transport) {
      response.status(sessionId ? 404 : 400).json({ jsonrpc: '2.0', error: { code: sessionId ? -32001 : -32000, message: sessionId ? 'Session not found' : 'A valid MCP session is required.' }, id: null });
      return;
    }
    await transport.handleRequest(request, response, request.body);
  } catch (error) {
    console.error(JSON.stringify({ event: 'mcp_transport_failed', message: 'transport_failed' }));
    if (!response.headersSent) response.status(500).json({ jsonrpc: '2.0', error: { code: -32603, message: 'Internal server error' }, id: null });
  }
}

async function handleSession(request: Request, response: Response): Promise<void> {
  const sessionId = request.header('mcp-session-id');
  const transport = sessionId ? transports[sessionId] : undefined;
  if (!transport) {
    response.status(sessionId ? 404 : 400).send(sessionId ? 'Session not found' : 'A valid MCP session is required.');
    return;
  }
  await transport.handleRequest(request, response, request.method === 'GET' ? undefined : request.body);
}

app.post('/mcp', handlePost);
app.get('/mcp', handleSession);
app.delete('/mcp', handleSession);

app.listen(port, '0.0.0.0', () => {
  console.log(JSON.stringify({ event: 'openmaic_mcp_started', port, baseUrl: baseUrl.origin }));
});
