<template>
	<section class="record-designer">
		<div class="pane-heading">
			<h3>记录与详情路径</h3>
			<span>编辑会话 · 有界检查</span>
		</div>
		<p class="muted">配置并保存记录范围和字段后，检查当前页面的实际值。打开详情/下一页会访问网站；检查本身不写正式记录。</p>
		<div class="editor-actions">
			<select :value="stepID" :disabled="working || !!context" aria-label="记录步骤" @change="chooseStep(($event.target as HTMLSelectElement).value)">
				<option value="">新记录步骤</option>
				<option v-for="step in recordSteps" :key="step.step_id" :value="step.step_id">{{ step.step_id }}</option></select
			><button :disabled="working || blocked" @click="reload">重新读取草稿</button>
		</div>
		<fieldset :disabled="working || blocked || unsupported" class="record-rule-form">
			<div class="command-form-grid">
				<label
					>记录方式<select v-model="plan.mode" @change="changeMode">
						<option value="single">整页一条记录</option>
						<option value="repeated">重复容器，每项一条</option>
					</select></label
				><label>本页 / 累积列表记录上限<input v-model.number="plan.max_records" type="number" min="1" max="200" /></label>
			</div>
			<div v-if="plan.mode === 'repeated' && plan.locator" class="command-form-grid">
				<label
					>容器定位<select v-model="plan.locator.strategy">
						<option value="css">CSS</option>
						<option value="xpath">XPath</option>
					</select></label
				><label>容器规则<input v-model="plan.locator.expression" maxlength="2048" /></label>
			</div>
			<template v-for="group in fieldGroups" :key="group.role">
				<div class="pane-heading">
					<h4>{{ group.role === 'list' ? '当前页 / 每条记录的字段' : '详情页字段' }}</h4>
					<button @click="addField(group.role)">添加字段</button>
				</div>
				<div v-for="(field, index) in group.fields" :key="index" class="record-field-rule">
					<input v-model="field.name" aria-label="字段名" placeholder="字段名" maxlength="64" />
					<select v-model="field.extract" aria-label="字段内容">
						<option value="text">文本</option>
						<option value="link">链接</option>
						<option value="image">图片地址</option>
						<option value="attribute">属性</option>
					</select>
					<input v-if="field.extract === 'attribute'" v-model="field.attribute" aria-label="属性名" placeholder="例如 data-id" />
					<template v-if="field.locator"
						><select v-model="field.locator.strategy" aria-label="字段定位">
							<option value="css">CSS</option>
							<option value="xpath">XPath</option></select
						><input
							v-model="field.locator.expression"
							aria-label="字段规则"
							placeholder="相对容器规则，如 .title 或 .//a"
							maxlength="2048" /></template
					><span v-else>记录容器自身</span>
					<button @click="field.locator = field.locator ? undefined : { strategy: 'css', expression: '' }">
						{{ field.locator ? '改用容器自身' : '指定子元素' }}</button
					><button @click="group.fields.splice(index, 1)">删除</button
					><button :disabled="index === 0" @click="moveField(group.fields, index, -1)">↑</button
					><button :disabled="index === group.fields.length - 1" @click="moveField(group.fields, index, 1)">↓</button
					><FieldOptions :field="field" :role="group.role" />
				</div>
			</template>
			<div class="editor-actions">
				<button v-if="!plan.detail" @click="enableDetail">添加详情路径</button
				><button
					v-else
					@click="
						plan.detail = undefined;
						plan.detail_fields = [];
					"
					:disabled="!!context"
				>
					移除详情路径
				</button>
			</div>
			<div v-if="plan.detail" class="detail-path-config">
				<h4>每条记录 → 发现详情 URL → 打开详情 → 提取详情字段 → 返回列表</h4>
				<div class="command-form-grid">
					<label
						>详情链接定位<select v-model="plan.detail.locator.strategy">
							<option value="css">CSS</option>
							<option value="xpath">XPath</option>
						</select></label
					><label>相对链接规则<input v-model="plan.detail.locator.expression" maxlength="2048" /></label
					><label
						>返回列表<select v-model="plan.detail.return_strategy">
							<option value="back">浏览器后退</option>
							<option value="navigate-list">重新打开列表地址</option>
						</select></label
					><label>最多详情候选<input v-model.number="plan.detail.max_details" type="number" min="1" max="5" /></label>
				</div>
			</div>
			<div class="editor-actions">
				<button
					v-if="!plan.next_page"
					@click="plan.next_page = { locator: { strategy: 'css', expression: '' }, kind: 'link', max_pages: 10, wait_ms: 500 }"
				>
					添加下一页入口</button
				><button v-else @click="plan.next_page = undefined">移除下一页入口</button>
			</div>
			<div v-if="plan.next_page" class="command-form-grid">
				<label
					>下一页类型<select v-model="plan.next_page.kind">
						<option value="link">链接导航</option>
						<option value="click">翻页按钮</option>
						<option value="load_more">加载更多（累积列表）</option>
					</select></label
				><label
					>定位方式<select v-model="plan.next_page.locator.strategy">
						<option value="css">CSS</option>
						<option value="xpath">XPath</option>
					</select></label
				><label>规则<input v-model="plan.next_page.locator.expression" maxlength="2048" /></label
				><label>最多列表轮次<input v-model.number="plan.next_page.max_pages" type="number" min="1" max="100" /></label
				><label>动作后等待（毫秒）<input v-model.number="plan.next_page.wait_ms" type="number" min="0" max="10000" /></label>
			</div>
			<p class="muted">
				字段定位相对于各自记录容器；详情字段相对于整页。当前页面检查不连续运行。正式运行遵循已发布轮次上限与运行预算；加载更多的记录上限须能覆盖累积列表，超出会标记受限。重复/空页自动停止；动作后等待用于异步列表加载。
			</p>
			<button :disabled="!localDirty || unsupported" @click="save">{{ working ? '处理中…' : '保存记录步骤' }}</button>
		</fieldset>
		<p v-if="localDirty" class="status-warn">记录配置尚未保存；保存前不会用旧规则生成新预览。</p>
		<p v-if="unsupported" class="status-warn">这份规则包含高级配置；请使用 JSON 编辑，当前表单不会覆盖它。</p>
		<div class="editor-actions">
			<button :disabled="!canPreview" @click="previewCurrent">{{ context ? '检查当前详情页字段' : '预览当前列表/单页' }}</button
			><button v-if="context" :disabled="!available || working || context.uncertain" @click="returnToList">返回列表并重新验证</button
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
					{{ link.url && visited.has(link.url) ? '本轮已访问' : '打开并检查详情' }}
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
		<p v-if="message" class="app-error" role="status">{{ message }}</p>
	</section>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import FieldOptions from './field-options.vue';
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
	draft: Collector;
	session?: BrowserSession;
	blocked: boolean;
	enabled: boolean;
	selectedStep?: Record<string, any>;
	execute: (action: Partial<EditorAction>) => Promise<EditorCommand>;
}>();
const emit = defineEmits<{
	(event: 'draft-saved', draft: Collector): void;
	(event: 'highlight', value: PageHighlight): void;
	(event: 'working', busy: boolean): void;
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
	recordIndex?: number;
	strategy: 'back' | 'navigate-list';
	uncertain: boolean;
};
const context = ref<PathContext>();
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
		!context.value?.uncertain
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
const fieldGroups = computed(() => [
	{ role: 'list' as const, fields: plan.value.fields },
	...(plan.value.detail ? [{ role: 'detail' as const, fields: plan.value.detail_fields || [] }] : []),
]);
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
function reload() {
	if (localDirty.value && !window.confirm('重新加载将丢弃未保存的记录配置，确认？')) return;
	const step = recordSteps.value.find((step) => step.step_id === stepID.value) || recordSteps.value[0];
	stepID.value = step?.step_id || '';
	loadPlan(step);
	saved.value = JSON.stringify(plan.value);
	baseRevision.value = props.draft.revision;
	epoch++;
}
function chooseStep(id: string) {
	if (working.value || context.value) return;
	if (localDirty.value && !window.confirm('切换步骤将丢弃未保存修改，确认？')) return;
	stepID.value = id;
	const step = recordSteps.value.find((step) => step.step_id === id);
	loadPlan(step);
	saved.value = id ? JSON.stringify(plan.value) : '';
	baseRevision.value = props.draft.revision;
	preview.value = undefined;
	epoch++;
}
function changeMode() {
	plan.value.locator = plan.value.mode === 'repeated' ? plan.value.locator || { strategy: 'css', expression: '' } : undefined;
}
function addField(role: 'list' | 'detail') {
	const fields = role === 'detail' ? plan.value.detail_fields || (plan.value.detail_fields = []) : plan.value.fields;
	fields.push({ name: `field_${fields.length + 1}`, extract: 'text', locator: { strategy: 'css', expression: '' } });
}
function enableDetail() {
	plan.value.detail = { locator: { strategy: 'css', expression: 'a' }, return_strategy: 'back', max_details: 3 };
	plan.value.detail_fields = plan.value.detail_fields || [];
}
function applySelection(rule: SelectionRule) {
	if (working.value || props.blocked || unsupported.value) return;
	if (rule.target === 'record') {
		plan.value.mode = 'repeated';
		plan.value.locator = { ...rule.locator };
	} else if (rule.target === 'field') {
		if (rule.scope) {
			if (plan.value.mode !== 'repeated' || JSON.stringify(plan.value.locator) !== JSON.stringify(rule.scope)) {
				message.value = '字段范围与记录配置不同，请先保存正确的记录范围。';
				return;
			}
		} else if (plan.value.mode === 'repeated' && !context.value) {
			message.value = '重复记录字段必须在记录范围内点选，或手填相对定位器。';
			return;
		}
		const fields = context.value ? plan.value.detail_fields || (plan.value.detail_fields = []) : plan.value.fields;
		fields.push({
			name: `field_${fields.length + 1}`,
			extract: rule.extract || 'text',
			locator: { ...rule.locator },
			...(rule.extract === 'attribute' ? { attribute: 'data-id' } : {}),
		});
	} else if (rule.target === 'detail') {
		if (plan.value.mode === 'repeated' && JSON.stringify(plan.value.locator) !== JSON.stringify(rule.scope)) {
			message.value = '详情链接须在同一记录范围内点选。';
			return;
		}
		enableDetail();
		plan.value.detail!.locator = { ...rule.locator };
	} else plan.value.next_page = { locator: { ...rule.locator }, kind: 'link', max_pages: 10, wait_ms: 500 };
	message.value = '已填入点选规则，检查后保存记录步骤。';
}
function validate() {
	const names = (fields: FieldRule[]) =>
		fields.every((f, i) => f.name && fields.findIndex((x) => x.name === f.name) === i && (!f.locator || f.locator.expression.trim()));
	return (
		['single', 'repeated'].includes(plan.value.mode) &&
		Number.isInteger(plan.value.max_records) &&
		plan.value.max_records >= 1 &&
		plan.value.max_records <= 20 &&
		names(plan.value.fields) &&
		names(plan.value.detail_fields || []) &&
		(plan.value.mode !== 'repeated' || !!plan.value.locator?.expression.trim())
	);
}
async function save() {
	if (working.value || props.blocked || unsupported.value || !localDirty.value) return;
	if (!validate()) {
		message.value = '请检查记录预算、字段名称和定位器。';
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
		});
		if (disposed) return;
		stepID.value = id;
		baseRevision.value = draft.revision;
		saved.value = JSON.stringify(plan.value);
		emit('draft-saved', draft);
		message.value = '记录步骤已保存；请重新检查当前页面。';
	} catch (cause: any) {
		message.value = cause?.error?.message || '保存失败，本地配置已保留。';
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
async function previewCurrent() {
	if (!canPreview.value) return;
	setWorking(true);
	message.value = '';
	try {
		await readPreview(context.value ? 'detail' : 'list');
	} catch (cause: any) {
		message.value = cause?.error?.message || cause?.message || '预览失败';
	} finally {
		setWorking(false);
	}
}
async function execute(action: Partial<EditorAction>) {
	const result = await props.execute({ ...action, page_state_id: action.page_state_id || props.session?.page_state_id });
	if (result.status !== 'succeeded') throw new Error('动作失败或结果未确认，路径停止；请查看动作回执');
	if (
		action.type === 'navigate' &&
		action.value &&
		result.result.final_url &&
		new URL(result.result.final_url).origin !== new URL(action.value).origin
	)
		throw new Error('页面跳转到范围外的域名，路径停止，请手动检查。');
	return result;
}
async function openDetail(url: string, recordIndex = 0) {
	if (!canNavigate.value || !plan.value.detail || context.value || visited.value.size >= plan.value.detail.max_details) return;
	const pageState = preview.value!.page_state_id,
		sessionID = props.session?.session_id;
	if (!window.confirm(`打开详情 ${url}，随后检查字段；不会写正式数据。确认访问？`)) return;
	if (!canNavigate.value || sessionID !== props.session?.session_id) return;
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
	try {
		await execute({ type: 'navigate', value: url, timeout_ms: 10000, page_state_id: pageState });
		context.value.uncertain = false;
		storeContext();
		visited.value.add(url);
		await readPreview('detail');
		message.value = '详情已检查，确认后返回列表；原列表预览已过期。';
	} catch (cause: any) {
		message.value = cause?.error?.message || cause?.message || '详情路径中断';
	} finally {
		setWorking(false);
	}
}
async function returnToList() {
	if (!context.value || !available.value || working.value || context.value.uncertain) return;
	const path = { ...context.value },
		pageState = props.session!.page_state_id;
	if (!window.confirm(`按 ${path.strategy} 返回列表并重新验证记录范围，确认？`)) return;
	if (!available.value || path.sessionID !== props.session?.session_id) return;
	setWorking(true);
	context.value.uncertain = true;
	storeContext();
	try {
		await execute(
			path.strategy === 'back'
				? { type: 'back', timeout_ms: 10000, page_state_id: pageState }
				: { type: 'navigate', value: path.listURL, timeout_ms: 10000, page_state_id: pageState }
		);
		const result = await readPreview('list');
		if (new URL(result.current_url).href !== new URL(path.listURL).href || !result.records.length)
			throw new Error('返回后的地址或记录范围不匹配，请手动恢复列表');
		context.value = undefined;
		storeContext();
		message.value = '已返回列表并重新发现记录，可继续选择下一条详情。';
	} catch (cause: any) {
		message.value = cause?.error?.message || cause?.message || '返回列表失败，路径停止';
	} finally {
		setWorking(false);
	}
}
async function openNext(next: RecordPreview['next_pages'][number]) {
	if (!canNavigate.value || nextVisited.value || context.value || !plan.value.next_page) return;
	const pageState = preview.value!.page_state_id,
		sessionID = props.session?.session_id;
	if (!window.confirm('本轮只访问一次下一页/点击加载更多，再检查记录范围；不会连续翻页。确认？')) return;
	if (!canNavigate.value || sessionID !== props.session?.session_id) return;
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
		message.value = cause?.error?.message || cause?.message || '下一页检查中断，不会自动重试';
	} finally {
		setWorking(false);
	}
}
function forgetContext() {
	if (window.confirm('清除仅删除路径上下文，不移动网页；请先查询未确认动作并确认页面位置。确认？')) {
		context.value = undefined;
		storeContext();
		preview.value = undefined;
	}
}
watch(
	() => props.draft.revision,
	() => {
		epoch++;
		if (!localDirty.value && !working.value) reload();
	}
);
watch(
	() => props.selectedStep?.step_id,
	() => {
		if (props.selectedStep?.type === 'record_set') chooseStep(props.selectedStep.step_id);
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
onMounted(() => {
	saved.value = JSON.stringify(plan.value);
	reload();
	restoreContext();
});
onBeforeUnmount(() => {
	disposed = true;
	epoch++;
});
function moveField(fields: FieldRule[], index: number, offset: number) {
	const next = index + offset;
	if (next < 0 || next >= fields.length) return;
	[fields[index], fields[next]] = [fields[next], fields[index]];
}
async function focusField(id: string, key: string, role: string) {
	if (id !== stepID.value) {
		chooseStep(id);
		await nextTick();
	}
	const el = document.getElementById(`v22-field-${role}-${key}`) as HTMLDetailsElement | null;
	if (el) {
		el.open = true;
		el.scrollIntoView({ block: 'center', behavior: 'smooth' });
	}
}
defineExpose({ applySelection, localDirty, recordScope, detailPage, focusField });
</script>
