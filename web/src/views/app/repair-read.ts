export const evidenceText = (status: string) =>
	(
		({
			available: '原始输入已保存，可复制到修复草稿做离线提取验证',
			missing: '原始输入未保存或已清理；保留诊断，需要显式重新取样',
			corrupt: '输入完整性校验失败，不能带入；需要重新取样',
			too_large: '输入超过离线快照上限，不能带入；需要重新取样',
			unsupported: '输入格式不支持离线预览',
		}) as Record<string, string>
	)[status] || status;
