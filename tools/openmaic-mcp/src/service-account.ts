type RemoteFetch = (path: string, init?: RequestInit) => Promise<Response>;

// The caller's platform account never selects the hosted generation identity.
export class AccountBoundRemote {
  constructor(private remote: RemoteFetch, private expectedSubject: () => Promise<string>) {}

  async fetch(path: string, init?: RequestInit): Promise<Response> {
    if (path === '/api/materials' || path.startsWith('/api/materials/') ||
        path === '/api/generate-classroom' || path.startsWith('/api/generate-classroom/')) {
      const expected = await this.expectedSubject();
      if (!/^user:[A-Za-z0-9_-]{1,128}$/.test(expected)) throw new Error('service_account_not_configured');
      let response: Response;
      try { response = await this.remote('/api/credits'); }
      catch { throw new Error('service_account_verification_unavailable'); }
      if (response.status === 401 || response.status === 403) throw new Error('service_account_login_required');
      if (!response.ok || !(response.headers.get('content-type') ?? '').includes('application/json')) throw new Error('service_account_verification_unavailable');
      const account: unknown = await response.json().catch(() => null);
      if (!account || typeof account !== 'object' || Array.isArray(account)) throw new Error('service_account_verification_unavailable');
      const subject = account as Record<string, unknown>;
      if (subject.success === false || typeof subject.subjectKey !== 'string' || !subject.subjectKey.startsWith('user:')) throw new Error('service_account_login_required');
      if (subject.subjectKey !== expected) throw new Error('service_account_mismatch');
    }
    return this.remote(path, init);
  }
}
