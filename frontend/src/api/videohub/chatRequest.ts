export type ChatScope = 'global' | 'video'

export interface ChatRequestScope {
  scope: ChatScope
  agent_id?: string
  agent_enabled?: boolean
  auto_route?: boolean
  agent_source_tenant_id?: string
  knowledge_base_ids: string[]
  knowledge_ids: string[]
  tenant_id?: string | number
}

/** Automatic routing is enabled only when the user has not selected an agent. */
export function shouldAutoRoute(agentId?: string, agentExplicit = false): boolean {
  return !agentExplicit && !String(agentId || '').trim()
}

/** Remove the server's configured fallback when the user chose auto-routing. */
export function resolveChatScopeAgent<T extends {
  agent_id?: string
  agent_source_tenant_id?: string
}>(
  scope: T,
  options: { agentId?: string; agentSourceTenantId?: string | null; autoRoute?: boolean },
): T {
  if (!options.autoRoute || options.agentId || options.agentSourceTenantId) return scope
  const { agent_id: _agentId, agent_source_tenant_id: _sourceTenantId, ...autoScope } = scope
  return autoScope as T
}

export function normalizeTenantId(value?: string | number | null): string {
  return value === undefined || value === null ? '' : String(value).trim()
}

export function buildChatRequest(
  scope: ChatRequestScope,
  query: string,
  token: string,
  fallbackTenantId?: string | number | null,
) {
  const tenantId = normalizeTenantId(scope.tenant_id) || normalizeTenantId(fallbackTenantId)
  const sourceTenantId = normalizeTenantId(scope.agent_source_tenant_id)
  const numericSourceTenantId = sourceTenantId && /^\d+$/.test(sourceTenantId)
    ? Number(sourceTenantId)
    : undefined
  return {
    headers: {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${token}`,
      ...(tenantId ? { 'X-Tenant-ID': tenantId } : {}),
    },
    body: {
      query,
      knowledge_base_ids: scope.knowledge_base_ids,
      // Keep both wiki source_refs and transcript fallback scoped to the
      // current video's active transcript generation.
      ...(scope.scope === 'video' || !scope.agent_id ? { knowledge_ids: scope.knowledge_ids } : {}),
      agent_enabled: scope.agent_enabled ?? Boolean(scope.agent_id),
      auto_route: scope.auto_route ?? false,
      ...(scope.agent_id ? { agent_id: scope.agent_id } : {}),
      ...(numericSourceTenantId ? { agent_source_tenant_id: numericSourceTenantId } : {}),
      disable_title: true,
      channel: 'web',
    },
  }
}

export function normalizeChatError(error: unknown): Error {
  const payload = error && typeof error === 'object' ? error as {
    status?: number
    message?: string
    error?: string | { message?: string }
  } : {}
  const status = error instanceof Error
    ? (error as Error & { status?: number }).status
    : payload.status
  if (status === 403) {
    return new Error('当前账号没有访问视频知识库所属工作空间的权限，请联系管理员加入该工作空间')
  }
  if (error instanceof Error) return error

  const message = typeof payload.error === 'string'
    ? payload.error
    : payload.error?.message || payload.message
  return new Error(message || '问答生成失败，请稍后重试')
}
