export function readError(cause: any): string {
	return cause?.error?.message || cause?.message || '读取失败，请重试';
}
export function displayJSON(raw: string): string {
	if (!raw) return '—';
	if (raw.startsWith('"')) {
		try {
			return JSON.parse(raw);
		} catch {}
	}
	return raw;
}
export function parseJSON<T>(raw: string, fallback: T): T {
	try {
		return JSON.parse(raw);
	} catch {
		return fallback;
	}
}
export function runStatus(value: string): string {
	return (
		(
			{
				queued: '排队中',
				running: '执行中',
				succeeded: '已完成',
				limited: '有限覆盖',
				partial: '未完整通过',
				failed: '失败',
				cancelled: '已取消',
			} as Record<string, string>
		)[value] || value
	);
}
