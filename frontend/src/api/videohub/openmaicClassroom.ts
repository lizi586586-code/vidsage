import { get, post } from '@/utils/request'

export type OpenMAICCompletionStatus = 'not_started' | 'in_progress' | 'completed' | 'unknown'

export interface OpenMAICClassroomStatus {
  request_id: string
  generation_status: 'queued' | 'submitting' | 'running' | 'succeeded' | 'failed' | 'cancelled'
  progress?: number | null
  classroom_id: string
  classroom_url: string
  status: OpenMAICCompletionStatus
  completed_at?: string
  completion_source?: string
  evidence_version?: string
  last_error_code?: string
}

export async function prepareOpenMAICClassroom(input: { video_id?: string; knowledge_ids?: string[]; requirement: string }): Promise<OpenMAICClassroomStatus> {
  const response = await post<{ success: boolean; data?: OpenMAICClassroomStatus }>('/api/custom/learning/openmaic/prepare', input)
  if (!response.success || !response.data) throw new Error('课程准备失败，请检查课程资料和记忆权限')
  return response.data
}

export async function fetchOpenMAICClassroomStatus(classroomID: string): Promise<OpenMAICClassroomStatus> {
  const response = await get<{ success: boolean; data?: OpenMAICClassroomStatus }>(
    `/api/custom/learning/openmaic/${encodeURIComponent(classroomID)}/status`,
  )
  if (!response.success || !response.data) throw new Error('课程状态暂不可用')
  return response.data
}

export async function cancelOpenMAICClassroom(requestId: string): Promise<OpenMAICClassroomStatus> {
  try {
    const response = await post<{ success: boolean; data?: OpenMAICClassroomStatus; error?: string }>(
      `/api/custom/learning/openmaic/${encodeURIComponent(requestId)}/cancel`, {},
    )
    if (!response.success || !response.data) throw { status: 500, message: response.error || 'cancel_request_failed' }
    return response.data
  } catch (error) {
    const failure = error as { status?: number; message?: string; error?: string }
    throw { ...failure, code: failure.error || (failure.status ? failure.message : 'cancel_request_failed') }
  }
}
