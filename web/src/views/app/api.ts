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

async function get<T>(url: string): Promise<T> {
  const response: any = await request({ url, method: 'get' });
  return response.data as T;
}

export const appApi = {
  me: () => get<Identity>('/api/v3/me'),
  capabilities: () => get<Capabilities>('/api/v3/capabilities'),
  home: () => get<HomeState>('/api/v3/home'),
};
