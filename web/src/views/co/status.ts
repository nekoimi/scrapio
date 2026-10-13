export function statusLabel(status: string) {
	return (
		(
			{
				preparing: '准备输入',
				skipped: '刷新间隔内跳过',
				skipped_overlap: '已有运行，跳过触发',
				started: '已触发运行',
				waiting_upstream: '等待上游',
				dispatching: '正在分发',
				backpressured: '等待下游消化积压',
				filtered: '已过滤',
				delivered: '已派发',
				merged: '合并到已有工作项',
				ready: '有效输入',
				invalid: '映射无效',
				queued: '排队',
				running: '运行中',
				pausing: '等待当前页保存后暂停',
				paused: '已暂停',
				cancelled: '已取消',
				awaiting_confirmation: '等待人工确认',
				succeeded: '完成',
				partial: '部分完成',
				limited: '达到预算',
				failed: '失败',
			} as Record<string, string>
		)[status] || status
	);
}
