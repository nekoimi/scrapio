import request from '/@/utils/request';

export interface Identity { id: string; username: string }
export interface Capabilities {
  editor_protocol: string;
  browser_ready: boolean;
  supported_actions: string[];
  supports_live_inspection: boolean;
  definition_versions: number[];
  reason?: string;
}
export interface HomeState { status: 'pending' | 'ready'; reason?: string }
export interface Collector { id: string; name: string; entry_url: string; entry_type: 'web' | 'json'; status: string; definition: Record<string, any>; revision: number; validation_status?: 'not_validated' | 'valid' | 'invalid' | 'stale'; validation_errors?: string[]; save_summary?: { revision: number; changed_fields: string[]; saved_at: string }; updated_at: string }

async function get<T>(url: string): Promise<T> {
  const response: any = await request({ url, method: 'get' });
  return response.data as T;
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
};
