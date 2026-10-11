// The caller's platform account never selects the hosted generation identity.
export class AccountBoundRemote {
    remote;
    expectedSubject;
    constructor(remote, expectedSubject) {
        this.remote = remote;
        this.expectedSubject = expectedSubject;
    }
    async fetch(path, init) {
        if (path === '/api/materials' || path.startsWith('/api/materials/') ||
            path === '/api/generate-classroom' || path.startsWith('/api/generate-classroom/')) {
            const expected = await this.expectedSubject();
            if (!/^user:[A-Za-z0-9_-]{1,128}$/.test(expected))
                throw new Error('service_account_not_configured');
            let response;
            try {
                response = await this.remote('/api/credits');
            }
            catch {
                throw new Error('service_account_verification_unavailable');
            }
            if (response.status === 401 || response.status === 403)
                throw new Error('service_account_login_required');
            if (!response.ok || !(response.headers.get('content-type') ?? '').includes('application/json'))
                throw new Error('service_account_verification_unavailable');
            const account = await response.json().catch(() => null);
            if (!account || typeof account !== 'object' || Array.isArray(account))
                throw new Error('service_account_verification_unavailable');
            const subject = account;
            if (subject.success === false || typeof subject.subjectKey !== 'string' || !subject.subjectKey.startsWith('user:'))
                throw new Error('service_account_login_required');
            if (subject.subjectKey !== expected)
                throw new Error('service_account_mismatch');
        }
        return this.remote(path, init);
    }
}
