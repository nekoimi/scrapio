<template>
	<section class="record-designer">
		<p v-if="selectionBlocked" class="status-warn" role="status">{{ detailBlockReason }}</p>
		<div v-if="context?.needsConfirmation" class="selection-scope" role="status">
			<p>详情打开动作已完成，当前页面用途尚待核对。请确认网页是否为所需详情。</p>
			<p>原地址：{{ context.requestedURL }}<br />实际地址：{{ context.detailURL }}</p>
			<button :disabled="working || !available || !samePageURL(context.detailURL, session?.current_url)" @click="resumeDetailFields">确认当前详情页并选字段</button>
			<button :disabled="working || !available || localDirty" @click="returnToList">返回列表</button>
		</div>
		<div v-show="view !== 'preview'">
			<div class="record-source-toolbar">
				<label
					>当前采集步骤<select
						:value="stepID"
						:disabled="working || !!context || localDirty"
						@change="chooseStep(($event.target as HTMLSelectElement).value)"
					>
						<option value="">新采集步骤</option>
						<option v-for="(step, index) in recordSteps" :key="step.step_id" :value="step.step_id">
							采集步骤 {{ index + 1 }} · {{ step.config?.fields?.length || 0 }} 个列表字段
						</option>
					</select></label
				>
				<details class="editor-advanced">
					<summary>草稿工具</summary>
					<button :disabled="working || blocked" @click="reload">重新读取草稿</button>
				</details>
			</div>
			<RecordRuleEditor
				ref="ruleEditor"
				:plan="plan"
				:available="available && !selectionBlocked"
				:disabled="working || blocked || unsupported"
				:detail-page="!!context"
				:can-preview="canPreview"
				:can-open-detail="available && !working && !unsupported && !!plan.detail && !context"
				:unsaved="localDirty"
				:can-return="available && !working && !context?.uncertain && !localDirty"
				:preview="preview"
				:preview-stale="previewStale"
				@select="beginSelection"
				@inspect-path="inspectPath"
				@return-list="returnToList"
				@open-detail="openExampleDetail"
			/>
			<div class="record-save-bar">
				<span>{{ localDirty ? '有未保存修改' : stepID ? '规则已保存 · 页面检查另行核对' : '尚未保存采集步骤' }}</span>
				<div class="editor-actions">
					<button :disabled="working || blocked || !localDirty || unsupported" @click="save">保存修改</button>
					<button
						class="primary-action"
						:disabled="working || blocked || unsupported || !available || (!localDirty && !canPreview)"
						@click="saveAndPreview"
					>
						{{ working ? '处理中…' : localDirty ? '保存并查看数据' : '查看当前页数据' }}
					</button>
					<button :disabled="!localDirty || working || blocked" @click="undoLocal">撤销修改</button>
				</div>
			</div>
			<p v-if="selectionNotice" class="record-selection-notice" role="status">
				{{ selectionNotice }}<button :disabled="!available || working" @click="beginSelection('field')">继续选字段</button>
			</p>
			<p v-if="unsupported" class="status-warn">这份规则包含高级配置；请使用 JSON 编辑，当前表单不会覆盖它。</p>
		</div>
		<div v-show="view !== 'rules'">
			<div class="editor-actions">
				<button :disabled="!canPreview" @click="previewCurrent()">{{ context ? '检查当前详情页字段' : '预览当前列表/单页' }}</button
				><button v-if="context" :disabled="!available || working || localDirty || context.uncertain" @click="returnToList">返回列表并重新验证</button
				><button v-if="context" :disabled="working || blocked" @click="forgetContext">清除路径上下文</button>
			</div>
			<p v-if="context" class="selection-scope">
				列表记录 {{ (context.recordIndex || 0) + 1 }} · 列表地址：{{ context.listURL }} · {{ context.strategy
				}}{{ context.uncertain ? ' · 上次动作未确认，请先查询回执并检查页面' : '' }}
			</p>
			<section v-if="preview" class="record-preview-results">
				<strong
					>{{ preview.stage === 'detail' ? '详情' : '当前页' }}预览 · {{ preview.records.length }} 条{{
						preview.truncated ? '（有界截取）' : ''
					}}</strong
				>
				<p class="muted">revision {{ previewRevision }} · {{ preview.current_url }} · 正式写入 0 条</p>
				<p v-if="plan.max_records > 20" class="muted">编辑器预览最多取前 20 条样例，正式采集上限仍为 {{ plan.max_records }} 条。</p>
				<p v-if="previewStale" class="status-warn">页面或草稿已变化，以下是过期预览。重新检查后才可继续访问路径。</p>
				<div class="record-preview-table">
					<table>
						<thead>
							<tr>
								<th>记录</th>
								<th v-for="name in previewNames" :key="name">{{ name }}</th>
							</tr>
						</thead>
						<tbody>
							<tr v-for="record in preview.records" :key="record.index">
								<td>{{ record.index + 1 }}</td>
								<td v-for="name in previewNames" :key="name">
									<button class="field-source" :disabled="previewStale" @click="highlightField(record.fields.find((f) => f.name === name))">
										{{ record.values[name] || '—' }}</button
									><small>{{ diagnostic(record.fields.find((f) => f.name === name)) }}</small>
								</td>
							</tr>
						</tbody>
					</table>
				</div>
				<h4 v-if="preview.stage === 'list' && plan.detail">详情候选（最多 {{ plan.detail.max_details }} 个）</h4>
				<p v-if="preview.stage === 'list' && plan.detail && !preview.detail_urls.length" class="status-warn">
					未发现详情链接，请检查每条记录内的规则。
				</p>
				<div v-for="(link, index) in preview.detail_urls" :key="index" class="path-candidate">
					<span>记录 {{ link.record_index + 1 }} · {{ link.url || link.raw_url || '无地址' }}</span
					><small v-if="link.error">{{ link.error }}</small
					><button
						:disabled="
							!canNavigate || !!link.error || !link.url || visited.has(link.url) || visited.size >= (plan.detail?.max_details || 0) || !!context
						"
						@click="openDetail(link.url!, link.record_index)"
					>
						{{ link.url && visited.has(link.url) ? '本轮已访问' : '打开详情并选字段' }}
					</button>
				</div>
				<h4 v-if="preview.stage === 'list' && plan.next_page">下一页入口（本轮仅验证一次）</h4>
				<p v-if="preview.stage === 'list' && plan.next_page && !preview.next_pages.length" class="muted">没有下一页入口，当前范围结束。</p>
				<div v-for="(next, index) in preview.next_pages" :key="index" class="path-candidate">
					<span>{{ next.url || '点击按钮 / 加载更多' }}{{ next.disabled ? ' · 已禁用' : '' }}</span
					><small v-if="next.error">{{ next.error }}</small
					><button
						:disabled="!canNavigate || nextVisited || !!next.error || next.disabled || !!context || preview.next_pages.length !== 1"
						@click="openNext(next)"
					>
						验证下一页
					</button>
				</div>
				<p v-for="warning in preview.warnings" :key="warning" class="muted">{{ warning }}</p>
			</section>
			<p v-if="!preview" class="muted">保存规则后，点击“预览当前列表/单页”检查提取结果。此处不写入正式数据。</p>
		</div>
		<EditorErrorNotice v-if="failureDetails" :problem="failureDetails" />
		<p v-else-if="message" class="app-error" role="status">{{ message }}</p>
	</section>
</template>

<script setup lang="ts">
import { confirmAction } from '../../utils/dialog';
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import RecordRuleEditor from './record-rule-editor.vue';
import EditorErrorNotice from './editor-error-notice.vue';
import { editorProblem, problemMessage, recordConfigProblem, type EditorProblem } from './editor-feedback';
import {
	appApi,
	type BrowserSession,
	type Collector,
	type EditorAction,
	type EditorCommand,
	type FieldRule,
	type FieldPreview,
	type RecordPlan,
	type RecordPreview,
	type SelectionRule,
	type PageHighlight,
} from './api';
const props = defineProps<{
	view?: 'rules' | 'preview' | 'all';
	draft: Collector;
	session?: BrowserSession;
	blocked: boolean;
	enabled: boolean;
	selectedStep?: Record<string, any>;
	execute: (action: Partial<EditorAction>) => Promise<EditorCommand>;
}>();
const emit = defineEmits<{
	(event: 'session-state', session: BrowserSession): void;
	(event: 'draft-saved', draft: Collector): void;
	(event: 'highlight', value: PageHighlight): void;
	(event: 'working', busy: boolean): void;
	(event: 'preview-requested'): void;
	(event: 'select', target: SelectionRule['target'], field?: FieldRule): void;
}>();
const emptyPlan = (): RecordPlan => ({ mode: 'single', max_records: 10, fields: [], detail_fields: [] });
const plan = ref<RecordPlan>(emptyPlan()),
	stepID = ref(''),
	baseRevision = ref(props.draft.revision),
	saved = ref(''),
	working = ref(false),
	message = ref('');
const preview = ref<RecordPreview>(),
	previewRevision = ref(0);
const visited = ref(new Set<string>()),
	nextVisited = ref(false);
const malformed = ref(false);
type PathContext = {
	sessionID: string;
	stepID: string;
	listURL: string;
	detailURL?: string;
	documentID?: string;
	commandID?: string;
	requestedURL?: string;
	needsConfirmation?: boolean;
	recordIndex?: number;
	strategy: 'back' | 'navigate-list';
	uncertain: boolean;
};
const context = ref<PathContext>();
const ruleEditor = ref<InstanceType<typeof RecordRuleEditor>>();
const selectionNotice = ref('');
const failureDetails = ref<EditorProblem>();
watch(message, () => { failureDetails.value = undefined; }, { flush: 'sync' });
function reportFailure(cause: any, action: string) {
	const problem = editorProblem(cause, action);
	message.value = problemMessage(problem);
	failureDetails.value = problem;
}
let fieldToReplace: { field: FieldRule; role: 'list' | 'detail' } | undefined;
const summary = computed(() => ({
	record: plan.value.mode === 'single' ? '整页一条' : plan.value.locator?.expression ? '重复容器 · 已配置' : '待配置范围',
	field: `列表 ${plan.value.fields.length} 个${plan.value.detail ? ` · 详情 ${plan.value.detail_fields?.length || 0} 个` : ''}`,
	detail: plan.value.detail ? '已配置 · 可检查路径' : '未启用 · 可选',
	next: plan.value.next_page ? '已配置 · 可检查入口' : '未启用 · 可选',
}));
let disposed = false,
	epoch = 0;
const recordSteps = computed<Record<string, any>[]>(() =>
	Array.isArray(props.draft.definition.steps) ? props.draft.definition.steps.filter((step: any) => step.type === 'record_set') : []
);
const localDirty = computed(() => JSON.stringify(plan.value) !== saved.value);
const available = computed(() => props.enabled && !!props.session?.available && !props.blocked);
const canPreview = computed(
	() =>
		available.value &&
		!working.value &&
		!localDirty.value &&
		!unsupported.value &&
		!!stepID.value &&
		baseRevision.value === props.draft.revision &&
		!selectionBlocked.value
);
const previewStale = computed(
	() =>
		!preview.value ||
		localDirty.value ||
		previewRevision.value !== props.draft.revision ||
		preview.value.page_state_id !== props.session?.page_state_id ||
		!props.session?.available
);
const canNavigate = computed(() => canPreview.value && !previewStale.value);
const previewNames = computed(() => Array.from(new Set(preview.value?.records.flatMap((row) => row.fields.map((field) => field.name)) || [])));
const unsupported = computed(
	() =>
		malformed.value ||
		Object.keys(plan.value).some((key) => !['mode', 'locator', 'max_records', 'fields', 'detail', 'detail_fields', 'next_page'].includes(key)) ||
		[...plan.value.fields, ...(plan.value.detail_fields || [])].some((field) =>
			Object.keys(field).some(
				(key) =>
					!['name', 'locator', 'extract', 'attribute', 'field_key', 'type', 'required', 'multiple', 'clean', 'regex', 'date_format'].includes(key)
			)
		) ||
		(!!plan.value.detail && Object.keys(plan.value.detail).some((key) => !['locator', 'return_strategy', 'max_details'].includes(key))) ||
		(!!plan.value.next_page && Object.keys(plan.value.next_page).some((key) => !['locator', 'kind', 'max_pages', 'wait_ms'].includes(key)))
);
const recordScope = computed(() => (plan.value.mode === 'repeated' ? plan.value.locator : undefined));
const detailPage = computed(() => !!context.value);
function samePageURL(left?: string, right?: string) {
	if (!left || !right) return false;
	try { return new URL(left).href === new URL(right).href; } catch { return false; }
}
const detailBlockReason = computed(() => {
	const path = context.value;
	if (!path) return '';
	if (path.sessionID !== props.session?.session_id) return '浏览器会话已切换，请在当前会话重新打开示例详情。';
	if (path.uncertain) return '上一次路径动作结果尚未确认。点击“检查当前详情页”查询原回执，不会重新打开网页。';
	if (path.needsConfirmation) return '详情已打开，尚未确认页面用途。点击“检查当前详情页”，核对实际网页后继续选字段。';
	if (samePageURL(path.listURL, props.session?.current_url)) return '浏览器当前仍在原列表页，不能在这里选择详情字段。请恢复目标详情页，或清除路径后从列表重新打开。';
	if (path.detailURL && !samePageURL(path.detailURL, props.session?.current_url))
		return `当前地址与已确认的详情地址不同。点击“检查当前详情页”核对，或返回列表。当前：${props.session?.current_url || '尚未同步'}；已确认：${path.detailURL}`;
	if (path.documentID && props.session?.state?.document_id && path.documentID !== props.session.state.document_id)
		return '详情文档已重新加载或切换。请检查当前详情页并确认页面用途后继续选字段。';
	return '';
});
const selectionBlocked = computed(() => !!detailBlockReason.value);
function setWorking(value: boolean) {
	working.value = value;
	emit('working', value);
}
function storageKey() {
	return `scrapio:v22:path:${props.session?.session_id || ''}:${props.draft.id}`;
}
function storeContext() {
	if (context.value) sessionStorage.setItem(storageKey(), JSON.stringify(context.value));
	else sessionStorage.removeItem(storageKey());
}
function restoreContext() {
	context.value = undefined;
	try {
		const raw = sessionStorage.getItem(storageKey());
		if (raw) {
			const value = JSON.parse(raw),
				step = recordSteps.value.find((step) => step.step_id === value.stepID);
			if (
				step &&
				value.sessionID === props.session?.session_id &&
				typeof value.listURL === 'string' &&
				value.listURL.length <= 4096 &&
				['back', 'navigate-list'].includes(value.strategy)
			) {
				context.value = value;
				stepID.value = value.stepID;
				loadPlan(step);
				saved.value = JSON.stringify(plan.value);
				baseRevision.value = props.draft.revision;
			} else message.value = '原路径步骤或会话已变化，请手动确认页面位置。';
		}
	} catch {
		message.value = '路径上下文无法恢复；请手动确认当前页面。';
	}
}
function loadPlan(step?: Record<string, any>) {
	fieldToReplace = undefined;
	selectionNotice.value = '';
	const value = step?.config;
	malformed.value =
		!!step &&
		(!value ||
			!Array.isArray(value.fields) ||
			value.fields.some((f: any) => !f || typeof f !== 'object') ||
			(value.detail_fields !== undefined &&
				(!Array.isArray(value.detail_fields) || value.detail_fields.some((f: any) => !f || typeof f !== 'object'))) ||
			!['single', 'repeated'].includes(value.mode));
	plan.value = malformed.value ? emptyPlan() : step ? JSON.parse(JSON.stringify(value)) : emptyPlan();
}
async function reload() {
	if (localDirty.value && !(await confirmAction('重新加载将丢弃未保存的记录配置，确认？'))) return;
	const step = recordSteps.value.find((step) => step.step_id === stepID.value) || recordSteps.value[0];
	stepID.value = step?.step_id || '';
	loadPlan(step);
	saved.value = JSON.stringify(plan.value);
	baseRevision.value = props.draft.revision;
	epoch++;
}
async function chooseStep(id: string) {
	if (working.value || context.value) return;
	if (localDirty.value && !(await confirmAction('切换步骤将丢弃未保存修改，确认？'))) return;
	stepID.value = id;
	ruleEditor.value?.resetNode();
	const step = recordSteps.value.find((step) => step.step_id === id);
	loadPlan(step);
	saved.value = id ? JSON.stringify(plan.value) : '';
	baseRevision.value = props.draft.revision;
	preview.value = undefined;
	epoch++;
}
function enableDetail() {
	plan.value.detail = { locator: { strategy: 'css', expression: 'a' }, return_strategy: 'back', max_details: 3 };
	plan.value.detail_fields = plan.value.detail_fields || [];
}
function applySelection(rule: SelectionRule) {
	if (selectionBlocked.value) {
		message.value = detailBlockReason.value;
		return false;
	}
	if (working.value || props.blocked || unsupported.value || (context.value && rule.target !== 'field')) return false;
	if (rule.target === 'record') {
		plan.value.mode = 'repeated';
		plan.value.locator = { ...rule.locator };
	} else if (rule.target === 'field') {
		const role = context.value ? 'detail' : 'list';
		if (fieldToReplace && fieldToReplace.role !== role) {
			message.value = '字段所属页面已变化，请返回原页面重新点选。';
			return false;
		}
		if (rule.scope) {
			if (plan.value.mode !== 'repeated' || JSON.stringify(plan.value.locator) !== JSON.stringify(rule.scope)) {
				message.value = '字段范围与记录配置不同，请先使用正确的记录范围。';
				return false;
			}
		} else if (plan.value.mode === 'repeated' && !context.value) {
			message.value = '重复记录字段必须在记录范围内点选，或手填相对定位器。';
			return false;
		}
		const fields = context.value ? plan.value.detail_fields || (plan.value.detail_fields = []) : plan.value.fields;
		const replacing = fieldToReplace?.field;
		if (replacing && !fields.includes(replacing)) {
			message.value = '原字段已变化，请重新选择要编辑的字段。';
			return false;
		}
		let suffix = fields.length + 1;
		while (fields.some((field) => field.name === `field_${suffix}`)) suffix++;
		const name = replacing?.name || rule.name || `field_${suffix}`;
		if (fields.some((field) => field !== replacing && field.name === name)) {
			message.value = '字段名称已存在，请修改名称后重新加入。';
			return false;
		}
		const field: FieldRule = {
			name,
			extract: rule.extract || 'text',
			locator: { ...rule.locator },
			...(rule.extract === 'attribute' ? { attribute: 'data-id' } : {}),
		};
		if (replacing) replacing.locator = field.locator;
		else fields.push(field);
		selectionNotice.value = `已${replacing ? '更新定位' : '添加字段'}“${name}” · 尚未保存`;
		ruleEditor.value?.focusSelection('field', replacing || fields[fields.length - 1]);
	} else if (rule.target === 'detail') {
		if (plan.value.mode === 'repeated' && JSON.stringify(plan.value.locator) !== JSON.stringify(rule.scope)) {
			message.value = '详情链接须在同一记录范围内点选。';
			return false;
		}
		if (!plan.value.detail) enableDetail();
		plan.value.detail!.locator = { ...rule.locator };
	} else
		plan.value.next_page = {
			max_pages: 10,
			wait_ms: 500,
			...plan.value.next_page,
			locator: { ...rule.locator },
			kind: rule.next_kind || plan.value.next_page?.kind || 'link',
		};
	fieldToReplace = undefined;
	if (rule.target !== 'field') {
		selectionNotice.value = `已更新${({ record: '记录范围', detail: '详情入口', next: '翻页入口' } as const)[rule.target]} · 尚未保存`;
		ruleEditor.value?.focusSelection(rule.target);
	}
	message.value = '';
	return true;
}
function beginSelection(target: SelectionRule['target'], field?: FieldRule) {
	if (working.value || props.blocked || unsupported.value || !available.value || selectionBlocked.value || (context.value && target !== 'field')) return;
	prepareSelection(target, field);
	emit('select', target, field);
}
function prepareSelection(target: SelectionRule['target'], field?: FieldRule) {
	fieldToReplace = target === 'field' && field ? { field, role: context.value ? 'detail' : 'list' } : undefined;
}
async function inspectPath(_target: 'detail' | 'next') {
	if (!canPreview.value) return;
	emit('preview-requested');
	await previewCurrent();
}
function validate() {
	return !recordConfigProblem(plan.value);
}
function undoLocal() {
	if (working.value || props.blocked || !localDirty.value) return;
	plan.value = saved.value ? JSON.parse(saved.value) : emptyPlan();
	selectionNotice.value = '';
	fieldToReplace = undefined;
	preview.value = undefined;
	message.value = '已撤销本地规则修改。';
}
async function saveAndPreview() {
	if (working.value || props.blocked || unsupported.value || !available.value) return;
	if (!localDirty.value || (await save())) {
		emit('preview-requested');
		await nextTick();
		await previewCurrent();
	}
}
async function save() {
	if (working.value || props.blocked || unsupported.value || !localDirty.value) return;
	if (!validate()) {
		const issue = recordConfigProblem(plan.value)!;
		reportFailure({ error: { code: 'INVALID_RECORD_CONFIG', message: issue.message, stage: 'save', path: issue.path } }, '保存采集规则');
		return;
	}
	if (!Array.isArray(props.draft.definition.steps)) {
		message.value = '高级步骤结构请使用 JSON 编辑。';
		return;
	}
	setWorking(true);
	message.value = '';
	try {
		const id = stepID.value || `records-${crypto.randomUUID()}`;
		const previous = props.draft.definition.steps.find((s: any) => s.step_id === id);
		for (const field of [...plan.value.fields, ...(plan.value.detail_fields || [])]) field.field_key ||= `field-${crypto.randomUUID()}`;
		const next = { ...previous, step_id: id, type: 'record_set', config: JSON.parse(JSON.stringify(plan.value)) };
		const steps = previous ? props.draft.definition.steps.map((s: any) => (s.step_id === id ? next : s)) : [...props.draft.definition.steps, next];
		const draft = await appApi.updateCollector(props.draft.id, {
			name: props.draft.name,
			expected_revision: baseRevision.value,
			definition: { ...props.draft.definition, steps },
		}, true);
		if (disposed) return;
		stepID.value = id;
		baseRevision.value = draft.revision;
		saved.value = JSON.stringify(plan.value);
		emit('draft-saved', draft);
		message.value = '记录步骤已保存；请重新检查当前页面。';
		selectionNotice.value = '';
		return true;
	} catch (cause: any) {
		reportFailure(cause, '保存采集规则');
	} finally {
		setWorking(false);
	}
}
function diagnostic(field?: FieldPreview) {
	return field ? `${field.truncated ? '至少 ' : ''}${field.match_count} 个命中${field.errors.length ? ' · ' + field.errors.join(' / ') : ''}` : '';
}
function highlightField(field?: FieldPreview) {
	if (!preview.value || previewStale.value || !field?.source) return;
	emit('highlight', {
		page_state_id: preview.value.page_state_id,
		kind: 'selected',
		elements: [{ ...field.source, tag: '', text: '', attributes: {}, parent_id: '', child_count: 0, boundary: '' }],
	});
}
async function readPreview(stage: 'list' | 'detail') {
	const session = props.session;
	if (!session) throw new Error('会话不可用');
	const revision = props.draft.revision,
		currentEpoch = epoch;
	const result = await appApi.previewRecords(session.session_id, session.page_state_id, revision, stepID.value, stage);
	if (
		disposed ||
		currentEpoch !== epoch ||
		session.session_id !== props.session?.session_id ||
		revision !== props.draft.revision ||
		result.page_state_id !== props.session?.page_state_id
	)
		throw new Error('页面或草稿已变化，丢弃过期预览');
	preview.value = result;
	previewRevision.value = revision;
	return result;
}
async function previewCurrent(action = context.value ? '检查详情页字段' : '检查列表记录与详情入口') {
	if (!canPreview.value) return;
	setWorking(true);
	previewRevision.value = 0;
	message.value = '';
	try {
		await readPreview(context.value ? 'detail' : 'list');
		return true;
	} catch (cause: any) {
		reportFailure(cause, action);
		return false;
	} finally {
		setWorking(false);
	}
}
async function execute(action: Partial<EditorAction>) {
	const result = await props.execute({ ...action, page_state_id: action.page_state_id || props.session?.page_state_id });
	if (result.status !== 'succeeded') throw {
		command_id: result.command_id,
		error: {
			code: result.status === 'failed' ? result.result.error_code || 'COMMAND_FAILED' : 'OUTCOME_UNCERTAIN',
			message: result.status === 'failed' ? result.result.error || '网页动作执行失败，路径已停止。' : '动作结果尚未确认，路径已停止。',
			stage: `command:${action.type}`,
		},
	};
	if (result.result.final_url) {
		const destination = new URL(result.result.final_url);
		if (!['http:', 'https:'].includes(destination.protocol) || destination.username || destination.password)
			throw new Error('动作已完成，但当前地址无法用于网页采集，请检查实际页面。');
	}
	return result;
}
async function readDetailFields() {
	const path = context.value;
	if (disposed || !path || path.uncertain || path.sessionID !== props.session?.session_id || path.stepID !== stepID.value || !samePageURL(path.detailURL, props.session?.current_url)) return false;
	if (path.needsConfirmation) {
		const page = props.session?.page_state_id;
		if (!(await confirmAction(`网页已打开并发生跨域跳转。\n原地址：${path.requestedURL}\n实际地址：${path.detailURL}\n请核对网页内容；确认这是需要的详情页后继续选字段。`, '确认详情页面'))) {
			message.value = '详情已打开，尚未确认页面用途；可核对后继续选字段，或返回列表。';
			return false;
		}
		if (disposed || context.value !== path || props.session?.session_id !== path.sessionID ||
			props.session?.page_state_id !== page || !samePageURL(props.session?.current_url, path.detailURL))
			throw new Error('确认期间网页或会话已变化，请核对当前页面后继续。');
		path.needsConfirmation = false;
		storeContext();
	}
	await readPreview('detail');
	message.value = '已打开示例详情，请在网页中选择详情字段；保存后可返回列表继续配置。';
	return true;
}
async function resumeDetailFields() {
	if (!available.value || working.value || localDirty.value || !context.value?.needsConfirmation) return;
	setWorking(true);
	let ready = false;
	try { ready = await readDetailFields(); }
	catch (cause) { reportFailure(cause, '确认详情页面并检查字段'); }
	finally { setWorking(false); }
	if (ready && !selectionBlocked.value) {
		ruleEditor.value?.focusSelection('detail');
		beginSelection('field');
	}
}
async function recoverDetailPage() {
	const path = context.value, session = props.session;
	if (!path || !session || disposed || working.value || !available.value || localDirty.value) return;
	setWorking(true);
	previewRevision.value = 0;
	message.value = '';
	let ready = false;
	try {
		// Query only; never replay a navigation whose outcome is unknown. Older
		// stored contexts have no command ID, so inspect the session's receipts.
		if (path.commandID) await appApi.browserCommand(session.session_id, path.commandID);
		const commands = (await appApi.browserCommands(session.session_id, true)).items;
		const unresolved = commands.filter(command => ['queued', 'running', 'uncertain'].includes(command.status));
		if (unresolved.length) {
			const receipt = await appApi.browserCommand(session.session_id, unresolved[0].command_id);
			if (unresolved.length > 1 || ['queued', 'running', 'uncertain'].includes(receipt.status))
				throw new Error('原网页动作仍在执行或结果未知，请先到操作记录核对该回执；本次没有重新派发动作。');
		}
		const state = await appApi.browserSession(session.session_id, true);
		if (disposed || context.value !== path || props.session?.session_id !== session.session_id) return;
		if (state.session_id !== path.sessionID) throw new Error('返回的会话与原详情路径不一致，请重新打开当前会话。');
		emit('session-state', state);
		await nextTick();
		if (!state.available || props.session?.page_state_id !== state.page_state_id || !samePageURL(props.session?.current_url, state.current_url))
			throw new Error('当前浏览器状态尚未同步，请更新会话后再次检查；没有重新打开详情。');
		const destination = new URL(state.current_url);
		if (!['http:', 'https:'].includes(destination.protocol) || destination.username || destination.password)
			throw new Error('当前地址无法用于网页采集，请先恢复目标详情页。');
		if (samePageURL(state.current_url, path.listURL))
			throw new Error('浏览器当前仍在原列表页，不能确认成详情页。请恢复目标详情页，或清除路径后从列表重新打开。');
		if (!(await confirmAction(`当前网页：${state.current_url}\n原列表：${path.listURL}\n请核对画面，确认当前网页是需要配置字段的详情页。此操作不会重新访问网站。`, '确认当前详情页'))) return;
		if (disposed || context.value !== path || props.session?.session_id !== session.session_id ||
			props.session?.page_state_id !== state.page_state_id || !samePageURL(props.session?.current_url, state.current_url))
			throw new Error('确认期间页面已变化，请重新检查当前详情页。');
		path.detailURL = state.current_url;
		path.documentID = state.state?.document_id;
		path.uncertain = false;
		path.needsConfirmation = false;
		storeContext();
		ready = await readDetailFields();
	} catch (cause) { reportFailure(cause, '检查并恢复当前详情页'); }
	finally { setWorking(false); }
	if (ready && !selectionBlocked.value) {
		ruleEditor.value?.focusSelection('detail');
		beginSelection('field');
	}
}
async function openDetail(url: string, recordIndex = 0) {
	if (!canNavigate.value || !plan.value.detail || context.value || visited.value.size >= plan.value.detail.max_details) return;
	if (preview.value?.stage !== 'list' || !preview.value.detail_urls.some(link => link.url === url && link.record_index === recordIndex && !link.error)) return;
	const pageState = preview.value!.page_state_id,
		sessionID = props.session?.session_id;
	if (!(await confirmAction(`打开示例详情 ${url}，成功后在详情页选择字段；不会写正式数据。确认访问？`))) return;
	if (!canNavigate.value || sessionID !== props.session?.session_id || pageState !== props.session?.page_state_id || disposed || context.value) return;
	const session = props.session!;
	context.value = {
		sessionID: session.session_id,
		stepID: stepID.value,
		listURL: preview.value!.current_url,
		recordIndex,
		strategy: plan.value.detail.return_strategy,
		uncertain: true,
	};
	storeContext();
	setWorking(true);
	let ready = false;
	let phase = '打开示例详情';
	try {
		const command = await execute({ type: 'navigate', value: url, timeout_ms: 10000, page_state_id: pageState });
		await nextTick();
		if (sessionID !== props.session?.session_id || context.value?.sessionID !== sessionID) throw new Error('会话已切换，请在当前会话重新打开示例详情。');
		context.value.detailURL = command.result.final_url || url;
		context.value.commandID = command.command_id;
		context.value.requestedURL = url;
		// A successful receipt is known even if the state event is still behind.
		context.value.uncertain = false;
		storeContext();
		if (!samePageURL(props.session?.current_url, context.value.detailURL)) {
			context.value.needsConfirmation = true;
			storeContext();
			message.value = '详情动作已成功，当前网页地址尚未同步。点击“检查当前详情页”刷新状态后继续，不会重复导航。';
			return;
		}
		if (context.value.detailURL.split('#')[0] === context.value.listURL.split('#')[0]) throw new Error('打开详情后仍停留在列表地址，请检查入口或网站重定向，不能在列表中选择详情字段。');
		context.value.uncertain = false;
		context.value.documentID = props.session?.state?.document_id;
		context.value.requestedURL = url;
		const requested = new URL(url), destination = new URL(context.value.detailURL);
		// HTTPS upgrades on the same host are routine; a different origin needs
		// a page-purpose decision, not a false navigation failure.
		context.value.needsConfirmation = requested.origin !== destination.origin &&
			!(requested.protocol === 'http:' && destination.protocol === 'https:' && requested.hostname === destination.hostname && !requested.port && !destination.port);
		storeContext();
		visited.value.add(url);
		phase = '检查已打开详情页的字段';
		ready = await readDetailFields();
	} catch (cause: any) {
		if (context.value?.sessionID === sessionID && cause?.command_id) {
			context.value.commandID = cause.command_id;
			storeContext();
		}
		reportFailure(cause, phase);
	} finally {
		setWorking(false);
	}
	if (ready && !selectionBlocked.value) {
		ruleEditor.value?.focusSelection('detail');
		beginSelection('field');
	}
}
async function openExampleDetail() {
	if (!available.value || working.value || unsupported.value || !plan.value.detail || context.value) return;
	if (localDirty.value && !(await save())) return;
	await nextTick();
	if (!canPreview.value) return;
	if (!(await previewCurrent('打开详情前检查列表记录与详情入口'))) return;
	if (!canNavigate.value || preview.value?.stage !== 'list') return;
	const candidate = preview.value.detail_urls.find(link => link.url && !link.error && !visited.value.has(link.url));
	if (!candidate?.url) {
		message.value = '没有可打开的示例详情，请检查详情入口规则或在候选列表中核对地址。';
		emit('preview-requested');
		return;
	}
	await openDetail(candidate.url, candidate.record_index);
}
async function returnToList() {
	if (!context.value || !available.value || working.value || localDirty.value || context.value.uncertain) return;
	const path = { ...context.value },
		pageState = props.session!.page_state_id;
	if (!(await confirmAction(`按 ${path.strategy} 返回列表并重新验证记录范围，确认？`))) return;
	if (disposed || !available.value || path.sessionID !== props.session?.session_id || pageState !== props.session?.page_state_id || working.value || localDirty.value) return;
	setWorking(true);
	context.value.uncertain = true;
	storeContext();
	try {
		const command = await execute(
			path.strategy === 'back'
				? { type: 'back', timeout_ms: 10000, page_state_id: pageState }
				: { type: 'navigate', value: path.listURL, timeout_ms: 10000, page_state_id: pageState }
		);
		if (context.value) {
			context.value.commandID = command.command_id;
			storeContext();
		}
		const result = await readPreview('list');
		if (new URL(result.current_url).href !== new URL(path.listURL).href || !result.records.length)
			throw new Error('返回后的地址或记录范围不匹配，请手动恢复列表');
		context.value = undefined;
		storeContext();
		message.value = '已返回列表并重新发现记录，可继续选择下一条详情。';
	} catch (cause: any) {
		if (context.value?.sessionID === path.sessionID && cause?.command_id) {
			context.value.commandID = cause.command_id;
			storeContext();
		}
		reportFailure(cause, '返回列表并检查记录范围');
	} finally {
		setWorking(false);
	}
}
async function openNext(next: RecordPreview['next_pages'][number]) {
	if (!canNavigate.value || nextVisited.value || context.value || !plan.value.next_page) return;
	const pageState = preview.value!.page_state_id,
		sessionID = props.session?.session_id;
	if (!(await confirmAction('本轮只访问一次下一页/点击加载更多，再检查记录范围；不会连续翻页。确认？'))) return;
	if (disposed || !canNavigate.value || sessionID !== props.session?.session_id || pageState !== props.session?.page_state_id || context.value || nextVisited.value) return;
	nextVisited.value = true;
	setWorking(true);
	try {
		await execute(
			next.kind === 'link'
				? { type: 'navigate', value: next.url, timeout_ms: 10000, page_state_id: pageState }
				: { type: 'click', locator: plan.value.next_page.locator, timeout_ms: 10000, page_state_id: pageState }
		);
		const result = await readPreview('list');
		message.value = result.records.length ? '下一页记录范围已检查，本轮到此停止。' : '下一页没有记录，本轮结束。';
	} catch (cause: any) {
		reportFailure(cause, '访问并检查下一页');
	} finally {
		setWorking(false);
	}
}
async function forgetContext() {
	if ((await confirmAction('清除仅删除路径上下文，不移动网页；请先查询未确认动作并确认页面位置。确认？'))) {
		context.value = undefined;
		storeContext();
		preview.value = undefined;
	}
}
watch(
	() => props.draft.revision,
	async () => {
		epoch++;
		if (!localDirty.value && !working.value) await reload();
	}
);
watch(
	() => props.selectedStep?.step_id,
	async () => {
		if (props.selectedStep?.type === 'record_set') await chooseStep(props.selectedStep.step_id);
	}
);
watch(
	() => props.session?.session_id,
	() => {
		epoch++;
		visited.value = new Set();
		nextVisited.value = false;
		preview.value = undefined;
		restoreContext();
	}
);
onMounted(async () => {
	saved.value = JSON.stringify(plan.value);
	await reload();
	restoreContext();
});
onBeforeUnmount(() => {
	disposed = true;
	epoch++;
});
async function focusField(id: string, key: string, role: string) {
	if (id !== stepID.value) {
		await chooseStep(id);
		await nextTick();
	}
	if (id !== stepID.value) return;
	await ruleEditor.value?.focusField(key, role);
}
defineExpose({ applySelection, localDirty, recordScope, detailPage, selectionBlocked, detailBlockReason, recoverDetailPage, returnToList, forgetContext, focusField, summary, prepareSelection, selectionError: message });
</script>
