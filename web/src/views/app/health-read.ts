export const dateText = (value: string | null | undefined) => (value ? new Date(value).toLocaleString() : '尚无');
export const outcomeText = (value: string) =>
	(
		({
			never_run: '尚未完成首次运行',
			healthy: '最近运行正常',
			limited: '最近运行有限覆盖',
			needs_attention: '最近运行需处理',
			cancelled: '最近运行已取消',
			archived: '已归档',
			unknown: '结果未知',
		}) as Record<string, string>
	)[value] || value;
export const issueText = (kind: string) =>
	(({ run_failed: '运行未完整通过', limited_coverage: '核对有限覆盖', schedule_blocked: '检查运行计划' }) as Record<string, string>)[kind] || kind;
export const decisionText = (value: string) =>
	(
		({
			created: '已入队',
			accepted_queue: '已接受等待',
			saved_enabled: '计划已启用',
			saved_paused: '计划已暂停',
			skipped_overlap: '因运行重叠跳过',
			skipped_capacity: '因运行容量跳过',
			disabled_invalid: '配置不兼容，计划已停用',
			disabled_unavailable: '版本或方案不可用，计划已停用',
		}) as Record<string, string>
	)[value] || value;
