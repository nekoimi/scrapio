import request from '/@/utils/request';

export interface Identity { id: number; username: string }
export interface Capabilities {
  editor_protocol: string;
  browser_ready: boolean;
  supported_actions: string[];
  supports_live_inspection: boolean;
  definition_versions: number[];
  reason?: string;
}
export interface HomeState { status: 'pending' | 'ready'; reason?: string }
export interface Collector { id: number; name: string; entry_url: string; entry_type: 'web' | 'json'; status: string; definition: string | Record<string, any>; revision: number; updated_at: string }

async function get<T>(url: string): Promise<T> {
  const response: any = await request({ url, method: 'get' });
  return response.data as T;
}

export const appApi = {
  me: () => get<Identity>('/api/v3/me'),
  capabilities: () => get<Capabilities>('/api/v3/capabilities'),
  home: () => get<HomeState>('/api/v3/home'),
  collectors: () => get<{ items: Collector[]; has_more: boolean }>('/api/v3/collectors'),
  collector: (id: number) => get<Collector>(`/api/v3/collectors/${id}/draft`),
  createCollector: (data: { name?: string; entry_url: string; entry_type: 'web' | 'json' }) => request({ url: '/api/v3/collectors', method: 'post', data, headers: { 'Idempotency-Key': crypto.randomUUID() } }).then((response: any) => response.data as Collector),
  updateCollector: (id: number, data: { name: string; expected_revision: number; definition: Record<string, any> }) => request({ url: `/api/v3/collectors/${id}/draft`, method: 'put', data }).then((response: any) => response.data as Collector),
};
