import request from '/@/utils/request';
import { captureBlob } from '../app/browser-state';
import type { BrowserSession, DOMChildren, ElementInspection, InspectionInput, LocatorCheck } from '../app/api';

export interface Field {
	key: string;
	label?: string;
	type: string;
	required: boolean;
	nullable: boolean;
	multiple: boolean;
	empty_allowed?: boolean;
	default?: unknown;
}
export interface Binding {
	kind: 'field' | 'constant' | 'template';
	path?: string;
	value?: unknown;
}
export interface Locator {
	strategy: 'css' | 'xpath' | 'jsonpath';
	expression: string;
}
export interface Transform {
 version:'co.transform.v1';
 input: Field;
 expression: string;
 samples: {value:unknown; expected:unknown}[];
}
export interface Mapping {
 transform?:Transform;
	key: string;
	source: 'element' | 'input' | 'constant';
	input_key?: string;
	value?: unknown;
	locator?: Locator;
	extract?: 'text' | 'link' | 'image' | 'attribute';
	attribute?: string;
	clean?: string;
}
export interface Action {
	id: string;
	kind: string;
	locator?: Locator;
	value?: Binding;
	timeout_ms: number;
	replay_class: string;
}
export interface Definition {
	definition_version: 'collector.v1';
	interpreter_version: 'co.interpreter.v1';
	input_fields: Field[];
	fetch: {
		kind: 'http_html' | 'http_json' | 'browser';
		url: Binding;
		method?: 'GET' | 'POST';
		query?: Record<string, Binding>;
		body?: Binding;
		credential_ref?: string;
		allowed_origins: string[];
	};
	actions: Action[];
	records: Locator;
	output: { port: 'records'; fields: Field[]; identity_fields: string[]; mapping: Mapping[] };
	additional_outputs?: { records: Locator; output: { port: string; fields: Field[]; identity_fields: string[]; mapping: Mapping[] } }[];
	pagination: {
		kind: string;
		next?: Locator;
		empty_marker?: Locator;
		cursor_parameter?: string;
		end_condition: string;
		resume: string;
		max_pages: number;
		max_records: number;
		max_duration_ms: number;
	};
}
export interface Collector {
	version_number?: number;
	latest_test?: { id?: string; status?: string; result_status?: string; mode?: string };
	id: string;
	name: string;
	revision: number;
	definition: Definition | Record<string, never>;
	archived: boolean;
	updated_at: string;
}
export interface Candidate {
	port?: string;
	identity?: string;
	values: Record<string, unknown>;
	valid: boolean;
	page: number;
	fields: Record<string, { status: string; match_count: number; message?: string; error?: {code:string;message:string}; transform?: {version:string;hash:string;before:unknown;after:unknown;error?:unknown} }>;
}
export interface TestResult {
	definition_hash?: string;
	status: string;
	mode: string;
	reason?: string;
	input: Record<string, unknown>;
	candidates: Candidate[];
	pages: { url: string; record_count: number; content_hash: string; empty_verified: boolean }[];
	error?: { code: string; message: string; location?: Record<string, string> };
	warnings: string[];
	snapshots?: { url: string; format: string; content: string }[];
	comparison?: { status: string; actual_count: number; expected_count: number };
	formal_writes: number;
}
export interface Operation<T = any> {
	id: string;
	resource_id: string;
	operation: string;
	status: 'queued' | 'running' | 'succeeded' | 'failed' | 'cancelled';
	result: T;
	error?: { code?: string; message?: string; location?: Record<string, string> };
	created_at: string;
}
export interface Sample {
	id: string;
	revision: number;
	input: Record<string, unknown>;
	snapshots: { url: string; format: string; content: string }[];
	expected_outputs: Record<string, unknown>[];
}
export interface Version {
	collector_id?: string;
	id: string;
	number: number;
	revision: number;
	definition: Definition;
	definition_hash: string;
	input_schema_hash: string;
	output_schema_hash: string;
	requirements: Record<string, unknown>;
	created_at: string;
}
export interface Page<T> {
	items: T[];
	next_cursor: string;
}
export const key = () => {
	if (crypto.randomUUID) return crypto.randomUUID();
	const bytes = crypto.getRandomValues(new Uint8Array(16));
	bytes[6] = (bytes[6] & 15) | 64;
	bytes[8] = (bytes[8] & 63) | 128;
	const h = Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('');
	return `${h.slice(0, 8)}-${h.slice(8, 12)}-${h.slice(12, 16)}-${h.slice(16, 20)}-${h.slice(20)}`;
};
export function call<T>(path: string, method = 'get', data?: unknown, idempotency?: string, timeout = 8000): Promise<T> {
	return request({
		url: '/api/v4' + path,
		method,
		data,
		localError: true,
		timeout,
		headers: idempotency ? { 'Idempotency-Key': idempotency } : undefined,
	}).then((r: any) => r.data as T);
}
export const coApi = {
	list: (cursor = '') => call<Page<Collector>>('/collectors?limit=20&cursor=' + encodeURIComponent(cursor)),
	get: (id: string) => call<Collector>('/collectors/' + id),
	save: (id: string, revision: number, definition: Definition) =>
		call<Collector>('/collectors/' + id + '/draft', 'put', { expected_revision: revision, definition }),
	operation: <T = any>(id: string) => call<Operation<T>>('/operations/' + id),
	tests: (id: string, cursor = '') => call<Page<Operation<TestResult>>>('/collectors/' + id + '/tests?limit=20&cursor=' + encodeURIComponent(cursor)),
	samples: (id: string) => call<Page<Sample>>('/collectors/' + id + '/samples?limit=100'),
	versions: (id: string) => call<Page<Version>>('/collectors/' + id + '/versions?limit=100'),
	version: (id: string) => call<Version>('/collector-versions/' + id),
	session: (id: string) => call<BrowserSession>('/sessions/' + id),
	frame: (session: BrowserSession) =>
		request({
			url: session.frame_url + '?page_state_id=' + encodeURIComponent(session.page_state_id),
			method: 'get',
			responseType: 'blob',
			responseEnvelope: true,
			localError: true,
			timeout: 8000,
		}).then((r: any) => captureBlob(r.data, r.headers['x-scrapio-frame'], session.page_state_id)),
	inspect: <T>(id: string, operation: string, input: Partial<InspectionInput> & { page_state_id: string; offset?: number; limit?: number }) =>
		call<T>('/sessions/' + id + '/inspections', 'post', { operation, page_state_id: input.page_state_id, input }, key()),
};
export const selectionApi = {
	inspectElement: (id: string, input: InspectionInput) => coApi.inspect<ElementInspection>(id, 'inspect', input),
	checkLocator: (id: string, input: InspectionInput) => coApi.inspect<LocatorCheck>(id, 'locator-check', input),
	recheckLocator: (id: string, input: InspectionInput) => coApi.inspect<LocatorCheck>(id, 'locator-recheck', input),
	domChildren: (id: string, page_state_id: string, element_id = '', offset = 0) =>
		coApi.inspect<DOMChildren>(id, 'dom', { page_state_id, element_id, offset, limit: 50 }),
};
export function describe(e: any) {
	const problem = e?.error || e?.response?.data?.error;
	return problem
		? [problem.message, ...Object.values(problem.location || {})].filter(Boolean).join(' · ')
		: e?.message || '请求失败；可查询原操作，不自动重试访问。';
}
export function initialDefinition(): Definition {
	return {
		definition_version: 'collector.v1',
		interpreter_version: 'co.interpreter.v1',
		input_fields: [{ key: 'url', type: 'url', required: true, nullable: false, multiple: false }],
		fetch: { kind: 'http_html', url: { kind: 'field', path: 'input.url' }, allowed_origins: [], method: 'GET' },
		actions: [],
		records: { strategy: 'css', expression: 'main' },
		output: {
			port: 'records',
			fields: [{ key: 'source_url', type: 'url', required: true, nullable: false, multiple: false }],
			identity_fields: ['source_url'],
			mapping: [{ key: 'source_url', source: 'input', input_key: 'url' }],
		},
		pagination: { kind: 'none', end_condition: 'single_page', resume: 'url_or_cursor', max_pages: 1, max_records: 100, max_duration_ms: 60000 },
	};
}
