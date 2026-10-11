import { mkdir, readFile, rename, writeFile } from 'node:fs/promises';
import { dirname } from 'node:path';
const idPattern = /^[A-Za-z0-9_-]{1,128}$/;
function generationProgress(value) {
    if (value == null)
        return undefined;
    if (typeof value !== 'number' || !Number.isInteger(value) || value < 0 || value > 100)
        throw new Error('invalid_generation_progress');
    return value;
}
export function classroomResult(value) {
    if (!value || typeof value !== 'object')
        throw new Error('invalid_generation_result');
    const result = value;
    if (typeof result.classroomId !== 'string' || !idPattern.test(result.classroomId) || result.classroomId === 'pending' || typeof result.url !== 'string')
        throw new Error('invalid_generation_result');
    const url = new URL(result.url);
    // Only this observed hosted alias maps to the verified public classroom.
    const allowedOrigin = ['https://open.maic.chat', 'http://live-ack-prod.cogevol.com', 'https://live-ack-prod.cogevol.com'].includes(url.origin);
    if (!allowedOrigin || url.username || url.password || url.search || url.hash || url.pathname !== `/classroom/${result.classroomId}`)
        throw new Error('invalid_generation_result');
    return { classroom_id: result.classroomId, classroom_url: `https://open.maic.chat/classroom/${result.classroomId}` };
}
async function json(response) {
    if (!response.ok)
        throw new Error(response.status === 401 || response.status === 403 ? 'remote_access_denied' : 'remote_request_failed');
    if (!(response.headers.get('content-type') ?? '').includes('application/json'))
        throw new Error('invalid_remote_response');
    const value = await response.json();
    if (!value || typeof value !== 'object' || Array.isArray(value))
        throw new Error('invalid_remote_response');
    return value;
}
function materialErrorCode(error) {
    const code = error instanceof Error ? error.message : '';
    return ['remote_access_denied', 'remote_request_failed', 'invalid_remote_response', 'invalid_course_material', 'invalid_material_response', 'material_extraction_failed', 'material_protocol_unavailable', 'legacy_material_unsupported', 'course_source_too_large', 'service_account_not_configured', 'service_account_login_required', 'service_account_mismatch', 'service_account_verification_unavailable'].includes(code) ? code : 'material_preparation_failed';
}
// Durable references let the worker recover after chat ends and bridge restarts.
// Sensitive material bytes and preferences never enter this journal.
export class ClassroomWorkflow {
    remote;
    access;
    backendURL;
    secret;
    journalPath;
    backendFetch;
    jobs = new Map();
    busy = false;
    journalQueue = Promise.resolve();
    initializing;
    timer;
    constructor(remote, access, backendURL, secret, journalPath, backendFetch = fetch) {
        this.remote = remote;
        this.access = access;
        this.backendURL = backendURL;
        this.secret = secret;
        this.journalPath = journalPath;
        this.backendFetch = backendFetch;
        this.initializing = this.load();
    }
    async load() {
        try {
            const raw = JSON.parse(await readFile(this.journalPath, 'utf8'));
            if (!Array.isArray(raw) || raw.some(job => !idPattern.test(job.request_id) || (job.job_id && !idPattern.test(job.job_id)) || !['running', 'succeeded', 'failed'].includes(job.generation_status)))
                throw new Error('invalid_job_journal');
            for (const job of raw) {
                generationProgress(job.progress);
                this.jobs.set(job.request_id, job);
            }
        }
        catch (error) {
            if (error.code !== 'ENOENT')
                throw new Error('job_journal_unavailable');
        }
    }
    async save() {
        this.journalQueue = this.journalQueue.catch(() => undefined).then(async () => {
            await mkdir(dirname(this.journalPath), { recursive: true, mode: 0o700 });
            const temporary = `${this.journalPath}.tmp`;
            await writeFile(temporary, JSON.stringify([...this.jobs.values()]), { mode: 0o600 });
            await rename(temporary, this.journalPath);
        });
        await this.journalQueue;
    }
    async backend(path, init = {}) {
        if (!this.backendURL || !this.secret)
            throw new Error('workflow_not_configured');
        const target = new URL(`/api/custom/learning/openmaic${path}`, this.backendURL);
        return json(await this.backendFetch(target, { ...init, headers: { 'content-type': 'application/json', authorization: `Bearer ${this.secret}` }, redirect: 'error', signal: AbortSignal.timeout(30_000) }));
    }
    async sync(job) {
        await this.backend('/status', { method: 'POST', body: JSON.stringify({ ...job, created_at: undefined, status: 'unknown', error_code: job.error_code ?? '' }) });
    }
    async start(requestID) {
        if (!idPattern.test(requestID))
            throw new Error('invalid_request_id');
        await this.initializing;
        if (this.jobs.has(requestID))
            return { success: true, request_id: requestID, status: this.jobs.get(requestID).generation_status };
        // Claim before any OpenMAIC side effect. A second invocation fails closed.
        const claim = await this.backend(`/${encodeURIComponent(requestID)}/claim`, { method: 'POST', body: '{}' });
        const context = claim.data;
        if (!context?.memory_read || typeof context.requirement !== 'string' || context.requirement.length > 8000 || !Array.isArray(context.materials) || !context.materials.length || context.materials.length > 5)
            throw new Error('invalid_course_context');
        const job = { request_id: requestID, job_id: '', generation_status: 'running', progress: 0, created_at: new Date().toISOString() };
        this.jobs.set(requestID, job);
        // Empty job ID records a claimed task whose creation outcome is unknown.
        await this.save();
        void this.create(job, context).catch(() => console.error(JSON.stringify({ event: 'classroom_creation_failed', code: 'workflow_failed' })));
        return { success: true, request_id: requestID, status: 'queued', memory_read: true, memory_count: context.memory_count, material_count: context.materials.length };
    }
    async create(job, context) {
        let creating = false;
        try {
            await this.access();
            // Older hosted jobs consume extracted text, and silently ignore materialIds.
            // Only a missing capabilities route selects that protocol; access/network
            // failures must never trigger a downgrade or a second creation request.
            const capabilities = await this.remote('/api/generate-classroom/capabilities');
            let legacy = false;
            if (capabilities.status === 404) {
                // A deliberately invalid, source-free request cannot create a job.
                // Confirm the observed legacy parser instead of guessing from a 404.
                const probe = await this.remote('/api/generate-classroom', { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ requirement: '', pdfContent: '' }) });
                if (probe.status !== 400 || !(probe.headers.get('content-type') ?? '').includes('application/json'))
                    throw new Error('material_protocol_unavailable');
                const parser = await probe.json();
                if (parser.errorCode !== 'INVALID_REQUEST' || parser.error !== 'Invalid pdfContent: expected { text: string; images: string[] }')
                    throw new Error('material_protocol_unavailable');
                legacy = true;
            }
            else {
                const supported = await json(capabilities);
                if (!supported.materials || typeof supported.materials !== 'object')
                    throw new Error('material_protocol_unavailable');
            }
            const materialIds = [];
            const texts = [];
            for (const material of context.materials) {
                const bytes = Buffer.from(material.content_base64, 'base64');
                if (!bytes.length || bytes.length > 30 * 1024 * 1024 || !['application/pdf', 'text/plain'].includes(material.mime_type))
                    throw new Error('invalid_course_material');
                if (legacy) {
                    if (material.mime_type !== 'text/plain')
                        throw new Error('legacy_material_unsupported');
                    const text = new TextDecoder('utf-8', { fatal: true }).decode(bytes);
                    if (!text.trim())
                        throw new Error('invalid_course_material');
                    texts.push(text);
                    continue;
                }
                const upload = await json(await this.remote('/api/materials', { method: 'POST', headers: { 'content-type': material.mime_type, 'x-material-filename': material.file_name }, body: bytes }));
                if (typeof upload.materialId !== 'string' || !idPattern.test(upload.materialId))
                    throw new Error('invalid_material_response');
                // Generation owns extraction, including starting idle uploads. The
                // hosted upload endpoint works even when its optional GET route is absent.
                const extraction = upload.extraction?.status;
                if (extraction === 'failed')
                    throw new Error('material_extraction_failed');
                if (extraction !== undefined && !['idle', 'extracting', 'queued', 'running', 'ready'].includes(String(extraction)))
                    throw new Error('invalid_material_response');
                materialIds.push(upload.materialId);
            }
            const text = texts.join('\n\n');
            // Refuse oversized source data rather than silently truncating a video.
            if (legacy && Buffer.byteLength(text, 'utf8') > 1024 * 1024)
                throw new Error('course_source_too_large');
            creating = true;
            const generation = await json(await this.remote('/api/generate-classroom', { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ requirement: context.requirement, ...(legacy ? { pdfContent: { text, images: [] } } : { materialIds }) }) }));
            if (typeof generation.jobId !== 'string' || !idPattern.test(generation.jobId))
                throw new Error('invalid_generation_response');
            job.job_id = generation.jobId;
            await this.save(); // Persist remote ID before publishing locally.
            await this.sync(job);
        }
        catch (error) {
            // A timeout after POST may have created a remote job. Never duplicate it.
            if (!job.job_id) {
                job.generation_status = 'failed';
                const code = materialErrorCode(error);
                job.error_code = creating && !code.startsWith('service_account_') ? 'generation_outcome_unknown' : code;
                await this.save();
            }
            try {
                await this.sync(job);
            }
            catch { /* Durable journal retries the local publication. */ }
        }
    }
    async reconcile() {
        if (this.busy)
            return;
        this.busy = true;
        try {
            await this.initializing;
            const pending = await this.backend('/pending');
            if (!Array.isArray(pending.data))
                throw new Error('invalid_pending_response');
            for (const row of pending.data) {
                if (!idPattern.test(row.request_id))
                    continue;
                if (!this.jobs.has(row.request_id) && row.job_id && idPattern.test(row.job_id))
                    this.jobs.set(row.request_id, { request_id: row.request_id, job_id: row.job_id, generation_status: 'running', progress: generationProgress(row.progress), classroom_id: row.classroom_id, classroom_url: row.classroom_url, created_at: row.created_at });
            }
            for (const job of [...this.jobs.values()]) {
                try {
                    if (job.generation_status === 'running' && job.job_id) {
                        const expired = !Number.isFinite(Date.parse(job.created_at)) || Date.now() - Date.parse(job.created_at) > 60 * 60_000;
                        let result;
                        try {
                            await this.access();
                            result = await json(await this.remote(`/api/generate-classroom/${encodeURIComponent(job.job_id)}`));
                        }
                        catch (error) {
                            if (!expired)
                                throw error;
                            job.generation_status = 'failed';
                            job.error_code = 'generation_timeout_unverified';
                            await this.save();
                            await this.sync(job);
                            this.jobs.delete(job.request_id);
                            await this.save();
                            continue;
                        }
                        const progress = generationProgress(result.progress);
                        if (result.status === 'succeeded') {
                            Object.assign(job, classroomResult(result.result));
                            job.generation_status = 'succeeded';
                            job.progress = 100;
                        }
                        else if (result.status === 'failed') {
                            job.generation_status = 'failed';
                            job.error_code = 'generation_failed';
                        }
                        else if (!['queued', 'running'].includes(String(result.status)))
                            throw new Error('invalid_generation_response');
                        else if (expired) {
                            job.generation_status = 'failed';
                            job.error_code = 'generation_timeout_confirmed';
                        }
                        if (job.generation_status !== 'succeeded' && progress !== undefined)
                            job.progress = progress;
                        await this.save();
                    }
                    if (job.job_id || job.generation_status === 'failed')
                        await this.sync(job);
                    // Published jobs no longer require a local journal entry. The business
                    // database remains the source of truth for future learning integration.
                    if (job.generation_status !== 'running') {
                        this.jobs.delete(job.request_id);
                        await this.save();
                    }
                }
                catch {
                    console.error(JSON.stringify({ event: 'classroom_reconcile_failed', code: 'reconcile_retry_pending' }));
                }
            }
        }
        finally {
            this.busy = false;
        }
    }
    async run() {
        await this.initializing;
        // Claimed work without a persisted remote ID cannot safely be resubmitted.
        for (const job of this.jobs.values())
            if (!job.job_id) {
                job.generation_status = 'failed';
                job.error_code = 'generation_outcome_unknown';
            }
        await this.save();
        this.timer = setInterval(() => { void this.reconcile().catch(() => console.error(JSON.stringify({ event: 'classroom_worker_failed', code: 'backend_unavailable' }))); }, 15_000);
        this.timer.unref();
        void this.reconcile().catch(() => undefined);
    }
    stop() { if (this.timer)
        clearInterval(this.timer); }
}
