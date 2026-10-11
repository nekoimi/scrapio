<template>
	<section class="selection-panel" aria-label="元素选择与定位验证">
		<div class="pane-heading">
			<h3>{{ targetLabels[target] }}</h3>
			<span>只读检查</span>
		</div>
		<p class="muted">点选后自动检查推荐规则。核对高亮和样例，再确认使用。</p>
		<p v-if="!session?.available" class="status-warn">请先打开或恢复浏览器会话。</p>
		<p v-if="!enabled" class="status-warn">当前服务不支持元素检查，请同步升级浏览器服务。</p>
		<p v-if="blocked" class="muted" role="status">当前操作尚未结束，选择配置已保留，暂时不能检查或确认。</p>
		<p v-if="hasPending && !checkValid" class="status-warn" role="status">{{ staleReason || '规则尚未通过当前页面验证；配置已保留，请重新验证后核对样例。' }}</p>
		<div v-if="hasPending" class="editor-actions">
			<button :disabled="!available || busy || !expression.trim()" @click="validateLocator">重新验证</button>
			<button :disabled="!available || busy" @click="reselect">重新点选</button>
			<button @click="cancelSelection">取消当前选择</button>
		</div>
		<p v-if="scope" class="selection-scope">
			当前记录范围：{{ scope.expression
			}}<button
				:disabled="busy"
				@click="clearScope"
			>
				清除范围
			</button>
		</p>
		<p v-if="targetMode === 'field' && !scope" class="muted">未指定记录范围，候选定位器相对于整页；先在记录模式选择范围可验证相对字段。</p>
		<section v-if="selection" class="selection-summary">
			<strong>{{ selectionValid ? '已选中' : '上次选择（元素引用待更新）' }} &lt;{{ selection.element.tag }}&gt; · {{ selection.element.element_id }}</strong>
			<p>{{ selection.element.text || '无文本' }}</p>
			<details class="editor-advanced">
				<summary>元素属性</summary>
				<dl>
					<template v-for="(value, key) in selection.element.attributes" :key="key"
						><dt>{{ key }}</dt>
						<dd>{{ value }}</dd></template
					>
				</dl>
			</details>
			<p v-if="selection.element.boundary" class="status-warn">{{ selection.element.boundary }} 边界；当前只检查外层元素。</p>
			<div class="editor-actions">
				<button :disabled="!available || busy || !selectionValid || !selection.element.parent_id" @click="changeRange('parent')">扩大到父元素</button>
				<label>子元素序号<input v-model.number="childIndex" type="number" min="1" :max="selection.element.child_count" /></label>
				<button :disabled="!available || busy || !selectionValid || !selection.element.child_count" @click="changeRange('child')">缩小到子元素</button>
				<button :disabled="!available || busy || !selectionValid" @click="selectSimilar">选择同类</button>
			</div>
			<details class="editor-advanced">
				<summary>其他定位规则</summary>
				<div v-for="candidate in selection.locators" :key="candidate.locator.strategy + candidate.locator.expression" class="locator-candidate">
					<button :disabled="!available || busy" @click="chooseCandidate(candidate)">{{ candidate.locator.strategy }} · {{ candidate.locator.expression }}</button>
					<small>{{ candidate.truncated ? '至少 ' : '' }}{{ candidate.match_count }} 个命中 · {{ candidate.kind }}</small>
					<p v-if="candidate.warning" class="muted">{{ candidate.warning }}</p>
				</div>
			</details>
		</section>
		<details class="editor-advanced">
			<summary>高级：手动修改 CSS / XPath</summary>
			<form @submit.prevent="validateLocator">
				<div class="command-form-grid">
					<label
						>定位方式<select v-model="strategy">
							<option value="css">CSS</option>
							<option value="xpath">XPath</option>
						</select></label
					><label
						>定位规则<input v-model="expression" maxlength="2048" placeholder="例如 .item .title；相对 XPath 使用 .//"
					/></label>
				</div>
				<button :disabled="!available || busy || !expression.trim()">{{ busy ? '检查中…' : '验证定位器' }}</button>
			</form>
		</details>
		<div v-if="check" class="locator-result" aria-live="polite">
			<strong>{{ checkValid ? '当前检查' : '上次检查（待验证）' }} · {{ check.truncated ? '至少 ' : '' }}{{ check.match_count }} 个命中</strong>
			<span v-if="scope"> · {{ check.scope_match_count }} 个记录范围</span>
			<p>
				{{
					!checkValid ? '这些样例属于上次验证，不能用于当前确认。' : check.comparison === 'changed'
						? '命中身份与上次检查不同，请重新核对。'
						: check.comparison === 'unchanged'
							? '当前命中身份与上次检查一致。'
							: '当前页面检查完成；重新打开页面后需重新验证。'
				}}
			</p>
			<ul>
				<li v-for="(sample, i) in check.sample_values" :key="i">{{ sample || '无文本' }}</li>
			</ul>
			<p v-for="warning in check.warnings" :key="warning" class="muted">{{ warning }}</p>
		</div>
		<div v-if="hasPending" class="locator-result">
			<div class="editor-actions">
				<template v-if="target === 'field'"
					><label>字段名称<input v-model="fieldName" maxlength="64" placeholder="例如：电影名称；留空自动命名" /></label
					><label
						>提取内容<select v-model="extractKind">
							<option value="text">文本</option>
							<option value="link">链接</option>
							<option value="image">图片地址</option>
							<option value="attribute">属性</option>
						</select></label
					></template
				>
				<label v-if="target === 'next'"
					>翻页方式<select v-model="nextKind">
						<option value="link">下一页链接</option>
						<option value="click">点击翻页按钮</option>
						<option value="load_more">点击加载更多</option>
					</select></label
				>
				<button class="primary-action"
					:disabled="
						!available ||
						!checkValid ||
						!check?.match_count ||
						check?.truncated ||
						busy ||
						(target === 'next' && (!!scope || check?.match_count !== 1)) ||
						(detailPage && (target === 'record' || target === 'detail' || target === 'next'))
					"
					@click="saveRule(target)"
				>
					{{ target === 'field' ? '加入字段' : target === 'record' ? '使用记录范围' : target === 'detail' ? '使用详情入口' : '使用下一页入口' }}
				</button>
				<button :disabled="!available || !checkValid || !!scope || check?.match_count !== 1 || check?.truncated || busy" @click="prepareAction">准备点击动作</button>
			</div>
			<p class="muted">选择会填入下方记录配置，保存后才能预览；详情与下一页访问需明确确认。</p>
		</div>
		<details class="dom-browser" @toggle="openDOM">
			<summary>DOM 层级（截图点选的替代入口）</summary>
			<div class="editor-actions">
				<button :disabled="!available || busy" @click="loadDOM()">页面根节点</button
				><button :disabled="!available || busy || !dom?.root.parent_id" @click="loadDOM(dom?.root.parent_id)">上一层</button>
			</div>
			<template v-if="dom">
				<button :disabled="!available || busy" @click="pickElement(dom!.root.element_id)">选中当前 &lt;{{ dom.root.tag }}&gt;</button>
				<ul class="dom-node-list">
					<li v-for="node in dom.items" :key="node.element_id">
						<button :disabled="!available || busy" @click="pickElement(node.element_id)">
							&lt;{{ node.tag }}&gt; {{ node.attributes.id || node.attributes.class || node.text.slice(0, 70) || node.element_id }}</button
						><button :disabled="!available || busy || !node.child_count" @click="loadDOM(node.element_id)">进入 {{ node.child_count }} 个子元素</button>
					</li>
				</ul>
				<button v-if="dom.next_offset !== null" :disabled="!available || busy" @click="loadDOM(dom.root.element_id, dom.next_offset)">
					下一批（共 {{ dom.total }} 个）
				</button>
			</template>
		</details>
		<p v-if="message" class="app-error" role="status">{{ message }}</p>
	</section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue';
import {
	appApi,
	type BrowserSession,
	type DOMChildren,
	type EditorLocator,
	type ElementInspection,
	type InspectionInput,
	type LocatorCheck,
	type PageHighlight,
	type SelectionRule,
	type FieldRule,
} from './api';
const props = defineProps<{
	session?: BrowserSession;
	mode: 'record' | 'field';
	target: SelectionRule['target'];
	enabled: boolean;
	blocked: boolean;
	revision: number;
	recordScope?: EditorLocator;
	detailPage?: boolean;
}>();
const emit = defineEmits<{
	(event: 'highlight', value: PageHighlight): void;
	(event: 'action-locator', value: EditorLocator): void;
	(event: 'rule', value: SelectionRule): void;
	(event: 'working', value: boolean): void;
}>();
const targetLabels: Record<SelectionRule['target'], string> = {
	record: '选择记录范围',
	field: '选择字段来源',
	detail: '选择详情入口',
	next: '选择下一页',
};
const fieldName = ref('');
const nextKind = ref<'link' | 'click' | 'load_more'>('link');
const extractKind = ref<FieldRule['extract']>('text');
const selection = ref<ElementInspection>();
const check = ref<LocatorCheck>();
const dom = ref<DOMChildren>();
const scope = ref<EditorLocator>();
const strategy = ref<'css' | 'xpath'>('css');
const expression = ref('');
const childIndex = ref(1);
const busy = ref(false);
watch(busy, value => emit('working', value), { flush: 'sync' });
const message = ref('');
const available = computed(() => props.enabled && !!props.session?.available && !props.blocked);
const targetMode = computed(() => props.target === 'field' || props.target === 'detail' ? 'field' : 'record');
const hasPending = computed(() => !!selection.value || !!check.value || !!expression.value || !!fieldName.value);
const staleReason = ref('');
const selectingAgain = ref(false);
const selectionStamp = ref(''), checkStamp = ref('');
function contextStamp() {
	return JSON.stringify([props.session?.session_id, props.session?.page_state_id, props.target, !!props.detailPage, scope.value, props.revision]);
}
const selectionValid = computed(() => !!selection.value && selectionStamp.value === contextStamp());
const checkValid = computed(() => !!check.value && checkStamp.value === contextStamp()
	&& check.value.locator.strategy === strategy.value && check.value.locator.expression === expression.value.trim());
// Local, bounded drafts for four goals on the list/detail page. Never persisted as validated evidence.
type PendingSelection = {
	selection?: ElementInspection; check?: LocatorCheck; scope?: EditorLocator; scopeSource: string;
	strategy: 'css' | 'xpath'; expression: string; fieldName: string; extractKind: FieldRule['extract'];
	nextKind: 'link' | 'click' | 'load_more'; childIndex: number;
};
const pendingSelections = new Map<string, PendingSelection>();
const scopeSource = () => JSON.stringify(props.recordScope) || '';
let appliedScopeSource = scopeSource();
const defaultScope = () => props.detailPage || props.target === 'record' || props.target === 'next' ? undefined : props.recordScope ? { ...props.recordScope } : undefined;
let generation = 0,
	disposed = false,
	hoverTimer: ReturnType<typeof setTimeout> | undefined;
let hoverGeneration = 0;
let queued: { input: Partial<InspectionInput>; pageState: string; hover: boolean; epoch: number } | undefined;
const evidence = new Map<string, string>();
function highlight(elements: PageHighlight['elements'], pageState: string, kind: PageHighlight['kind']) {
	emit('highlight', { elements, page_state_id: pageState, kind });
}
function stopInspection() {
	generation++;
	queued = undefined;
	leaveFrame();
}
function hideEvidence() {
	highlight([], props.session?.page_state_id || '', 'selected');
	highlight([], props.session?.page_state_id || '', 'similar');
	highlight([], props.session?.page_state_id || '', 'hover');
}
function invalidateEvidence(reason: string) {
	stopInspection();
	selectionStamp.value = checkStamp.value = '';
	dom.value = undefined;
	staleReason.value = reason;
	hideEvidence();
}
function cancelSelection() {
	invalidateEvidence('');
	selection.value = check.value = undefined;
	expression.value = fieldName.value = message.value = '';
	strategy.value = 'css'; extractKind.value = 'text'; nextKind.value = 'link'; childIndex.value = 1;
	selectingAgain.value = true;
}
function clearScope() {
	scope.value = undefined;
	invalidateEvidence('记录范围已调整，请重新验证规则。');
}
function reselect() {
	invalidateEvidence('请在当前画面重新点选并核对规则。');
	selectingAgain.value = true;
	message.value = '请在当前画面重新点选；字段名称和提取设置会保留。';
}
function accept(id: string, state: string, epoch: number) {
	return !disposed && epoch === generation && id === props.session?.session_id && state === props.session?.page_state_id && available.value;
}
function failure(cause: any, hover = false) {
	const code = cause?.error?.code;
	const hints: Record<string, string> = {
		NO_ELEMENT: '此位置没有可选元素，请选择可见内容。',
		OUTSIDE_RECORD_SCOPE: '请选择记录范围内部的字段，或先调整记录范围。',
		RECORD_SCOPE_EMPTY: '当前记录范围未匹配元素，请重新选择记录范围。',
		ELEMENT_REFERENCE_STALE: '元素已失效，请重新点选或使用当前定位规则验证。',
		INVALID_LOCATOR: '定位规则语法无效，请检查 CSS / XPath；相对 XPath 需以点开头。',
		INSPECTION_LIMIT: '检查超过数量限制，请缩小规则范围或重新打开会话。',
		STALE_PAGE_STATE: '页面已变化，配置已保留；请更新画面后重新验证。',
		INSPECTION_BUSY: '页面暂时忙碌，请等待当前操作结束后重试。',
		COMMAND_PENDING: '上一动作结果尚未确认，请先查询回执。',
		SESSION_GONE: '会话已结束，配置已保留；恢复或重新打开后请重新验证。',
		FORBIDDEN: '当前账号没有此会话的访问权限，请检查登录账号。',
		INVALID_ARGUMENT: '检查参数、元素或规则无效；请重新点选或核对定位规则。',
	};
	if (code === 'STALE_PAGE_STATE' || code === 'SESSION_GONE' || code === 'ELEMENT_REFERENCE_STALE')
		invalidateEvidence(hints[code]);
	if (hover && ['NO_ELEMENT', 'OUTSIDE_RECORD_SCOPE', 'RECORD_SCOPE_EMPTY', 'ELEMENT_REFERENCE_STALE', 'INVALID_ARGUMENT'].includes(code)) return;
	const requestID = cause?.error?.request_id || cause?.request_id;
	message.value = (hints[code] || '检查服务暂时不可用，请确认连接后重试；当前输入已保留。') + (requestID ? `（请求 ${requestID}）` : '');
}
function drainQueued() {
	const next = queued;
	queued = undefined;
	if (next && next.epoch === generation) void inspect(next.input, next.pageState, next.hover, next.epoch);
}
async function inspect(input: Partial<InspectionInput>, pageState: string, hover = false, epoch = generation) {
	const session = props.session;
	if (!session || !available.value || !pageState || epoch !== generation || pageState !== session.page_state_id) return;
	if (busy.value) {
		if (!hover) queued = { input, pageState, hover, epoch };
		return;
	}
	busy.value = true;
	const hoverEpoch = hoverGeneration;
	let recommended: LocatorCheck | undefined;
	try {
		const result = await appApi.inspectElement(session.session_id, {
			...input,
			page_state_id: pageState,
			mode: targetMode.value,
			...(targetMode.value === 'field' && scope.value ? { scope: scope.value } : {}),
		});
		if (!accept(session.session_id, pageState, epoch)) return;
		if (hover) {
			if (hoverEpoch === hoverGeneration) highlight([result.element], pageState, 'hover');
		} else {
			const firstSelection = !hasPending.value;
			selection.value = result;
			selectionStamp.value = contextStamp();
			checkStamp.value = '';
			selectingAgain.value = false;
			check.value = undefined;
			message.value = '';
			highlight([result.element], pageState, 'selected');
			highlight([], pageState, 'similar');
			highlight([], pageState, 'hover');
			recommended =
				(props.target === 'record' || input.expand_similar
					? result.locators.find((item) => item.kind === 'similar' && item.match_count > 1 && !item.truncated)
					: undefined) || result.locators.find((item) => item.match_count > 0 && !item.truncated);
			if (firstSelection && props.target === 'field') {
				extractKind.value = result.element.tag.toLowerCase() === 'img' ? 'image' : 'text';
			}
			if (firstSelection && props.target === 'next') nextKind.value = result.element.attributes.href ? 'link' : 'click';
		}
	} catch (cause: any) {
		if (accept(session.session_id, pageState, epoch) && (!hover || hoverEpoch === hoverGeneration)) {
			if (hover) highlight([], pageState, 'hover');
			failure(cause, hover);
		}
	} finally {
		busy.value = false;
		drainQueued();
	}
	if (recommended && !busy.value && accept(session.session_id, pageState, epoch)) await chooseCandidate(recommended);
}
function framePick(x: number, y: number, state: string) {
	stopInspection();
	void inspect({ position: { x, y } }, state, false, generation);
}
function frameHover(x: number, y: number, state: string) {
	if (!available.value || busy.value || (hasPending.value && !selectingAgain.value)) return;
	if (hoverTimer) clearTimeout(hoverTimer);
	hoverGeneration++;
	const epoch = generation;
	hoverTimer = setTimeout(() => {
		if (available.value && !busy.value && (!hasPending.value || selectingAgain.value)) void inspect({ position: { x, y } }, state, true, epoch);
	}, 250);
}
function leaveFrame() {
	if (hoverTimer) clearTimeout(hoverTimer);
	hoverGeneration++;
	highlight([], props.session?.page_state_id || '', 'hover');
}
function pickElement(id: string) {
	stopInspection();
	void inspect({ element_id: id }, props.session?.page_state_id || '');
}
function changeRange(relation: 'parent' | 'child') {
	if (!selection.value || !selectionValid.value || !available.value || busy.value) return;
	if (
		relation === 'child' &&
		(!Number.isInteger(childIndex.value) || childIndex.value < 1 || childIndex.value > selection.value.element.child_count)
	) {
		message.value = '子元素序号无效';
		return;
	}
	generation++;
	void inspect({ element_id: selection.value.element.element_id, relation, child_index: childIndex.value - 1 }, selection.value.page_state_id);
}
async function chooseCandidate(candidate: LocatorCheck) {
	strategy.value = candidate.locator.strategy;
	expression.value = candidate.locator.expression;
	await validateLocator();
}
async function selectSimilar() {
	if (!selection.value || !selectionValid.value || !available.value || busy.value) return;
	const existing = selection.value.locators.find((candidate) => candidate.kind === 'similar');
	if (existing) {
		await chooseCandidate(existing);
		return;
	}
	const id = selection.value.element.element_id;
	await inspect({ element_id: id, expand_similar: true }, selection.value.page_state_id);
}
async function validateLocator() {
	const session = props.session;
	if (!session || !available.value || busy.value || !expression.value.trim()) return;
	if (hoverTimer) clearTimeout(hoverTimer);
	const epoch = ++generation,
		pageState = session.page_state_id;
	const locator: EditorLocator = { strategy: strategy.value, expression: expression.value.trim() };
	const activeScope = targetMode.value === 'field' ? scope.value : undefined;
	const key = JSON.stringify([locator, activeScope]);
	busy.value = true;
	checkStamp.value = '';
	hideEvidence();
	message.value = '';
	try {
		const result = await appApi.checkLocator(session.session_id, {
			page_state_id: pageState,
			locator,
			...(activeScope ? { scope: activeScope } : {}),
			previous_fingerprint: evidence.get(key),
		});
		if (!accept(session.session_id, pageState, epoch) || expression.value.trim() !== locator.expression || strategy.value !== locator.strategy)
			return;
		evidence.set(key, result.fingerprint);
		if (evidence.size > 64) evidence.delete(evidence.keys().next().value!);
		check.value = result;
		checkStamp.value = contextStamp();
		staleReason.value = '';
		selectingAgain.value = false;
		highlight([], pageState, result.match_count === 1 ? 'similar' : 'selected');
		highlight(result.elements, pageState, result.match_count === 1 ? 'selected' : 'similar');
	} catch (cause: any) {
		if (accept(session.session_id, pageState, epoch)) failure(cause);
	} finally {
		busy.value = false;
		drainQueued();
	}
}
function prepareAction() {
	if (available.value && !busy.value && checkValid.value && check.value && !scope.value && check.value.match_count === 1 && !check.value.truncated)
		emit('action-locator', { ...check.value.locator });
}
function saveRule(target: SelectionRule['target']) {
	if (!available.value || !checkValid.value || !check.value || !check.value.match_count || check.value.truncated || busy.value) return;
	if (target === 'next' && (scope.value || check.value.match_count !== 1)) return;
	if (props.detailPage && target !== 'field') return;
	emit('rule', {
		target,
		locator: { ...check.value.locator },
		...(targetMode.value === 'field' && scope.value ? { scope: { ...scope.value } } : {}),
		extract: extractKind.value,
		...(target === 'field' ? { name: fieldName.value.trim() } : {}),
		...(target === 'next' ? { next_kind: nextKind.value } : {}),
	});
	message.value = '已提交到下方记录配置，请核对后保存并预览。';
}
async function loadDOM(id = '', offset = 0) {
	const session = props.session;
	if (!session || !available.value || busy.value) return;
	if (hoverTimer) clearTimeout(hoverTimer);
	const epoch = ++generation,
		pageState = session.page_state_id;
	busy.value = true;
	message.value = '';
	try {
		const result = await appApi.domChildren(session.session_id, pageState, id, offset);
		if (accept(session.session_id, pageState, epoch)) dom.value = result;
	} catch (cause: any) {
		if (accept(session.session_id, pageState, epoch)) failure(cause);
	} finally {
		busy.value = false;
		drainQueued();
	}
}
function openDOM(event: Event) {
	if ((event.target as HTMLDetailsElement).open && !dom.value) void loadDOM();
}
watch(
	() => [props.session?.session_id, props.session?.page_state_id, props.session?.available, props.revision],
	() => {
		invalidateEvidence('页面、会话或草稿已变化；配置已保留，请重新验证。');
	},
	{ flush: 'sync' }
);
watch(
	() => props.blocked,
	() => {
		stopInspection();
		hideEvidence();
		if (available.value && checkValid.value && check.value) highlight(check.value.elements, props.session!.page_state_id, check.value.match_count === 1 ? 'selected' : 'similar');
	},
	{ flush: 'sync' }
);
watch(
	() => `${props.target}:${!!props.detailPage}`,
	(key, previous) => {
		if (previous) pendingSelections.set(previous, {
			selection: selection.value, check: check.value, scope: scope.value, scopeSource: appliedScopeSource,
			strategy: strategy.value, expression: expression.value, fieldName: fieldName.value,
			extractKind: extractKind.value, nextKind: nextKind.value, childIndex: childIndex.value,
		});
		const cached = pendingSelections.get(key);
		selection.value = cached?.selection; check.value = cached?.check;
		scope.value = cached && cached.scopeSource === scopeSource() ? cached.scope : defaultScope();
		appliedScopeSource = scopeSource();
		strategy.value = cached?.strategy || 'css'; expression.value = cached?.expression || ''; fieldName.value = cached?.fieldName || '';
		extractKind.value = cached?.extractKind || 'text'; nextKind.value = cached?.nextKind || 'link'; childIndex.value = cached?.childIndex || 1;
		message.value = '';
		selectingAgain.value = false;
		invalidateEvidence('已切换选择目标，请在当前页面重新验证保留的规则。');
	},
	{ immediate: true, flush: 'sync' }
);
watch(scopeSource, () => {
	scope.value = defaultScope();
	appliedScopeSource = scopeSource();
	invalidateEvidence('记录范围已更新，请重新验证。');
}, { flush: 'sync' });
watch([strategy, expression], () => {
	checkStamp.value = '';
	stopInspection();
	selectingAgain.value = false;
	hideEvidence();
}, { flush: 'sync' });
watch([fieldName, extractKind, nextKind], () => {
	selectingAgain.value = false;
	leaveFrame();
}, { flush: 'sync' });
onBeforeUnmount(() => {
	disposed = true;
	generation++;
	if (hoverTimer) clearTimeout(hoverTimer);
});
defineExpose({ framePick, frameHover, leaveFrame });
</script>
