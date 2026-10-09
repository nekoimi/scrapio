import request from '/@/utils/request';
import { Session } from '/@/utils/storage';

export interface Identity { id: string; username: string }
export interface Capabilities {
  editor_protocol: string;
  browser_ready: boolean;
  editor_service_connected?: boolean;
  supported_actions: string[];
  supports_live_inspection: boolean;
  definition_versions: number[];
  reason?: string;
}
export interface HomeState { status: 'pending' | 'ready'; reason?: string }
export interface Collector { id: string; name: string; entry_url: string; entry_type: 'web' | 'json'; status: string; definition: Record<string, any>; revision: number; validation_status?: 'not_validated' | 'valid' | 'invalid' | 'stale'; validation_errors?: string[]; save_summary?: { revision: number; changed_fields: string[]; saved_at: string }; updated_at: string }
export interface BrowserSession { session_id: string; collector_id: string; draft_revision: number; status: string; expires_at: string; page_state_id: string; current_url: string; viewport: { width: number; height: number }; frame_url: string; heartbeat_interval_seconds: number; available: boolean; needs_reopen: boolean }

export interface EditorAction {
  type: string; value?: string; locator?: { strategy: 'css' | 'xpath'; expression: string };
  position?: { x: number; y: number }; timeout_ms: number; confirmed: boolean;
  page_state_id: string; expected_revision: number; record: boolean; step_id?: string;
}
export interface EditorCommand {
  command_id: string; session_id: string; status: 'queued' | 'running' | 'succeeded' | 'failed' | 'uncertain';
  type: string; request: EditorAction;
  result: { status: string; before_page_state_id: string; after_page_state_id: string; final_url: string;
    duration_ms: number; error?: string; error_code?: string; locator?: { strategy: 'css' | 'xpath'; expression: string };
    recording_status?: string; recording_error?: string; recorded_revision?: number };
}
export interface EditorCheckpoint {
  checkpoint_id: string; name: string; draft_revision: number; through_step_id: string;
  snapshot: { definition_version: number; entry_url: string; steps: Record<string, any>[] }; created_at: string;
}

async function get<T>(url: string): Promise<T> {
  const response: any = await request({ url, method: 'get' });
  return response.data as T;
}

async function streamBrowserSession(id: string, signal: AbortSignal, onSession: (session: BrowserSession) => void): Promise<void> {
  const baseURL = (import.meta.env.VITE_API_URL || '').replace(/\/+$/, '');
  const response = await fetch(`${baseURL}/api/v3/browser-sessions/${id}/events`, {
    headers: { Authorization: Session.get('token') || '', Accept: 'text/event-stream' },
    credentials: 'same-origin',
    signal,
  });
  if (!response.ok || !response.body) {
    const body = await response.json().catch(() => undefined);
    throw Object.assign(new Error(body?.error?.message || `会话事件流连接失败（${response.status}）`), { terminal: [401, 403, 404, 410].includes(response.status) });
  }
  if (!response.headers.get('Content-Type')?.includes('text/event-stream')) {
    await response.body.cancel();
    throw Object.assign(new Error('会话事件流响应格式错误'), { terminal: true });
  }
  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = '';
  try {
    while (!signal.aborted) {
      const { value, done } = await reader.read();
      if (done) return;
      buffer += decoder.decode(value, { stream: true }).replace(/\r\n/g, '\n');
      let boundary = buffer.indexOf('\n\n');
      while (boundary >= 0) {
        const message = buffer.slice(0, boundary);
        buffer = buffer.slice(boundary + 2);
        const data = message.split('\n').filter(line => line.startsWith('data:')).map(line => line.slice(5).trim()).join('\n');
        if (data) onSession(JSON.parse(data) as BrowserSession);
        boundary = buffer.indexOf('\n\n');
      }
    }
  } finally {
    await reader.cancel().catch(() => undefined);
    reader.releaseLock();
  }
}

export const appApi = {
  me: () => get<Identity>('/api/v3/me'),
  capabilities: () => get<Capabilities>('/api/v3/capabilities'),
  home: () => get<HomeState>('/api/v3/home'),
  collectors: (cursor = '') => get<{ items: Collector[]; has_more: boolean; next_cursor?: string }>(`/api/v3/collectors?limit=25${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ''}`),
  collector: (id: string) => get<Collector>(`/api/v3/collectors/${id}/draft`),
  copyCollector: (id: string) => request({ url: `/api/v3/collectors/${id}/copies`, method: 'post', headers: { 'Idempotency-Key': crypto.randomUUID() } }).then((response: any) => response.data as Collector),
  createCollector: (data: { name?: string; entry_url: string; entry_type: 'web' | 'json' }) => request({ url: '/api/v3/collectors', method: 'post', data, headers: { 'Idempotency-Key': crypto.randomUUID() } }).then((response: any) => response.data as Collector),
  updateCollector: (id: string, data: { name: string; expected_revision: number; definition: Record<string, any> }) => request({ url: `/api/v3/collectors/${id}/draft`, method: 'put', data }).then((response: any) => response.data as Collector),
  validateCollector: (id: string) => request({ url: `/api/v3/collectors/${id}/validate`, method: 'post' }).then((response: any) => response.data as { collector: Collector; validation: { valid: boolean; errors: string[] } }),
  createBrowserSession: (collectorId: string, draftRevision: number, key: string) => request({ url: '/api/v3/browser-sessions', method: 'post', data: { collector_id: collectorId, draft_revision: draftRevision, viewport: { width: 1280, height: 800 } }, headers: { 'Idempotency-Key': key } }).then((response: any) => response.data as BrowserSession),
  browserSession: (id: string) => get<BrowserSession>(`/api/v3/browser-sessions/${id}`),
  heartbeatBrowserSession: (id: string) => request({ url: `/api/v3/browser-sessions/${id}/heartbeat`, method: 'post' }).then((response: any) => response.data as BrowserSession),
  streamBrowserSession,
  resumeBrowserSession: (id: string) => request({ url: `/api/v3/browser-sessions/${id}/resume`, method: 'post' }).then((response: any) => response.data as BrowserSession),
  closeBrowserSession: (id: string) => request({ url: `/api/v3/browser-sessions/${id}`, method: 'delete' }),
  createBrowserCommand: (id: string, data: EditorAction, key: string) => request({ url: `/api/v3/browser-sessions/${id}/commands`, method: 'post', data, headers: { 'Idempotency-Key': key } }).then((response: any) => response.data as EditorCommand),
  browserCommand: (id: string, commandId: string) => get<EditorCommand>(`/api/v3/browser-sessions/${id}/commands/${commandId}`),
  browserCommands: (id: string) => get<{ items: EditorCommand[]; limit: number }>(`/api/v3/browser-sessions/${id}/commands`),
  checkpoints: (id: string) => get<{ items: EditorCheckpoint[] }>(`/api/v3/collectors/${id}/checkpoints`),
  createCheckpoint: (id: string, data: { name: string; expected_revision: number; through_step_id: string }) => request({ url: `/api/v3/collectors/${id}/checkpoints`, method: 'post', data }).then((response: any) => response.data as EditorCheckpoint),
  deleteCheckpoint: (id: string, key: string) => request({ url: `/api/v3/collectors/${id}/checkpoints/${key}`, method: 'delete' }),
  browserFrame: (url: string, pageStateId: string) => request({ url: `${url}?page_state_id=${encodeURIComponent(pageStateId)}`, method: 'get', responseType: 'blob' }).then((response: any) => response as Blob),
};
