import type { RecordPlan } from './api';

export interface EditorProblem {
	title: string;
	reason: string;
	recovery: string;
	code?: string;
	requestID?: string;
	commandID?: string;
	stage?: string;
	path?: string;
}

// Keep the initiating action even when the remote service only returns a generic
// code. Request metadata is for diagnosis, not an invitation to replay commands.
export function editorProblem(cause: any, action: string): EditorProblem {
	const body = cause?.response?.data || cause;
	const error = body?.error;
	const code = error?.code || cause?.code;
	let reason = error?.message || cause?.message || '请求没有返回可确认的结果。';
	let recovery = '当前配置已保留。请核对本操作涉及的配置，必要时展开诊断信息。';
	switch (code) {
		case 'INVALID_ARGUMENT':
			if (!error?.path || !error?.message) reason = '服务拒绝了本次请求，返回信息尚未细分到具体字段。';
			recovery = '请核对当前步骤的规则、字段和预算，并确认主平台与浏览器服务版本一致。无需盲目重新点选全部元素。';
			break;
		case 'INVALID_RECORD_PLAN': recovery = '核对记录方式、字段与预算，修正指出的配置后保存，再检查当前页面；若配置有效，请确认两端版本一致。'; break;
		case 'RECORD_STEP_MISSING': recovery = '确认当前采集步骤仍存在，详情入口已保存；核对最新草稿后重新检查。'; break;
		case 'INVALID_LOCATOR': recovery = '请检查对应 CSS / XPath；重复记录内的 XPath 必须以 . 开头，再验证该规则。'; break;
		case 'RECORD_PREVIEW_LIMIT': recovery = '单次浏览器预览最多 20 条。升级主平台以自动取样，不必降低正式采集上限。'; break;
		case 'INVALID_DETAIL_RULE': recovery = '进入“详情路径 → 高级”，将最多详情候选设为 1 至 5，再保存。'; break;
		case 'INVALID_FIELD_RULE': case 'FIELD_PREVIEW_LIMIT': recovery = '核对列表/详情字段的名称与提取方式；每个页面最多 30 个字段。'; break;
		case 'STALE_FRAME': case 'STALE_PAGE_STATE': case 'ELEMENT_REFERENCE_STALE': recovery = '更新当前网页画面，再验证当前规则；已填写的字段配置会保留。'; break;
		case 'RECORD_SCOPE_EMPTY': recovery = '确认浏览器当前是列表页，重新检查记录范围的命中，再检查详情入口。'; break;
		case 'OUTSIDE_RECORD_SCOPE': recovery = '在已选择的记录容器内部点选，或先调整记录范围。'; break;
		case 'COMMAND_PENDING': recovery = '先到“操作记录”查询上一动作回执，结果未确认前不要重复执行。'; break;
		case 'OUTCOME_UNCERTAIN': recovery = '动作结果尚未确认。先到“操作记录”查询这条动作回执并核对当前网页，确认结果前不要重复执行。'; break;
		case 'SESSION_GONE': recovery = '恢复或重新打开浏览器会话，再检查当前页面。'; break;
		case 'STALE_REVISION': recovery = '草稿已被其他请求修改。保留本地内容，核对并加载最新草稿后再保存。'; break;
		case 'INSPECTION_INPUT_LIMIT': recovery = '精简本次检查的字段或定位规则；这属于检查容量，不代表元素点选错误。'; break;
		case 'INSPECTION_FAILED': case 'BROWSER_UNAVAILABLE':
			reason = error?.message || '检查服务暂时不可用。';
			recovery = '确认浏览器服务连接后重试只读检查；网页动作结果请先查询回执。';
			break;
	}
	if (!error && (code === 'ECONNABORTED' || code === 'ETIMEDOUT' || /timeout|Network Error/i.test(reason))) {
		reason = '网络超时或服务连接中断，尚未确认本次请求结果。';
		recovery = '只读检查可在连接恢复后重试；保存或网页动作请先查询原草稿/回执，避免重复提交。';
	}
	return { title: `${action}失败`, reason, recovery, code,
		requestID: body?.request_id || error?.request_id || cause?.request_id,
		commandID: body?.command_id,
		stage: error?.stage, path: error?.path };
}

export function problemMessage(problem: EditorProblem) {
	return `${problem.title}：${problem.reason} ${problem.recovery}${problem.requestID ? `（请求 ${problem.requestID}）` : ''}`;
}

export function recordConfigProblem(plan: RecordPlan): { message: string; path: string } | undefined {
	const issue = (message: string, path: string) => ({ message, path });
	const locatorOK = (locator: any, relative = false) => !!locator && ['css', 'xpath'].includes(locator.strategy)
		&& typeof locator.expression === 'string' && !!locator.expression.trim() && locator.expression.length <= 2048
		&& (!relative || locator.strategy !== 'xpath' || locator.expression.startsWith('.'));
	if (!['single', 'repeated'].includes(plan.mode)) return issue('请选择整页一条或重复容器的记录方式。', 'config.mode');
	if (!Number.isInteger(plan.max_records) || plan.max_records < 1 || plan.max_records > 200) return issue('记录上限须为 1 至 200 的整数。', 'config.max_records');
	if (plan.mode === 'repeated' && !locatorOK(plan.locator)) return issue('重复记录尚未配置有效的记录范围，请先点选记录容器。', 'config.locator');
	for (const [role, fields] of [['列表', plan.fields], ['详情', plan.detail_fields || []]] as const) {
		const prefix = role === '列表' ? 'config.fields' : 'config.detail_fields';
		if (fields.length > 30) return issue(`${role}字段超过 30 个，请精简后保存。`, prefix);
		const names = new Set<string>();
		for (const [index, field] of fields.entries()) {
			const label = `${role}字段“${field.name || `第 ${index + 1} 项`}”`;
			if (!/^[\p{L}_][\p{L}\p{N}_-]{0,63}$/u.test(field.name)) return issue(`${label}名称无效：以字母、汉字或下划线开头，最多 64 字符，不含空格。`, `${prefix}[${index}].name`);
			if (names.has(field.name)) return issue(`${label}名称重复，请修改其中一个。`, `${prefix}[${index}].name`);
			names.add(field.name);
			if (field.locator && !locatorOK(field.locator, role === '列表' && plan.mode === 'repeated')) return issue(`${label}定位规则为空或无效；重复记录内的 XPath 须以 . 开头。`, `${prefix}[${index}].locator`);
			if (!['text', 'link', 'image', 'attribute'].includes(field.extract)) return issue(`${label}提取方式无效。`, `${prefix}[${index}].extract`);
			if (field.extract === 'attribute' && (!/^[a-zA-Z][a-zA-Z0-9_-]{0,63}$/.test(field.attribute || '') || field.attribute === 'value')) return issue(`${label}需要有效属性名，例如 data-id；不支持读取输入值 value。`, `${prefix}[${index}].attribute`);
		}
	}
	if (!plan.detail && plan.detail_fields?.length) return issue('已有详情字段，请先配置详情入口。', 'config.detail');
	if (plan.detail) {
		if (!locatorOK(plan.detail.locator, plan.mode === 'repeated')) return issue('详情入口定位规则无效，请点选链接或核对相对 XPath。', 'config.detail.locator');
		if (!['back', 'navigate-list'].includes(plan.detail.return_strategy)) return issue('请选择有效的详情返回方式。', 'config.detail.return_strategy');
		if (!Number.isInteger(plan.detail.max_details) || plan.detail.max_details < 1 || plan.detail.max_details > 5) return issue('最多详情候选须为 1 至 5 的整数。', 'config.detail.max_details');
	}
	if (plan.next_page) {
		if (!locatorOK(plan.next_page.locator)) return issue('翻页入口定位规则无效，请重新点选或填写规则。', 'config.next_page.locator');
		if (!['link', 'click', 'load_more'].includes(plan.next_page.kind)) return issue('请选择有效的翻页方式。', 'config.next_page.kind');
		if (!Number.isInteger(plan.next_page.max_pages) || plan.next_page.max_pages < 1 || plan.next_page.max_pages > 100) return issue('最多列表轮次须为 1 至 100 的整数。', 'config.next_page.max_pages');
		if (plan.next_page.wait_ms !== undefined && (!Number.isInteger(plan.next_page.wait_ms) || plan.next_page.wait_ms < 0 || plan.next_page.wait_ms > 10000)) return issue('翻页等待须为 0 至 10000 毫秒的整数。', 'config.next_page.wait_ms');
	}
}
