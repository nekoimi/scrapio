export function statusLabel(status: string) {
	return (
		(
			{
				preparing: '准备输入',
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
