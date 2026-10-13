import { call, key, type Definition, type Field, type Binding, type Candidate, type Page, type Operation } from './api';
export interface SaveBinding {
	table_id: string;
	schema_version: number;
	schema_hash: string;
	mapping: Record<string, Binding>;
	unique_key: string[];
	update_policy: string;
	null_policy: string;
}
export interface DataTable {
	id: string;
	name: string;
	schema_version: number;
	schema_hash: string;
	schema: { fields: Field[]; unique_key: string[] };
}
export interface TaskStage {
	id: string;
	name: string;
	collector_version_id: string;
	save: SaveBinding;
	on_error: string;
	refresh_seconds?: number;
	input_policy?: 'keep_all' | 'merge_identical';
}
export interface TaskSource {
	kind: 'manual' | 'table' | 'file';
	inputs: Record<string, unknown>[];
	snapshot_id?: string;
	snapshot_hash?: string;
	count?: number;
	accept_partial?: boolean;
}
export interface TaskEdge {
	id: string;
	from: string;
	to: string;
	port: 'records';
	emit: 'valid' | 'created' | 'changed';
	mapping: Record<string, Binding>;
	filter?: { path: string; op: 'exists' | 'equal' | 'not_equal'; value?: unknown };
	expand?: { path: string };
}
export interface TaskDefinition {
	definition_version: 'task.v1';
	entry_stage: string;
	source: TaskSource;
	stages: TaskStage[];
	edges: TaskEdge[];
	budget: {
		max_work_items: number;
		max_outputs: number;
		max_pending: number;
		browser_slots: number;
		max_duration_ms: number;
		max_dispatches?: number;
	};
	overlap_policy: 'skip';
}
export interface Task {
	id: string;
	name: string;
	revision: number;
	definition: TaskDefinition;
	updated_at: string;
}
export interface TaskSummary {
	archived: boolean;
	source_kind: string;
	stage_count: number;
	id: string;
	name: string;
	revision: number;
	input_count: number;
	table_id: string;
	schema_version: string;
	updated_at: string;
}
export interface Manifest {
	definition: TaskDefinition;
	collector: { id: string; collector_id: string; number: number };
	collector_definition: Definition;
	hash: string;
}
export interface TaskVersion {
	id: string;
	number: number;
	revision: number;
	manifest: Manifest;
}
export interface Run {
	task_id?: string;
	id: string;
	status: string;
	namespace: string;
	task_version_id: string;
	manifest: Manifest;
	control_epoch: number;
	input_count: number;
	preparing_cursor: number;
	output_count: number;
	retry_of?: string;
	retry_scope: string[];
	event_seq: number;
	created_at: string;
}
export interface WorkItem {
	stage_run_id: string;
	id: string;
	status: string;
	attempt: number;
	input_values: Record<string, unknown>;
	checkpoint: { pages?: number; next_url?: string; done?: boolean };
	error: { code?: string; message?: string; location?: Record<string, string> };
}
// Mutation identity survives lost responses/reloads. Changed payloads get distinct
// keys; the original operation remains queryable and is never blindly triggered.
export async function taskMutation<T = any>(operation: string, resource: string, path: string, payload: unknown): Promise<T> {
	const slot = `co-task:${operation}:${resource}`;
	const fingerprint = JSON.stringify(payload);
	const cached = sessionStorage.getItem(slot);
	let pending: { fingerprint: string; key: string } | undefined;
	try {
		pending = cached ? JSON.parse(cached) : undefined;
	} catch {
		sessionStorage.removeItem(slot);
	}
	if (pending && pending.fingerprint !== fingerprint) throw new Error('上次操作结果尚未确认；请查询原操作，或恢复原参数后再次点击。');
	const mutationKey = pending?.key || key();
	sessionStorage.setItem(slot, JSON.stringify({ fingerprint, key: mutationKey }));
	try {
		const result = await call<T>(path, 'post', payload, mutationKey);
		sessionStorage.removeItem(slot);
		return result;
	} catch (e: any) {
		if ((e?.response?.status && e.response.status < 500) || (e?.error?.code && !['INTERNAL_ERROR', 'OUTCOME_UNCERTAIN'].includes(e.error.code)))
			sessionStorage.removeItem(slot);
		throw e;
	}
}
export async function recoverTaskMutation(operation: string, resource: string) {
	const slot = `co-task:${operation}:${resource}`;
	const raw = sessionStorage.getItem(slot);
	if (!raw) return undefined;
	const pending = JSON.parse(raw);
	let result: Operation;
	try {
		result = await call<Operation>('/operations/by-key?' + new URLSearchParams({ operation, resource_id: resource, key: pending.key }));
	} catch (e: any) {
		if (e?.error?.code === 'NOT_FOUND') {
			throw new Error('暂未查到原操作，幂等键已保留；可恢复原参数后再次提交同一个请求，不要另建重复运行。');
		}
		throw e;
	}
	sessionStorage.removeItem(slot);
	return result.result;
}
export const taskApi = {
	list: (cursor = '') => call<Page<TaskSummary>>('/tasks?limit=20&cursor=' + encodeURIComponent(cursor)),
	get: (id: string) => call<Task>('/tasks/' + id),
	save: (id: string, revision: number, definition: TaskDefinition) =>
		call<Task>('/tasks/' + id + '/draft', 'put', { expected_revision: revision, definition }),
	versions: (id: string) => call<{ items: TaskVersion[] }>('/tasks/' + id + '/versions'),
	tables: (cursor = '') => call<Page<DataTable>>('/tables?limit=100&cursor=' + encodeURIComponent(cursor)),
	inputPreview: (version: string, inputs: unknown) =>
		call<{ items: { ordinal: number; source_key: string; values?: unknown; error?: { message: string } }[] }>(
			'/tasks/input-preview',
			'post',
			{ collector_version_id: version, inputs },
			key()
		),
	mappingPreview: (up: string, down: string, edge: TaskEdge, input: unknown, output: unknown) =>
		call<{ items: any[]; mapping_hash: string }>(
			'/tasks/mapping-preview',
			'post',
			{ upstream_version_id: up, downstream_version_id: down, edge, input, output },
			key()
		),
	savePreview: (version: string, save: SaveBinding, candidates: Candidate[]) =>
		call<{ decisions: { identity: string; decision: string; values: unknown; error?: { message: string } }[] }>(
			'/tasks/save-preview',
			'post',
			{ collector_version_id: version, save, candidates },
			key()
		),
	runs: (cursor = '') => call<Page<Run>>('/runs?limit=20&cursor=' + encodeURIComponent(cursor)),
	run: (id: string, summary = false) =>
		call<{ run: Run; stages: any[]; counts: { status: string; count: number }[] }>('/runs/' + id + (summary ? '?summary=1' : '')),
	runPage: <T = any>(id: string, kind: string, cursor = '') =>
		call<Page<T>>('/runs/' + id + '/' + kind + '?limit=50&cursor=' + encodeURIComponent(cursor)),
	item: (run: string, id: string, sourceCursor = '') =>
		call<{
			item: WorkItem;
			attempts: { id: string; number: number; status: string; command_refs: { command_id: string; status: string }[]; error: any }[];
			input: any[];
			sources: any[];
			next_source_cursor: string;
			lineage: any[];
			lineage_has_more: boolean;
		}>('/runs/' + run + '/work-items/' + id + '?source_cursor=' + encodeURIComponent(sourceCursor)),
	events: (id: string, after: number) =>
		call<{ items: { sequence: number; type: string; item_id?: string; data: unknown }[]; next_sequence: number; latest_sequence: number }>(
			'/runs/' + id + '/events?after=' + after
		),
	records: (id: string, cursor = '') =>
		call<Page<{ id: string; values: Record<string, unknown>; revision: number; schema_version: number }>>(
			'/tables/' + id + '/records?limit=50&cursor=' + encodeURIComponent(cursor)
		),
	history: (id: string) => call<any>('/records/' + id),
};
