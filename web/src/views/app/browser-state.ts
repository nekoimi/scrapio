export interface BrowserState {
	protocol: 'editor.state.v1';
	page_state_id: string;
	document_id: string;
	validation_state_id: string;
	frame_state_id: string;
	viewport: { width: number; height: number };
	scroll: { x: number; y: number };
	pixel_ratio: number;
	frame_sequence?: number;
	image?: { width: number; height: number };
	captured_at_unix_ms?: number;
}
export type CaptureBlob = Blob & { frameState?: BrowserState };
export function validBrowserState(state: any, page: string, capture = false): state is BrowserState {
	const size = (value: any) =>
		Number.isInteger(value?.width) &&
		Number.isInteger(value?.height) &&
		value.width > 0 &&
		value.height > 0 &&
		value.width <= 16384 &&
		value.height <= 16384;
	return (
		!!state &&
		state.protocol === 'editor.state.v1' &&
		state.page_state_id === page &&
		!!page &&
		[state.document_id, state.validation_state_id, state.frame_state_id].every((id) => typeof id === 'string' && /^[a-f0-9]{64}$/.test(id)) &&
		size(state.viewport) &&
		Number.isFinite(state.scroll?.x) &&
		Number.isFinite(state.scroll?.y) &&
		Number.isFinite(state.pixel_ratio) &&
		state.pixel_ratio > 0 &&
		state.pixel_ratio <= 8 &&
		(!capture ||
			(size(state.image) &&
				Number.isSafeInteger(state.frame_sequence) &&
				state.frame_sequence > 0 &&
				Number.isSafeInteger(state.captured_at_unix_ms) &&
				state.captured_at_unix_ms > 0))
	);
}
// Missing metadata is a supported legacy response. Malformed metadata is not.
export function captureBlob(blob: Blob, header: unknown, page: string): CaptureBlob {
	if (header === undefined || header === null || header === '') return blob;
	if (typeof header !== 'string' || header.length > 4096) throw new Error('画面元数据无效，请检查浏览器服务版本。');
	const state = JSON.parse(header);
	if (!validBrowserState(state, page, true)) throw new Error('画面与元数据版本不一致，请更新画面。');
	return Object.assign(blob, { frameState: state });
}
export function stateChange(before?: BrowserState, after?: BrowserState) {
	if (!before || !after) return 'legacy';
	if (before.document_id !== after.document_id) return 'document';
	if (before.validation_state_id !== after.validation_state_id) return 'validation';
	if (before.frame_state_id !== after.frame_state_id) return 'frame';
	return 'same';
}
