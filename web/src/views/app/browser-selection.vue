<template>
	<section class="selection-panel" aria-label="元素选择与定位验证">
		<div class="pane-heading">
			<h3>{{ targetLabels[target] }}</h3>
			<span>只读检查</span>
		</div>
		<p class="muted">点选后自动检查推荐规则。核对高亮和样例，再确认使用。</p>
		<section v-if="target === 'record' || ((target === 'field' || target === 'detail') && scope)" class="locator-result">
			<strong>用不同记录的同类元素校准规则</strong>
			<p class="muted">先选中一条记录或字段，点击下方按钮，再点另一条记录中的同类元素。最多 5 个样本；字段和详情入口需来自不同记录。</p>
			<div class="editor-actions">
				<button :disabled="!available || busy || !selectionValid || sampleElements.length >= 5" @click="addExample">
					{{ sampleElements.length ? '继续添加同类样本' : '添加第二个样本' }}
				</button>
				<button v-if="sampleElements.length" :disabled="busy" @click="clearExamples">清空校准样本</button>
			</div>
			<p v-if="addingExample" class="status-warn">请在另一条记录上点选同类元素；当前字段名称和提取方式保留。</p>
			<ul>
				<li v-for="element in sampleElements" :key="element.element_id">
					&lt;{{ element.tag }}&gt; {{ element.text || element.attributes.href || '无文本' }}
				</li>
			</ul>
		</section>
		<p v-if="!session?.available" class="status-warn">请先打开或恢复浏览器会话。</p>
		<p v-if="!enabled" class="status-warn">当前服务不支持元素检查，请同步升级浏览器服务。</p>
		<p v-if="blocked" class="muted" role="status">
			{{
				canConfirm
					? '点选画面已过期；规则使用当前复核结果。重新点选或操作网页前，请更新画面。'
					: '当前操作尚未结束，选择配置已保留，暂时不能检查或确认。'
			}}
		</p>
		<p v-if="rechecking" class="muted" role="status">正在后台复核当前规则，字段配置保持不变…</p>
		<p v-if="hasPending && !checkValid && !rechecking" class="status-warn" role="status">
			{{ staleReason || '规则尚未通过当前页面验证；配置已保留，请重新验证后核对样例。' }}
		</p>
		<section v-if="pendingComparison" class="selection-comparison">
			<strong>规则命中需要确认</strong>
			<p>上次样例：{{ comparisonBefore.join(' / ') || '无样例' }}</p>
			<p>当前样例：{{ samplesFor(pendingComparison).join(' / ') || '无匹配' }}</p>
			<button
				:disabled="!recheckAvailable() || !evidenceStateValid(pendingComparison) || !pendingComparison.match_count || pendingComparison.truncated"
				@click="acceptComparison"
			>
				确认新的命中
			</button>
		</section>
		<div v-if="hasPending" class="editor-actions">
			<button :disabled="!available || busy || !expression.trim()" @click="validateLocator">重新验证</button>
			<button :disabled="!available || busy" @click="reselect">重新点选</button>
			<button @click="cancelSelection">取消当前选择</button>
		</div>
		<p v-if="scope" class="selection-scope">当前记录范围：{{ scope.expression }}<button :disabled="busy" @click="clearScope">清除范围</button></p>
		<p v-if="targetMode === 'field' && !scope" class="muted">未指定记录范围，候选定位器相对于整页；先在记录模式选择范围可验证相对字段。</p>
		<section v-if="selection" class="selection-summary">
			<strong
				>{{ selectionValid ? '已选中' : '上次选择（元素引用待更新）' }} &lt;{{ selection.element.tag }}&gt; ·
				{{ selection.element.element_id }}</strong
			>
			<p>{{ selection.element.text || '无文本' }}</p>
			<p v-if="selection.enclosing_link" class="muted">文字所在链接：{{ selection.enclosing_link.attributes.href }}</p>
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
				<button v-if="selection.enclosing_link && target !== 'record'" :disabled="!available || busy || !selectionValid" @click="selectEnclosingLink">
					选中所在链接
				</button>
				<button :disabled="!available || busy || !selectionValid || !selection.element.parent_id" @click="changeRange('parent')">扩大到父元素</button>
				<label>子元素序号<input v-model.number="childIndex" type="number" min="1" :max="selection.element.child_count" /></label>
				<button :disabled="!available || busy || !selectionValid || !selection.element.child_count" @click="changeRange('child')">
					缩小到子元素
				</button>
				<button :disabled="!available || busy || !selectionValid" @click="selectSimilar">选择同类</button>
			</div>
			<details class="editor-advanced">
				<summary>其他定位规则</summary>
				<div v-for="candidate in selection.locators" :key="candidate.locator.strategy + candidate.locator.expression" class="locator-candidate">
					<button :disabled="!available || busy" @click="chooseCandidate(candidate)">
						{{ candidate.locator.strategy }} · {{ candidate.locator.expression }}
					</button>
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
					><label>定位规则<input v-model="expression" maxlength="2048" placeholder="例如 .item .title；相对 XPath 使用 .//" /></label>
				</div>
				<button :disabled="!available || busy || !expression.trim()">{{ busy ? '检查中…' : '验证定位器' }}</button>
			</form>
		</details>
		<div v-if="check" class="locator-result" aria-live="polite">
			<strong>{{ checkValid ? '当前检查' : '上次检查（待验证）' }} · {{ check.truncated ? '至少 ' : '' }}{{ check.match_count }} 个命中</strong>
			<span v-if="scope"> · {{ check.scope_match_count }} 个记录范围</span>
			<p>
				{{
					!checkValid
						? '这些样例属于上次验证，不能用于当前确认。'
						: check.comparison === 'changed'
							? '命中身份与上次检查不同，请重新核对。'
							: check.comparison === 'unchanged'
								? '当前命中身份与上次检查一致。'
								: '当前页面检查完成；重新打开页面后需重新验证。'
				}}
			</p>
			<ul>
				<li v-for="(sample, i) in displayedSamples" :key="i">
					{{ sample || (showLinkSamples ? '此命中没有可提取的 HTTP(S) 链接，请选中 a 标签并重新验证' : '无文本') }}
				</li>
			</ul>
			<p v-for="warning in check.warnings" :key="warning" class="muted">{{ warning }}</p>
			<p v-if="check.sample_coverage?.selected" :class="sampleMismatch ? 'status-warn' : 'status-good'">
				校准样本覆盖 {{ check.sample_coverage.matched }} / {{ check.sample_coverage.selected }}；未覆盖全部样本时不能确认。
			</p>
			<section v-if="check.record_check">
				<strong
					>逐条核对：检查 {{ check.record_check.checked }} / {{ check.record_check.total }} 条记录 · 缺失 {{ check.record_check.missing }} · 多次命中
					{{ check.record_check.multiple }} · 空值 {{ check.record_check.empty }}</strong
				>
				<p class="muted">下表最多展示 20 条，每条最多 3 个值。点击记录或值可高亮原元素；有多个命中时请核对是否需要缩小规则或配置多值。</p>
				<div class="record-preview-table">
					<table>
						<thead>
							<tr>
								<th>记录</th>
								<th>命中</th>
								<th>实际提取值</th>
							</tr>
						</thead>
						<tbody>
							<tr v-for="row in check.record_check.rows" :key="row.index">
								<td>
									<button :disabled="!checkValid" @click="highlight([row.record], check.page_state_id, 'selected')">第 {{ row.index + 1 }} 条</button>
								</td>
								<td :class="row.match_count !== 1 ? 'status-warn' : ''">{{ row.match_count }}{{ row.truncated ? '+' : '' }}</td>
								<td>
									<button
										v-for="(value, index) in row.values"
										:key="index"
										:disabled="!checkValid"
										@click="highlight([row.elements[index]], check.page_state_id, 'selected')"
									>
										{{ value || '空值' }}</button
									><span v-if="!row.match_count">未找到字段</span>
								</td>
							</tr>
						</tbody>
					</table>
				</div>
				<p v-if="check.record_check.truncated" class="status-warn">逐条检查超过预算，请缩小记录或字段范围后验证。</p>
				<label v-if="recordRisks"
					><input
						v-model="reviewedRisks"
						type="checkbox"
						:disabled="!checkValid"
					/>已核对缺失、空值和多次命中；会在保存后的完整数据预览中继续确认可空/多值配置。</label
				>
			</section>
		</div>
		<div v-if="hasPending" class="locator-result selection-confirm-footer">
			<div class="editor-actions">
				<template v-if="target === 'field'"
					><label>字段名称<input v-model="fieldName" :readonly="replacingField" maxlength="64" placeholder="例如：电影名称；留空自动命名" /></label
					><label
						>提取内容<select v-model="extractKind" :disabled="replacingField" @change="onExtractChanged">
							<option value="text">文本</option>
							<option value="link">链接</option>
							<option value="image">图片地址</option>
							<option value="attribute">属性</option>
						</select></label
					></template
				>
				<label v-if="target === 'field' && extractKind === 'attribute'"
					>属性名称<input v-model="attributeName" :readonly="replacingField" maxlength="128" placeholder="例如 data-id" @change="validateLocator"
				/></label>
				<label v-if="target === 'next'"
					>翻页方式<select v-model="nextKind">
						<option value="link">下一页链接</option>
						<option value="click">点击翻页按钮</option>
						<option value="load_more">点击加载更多</option>
					</select></label
				>
				<button
					class="primary-action"
					:disabled="
						!canConfirm ||
						!checkValid ||
						!check?.match_count ||
						check?.truncated ||
						sampleMismatch ||
						check?.record_check?.truncated ||
						(recordRisks && !reviewedRisks) ||
						busy ||
						(target === 'detail' && !detailLinkAvailable) ||
						(target === 'next' && (!!scope || check?.match_count !== 1)) ||
						(detailPage && (target === 'record' || target === 'detail' || target === 'next'))
					"
					@click="saveRule(target)"
				>
					{{
						target === 'field'
							? replacingField
								? '更新此字段定位'
								: detailPage
									? '添加到详情字段'
									: '添加到列表字段'
							: target === 'record'
								? '使用记录范围'
								: target === 'detail'
									? '使用详情入口'
									: '使用下一页入口'
					}}
				</button>
				<button :disabled="!available || !checkValid || !!scope || check?.match_count !== 1 || check?.truncated || busy" @click="prepareAction">
					准备点击动作
				</button>
			</div>
			<p class="muted">选择会填入采集流程的记录配置，保存后才能预览；详情与下一页访问需明确确认。</p>
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
						><button :disabled="!available || busy || !node.child_count" @click="loadDOM(node.element_id)">
							进入 {{ node.child_count }} 个子元素
						</button>
					</li>
				</ul>
				<button v-if="dom.next_offset !== null" :disabled="!available || busy" @click="loadDOM(dom.root.element_id, dom.next_offset)">
					下一批（共 {{ dom.total }} 个）
				</button>
			</template>
		</details>
		<EditorErrorNotice v-if="failureDetails" :problem="failureDetails" />
		<p v-else-if="message" class="app-error" role="status">{{ message }}</p>
	</section>
</template>

<script setup lang="ts">
import EditorErrorNotice from './editor-error-notice.vue';
import { editorProblem, problemMessage, type EditorProblem } from './editor-feedback';
import { stateChange } from './browser-state';
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue';
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
	type InspectedElement,
} from './api';
const props = defineProps<{
	session?: BrowserSession;
	mode: 'record' | 'field';
	target: SelectionRule['target'];
	enabled: boolean;
	blocked: boolean;
	revision: number;
	recheckEnabled?: boolean;
	replacingField?: boolean;
	recheckBlocked?: boolean;
	frameState?: BrowserSession['state'];
	recordScope?: EditorLocator;
	detailPage?: boolean;
}>();
const emit = defineEmits<{
	(event: 'verified-state', state: NonNullable<BrowserSession['state']>): void;
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
watch(
	check,
	() => {
		reviewedRisks.value = false;
	},
	{ flush: 'sync' }
);
const dom = ref<DOMChildren>();
const scope = ref<EditorLocator>();
const strategy = ref<'css' | 'xpath'>('css');
const expression = ref('');
const childIndex = ref(1);
const attributeName = ref('');
const sampleElements = ref<InspectedElement[]>([]),
	addingExample = ref(false),
	reviewedRisks = ref(false);
const sampleMismatch = computed(
	() =>
		(sampleElements.value.length > 0 && (!check.value?.sample_coverage || check.value.sample_coverage.selected !== sampleElements.value.length)) ||
		(!!check.value?.sample_coverage && check.value.sample_coverage.matched < check.value.sample_coverage.selected)
);
const recordRisks = computed(
	() => !!check.value?.record_check && !!(check.value.record_check.missing || check.value.record_check.multiple || check.value.record_check.empty)
);
function extractionInput() {
	return {
		mode: targetMode.value,
		extract: props.target === 'detail' || (props.target === 'next' && nextKind.value === 'link') ? ('link' as const) : extractKind.value,
		...(extractKind.value === 'attribute' ? { attribute: attributeName.value } : {}),
	};
}
function addExample() {
	if (!available.value || busy.value || !selectionValid.value || !selection.value || sampleElements.value.length >= 5) return;
	if (!sampleElements.value.some((el) => el.element_id === selection.value!.element.element_id)) sampleElements.value.push(selection.value.element);
	addingExample.value = true;
	selectingAgain.value = true;
	checkStamp.value = '';
	reviewedRisks.value = false;
	message.value = '请点选另一条记录上的同类元素，平台会用共同结构重新生成候选规则。';
}
function clearExamples() {
	sampleElements.value = [];
	addingExample.value = false;
	checkStamp.value = '';
	reviewedRisks.value = false;
}
const busy = ref(false);
watch(busy, (value) => emit('working', value), { flush: 'sync' });
const message = ref('');
const failureDetails = ref<EditorProblem>();
watch(
	message,
	() => {
		failureDetails.value = undefined;
	},
	{ flush: 'sync' }
);
const canConfirm = computed(() => (props.recheckEnabled ? !!props.session?.available && !props.recheckBlocked : available.value));
const available = computed(() => props.enabled && !!props.session?.available && !props.blocked);
const targetMode = computed(() => (props.target === 'field' || props.target === 'detail' ? 'field' : 'record'));
const hasPending = computed(() => !!selection.value || !!check.value || !!expression.value || !!fieldName.value);
const rechecking = ref(false),
	pendingComparison = ref<LocatorCheck>(),
	comparisonBefore = ref<string[]>([]);
let recheckTimer: ReturnType<typeof setTimeout> | undefined;
let recheckAfter = 0,
	recheckRunning = false,
	adoptingRecheck = false;
const staleReason = ref('');
const selectingAgain = ref(false);
const selectionStamp = ref(''),
	checkStamp = ref('');
function contextStamp() {
	return JSON.stringify([
		props.session?.session_id,
		props.session?.page_state_id,
		props.session?.state?.document_id,
		props.session?.state?.frame_state_id,
		props.session?.state?.validation_state_id,
		props.target,
		!!props.detailPage,
		scope.value,
		props.revision,
	]);
}
function evidenceStateValid(result: { state?: BrowserSession['state'] }) {
	const current = props.session?.state;
	return (
		!current ||
		(!!result.state &&
			current.document_id === result.state.document_id &&
			current.frame_state_id === result.state.frame_state_id &&
			current.validation_state_id === result.state.validation_state_id)
	);
}
const selectionValid = computed(() => !!selection.value && evidenceStateValid(selection.value) && selectionStamp.value === contextStamp());
const showLinkSamples = computed(
	() =>
		(props.target === 'field' && extractKind.value === 'link') || props.target === 'detail' || (props.target === 'next' && nextKind.value === 'link')
);
function samplesFor(result?: LocatorCheck) {
	return showLinkSamples.value
		? result?.link_sample_values || result?.elements.map((element) => element.attributes?.href || '') || []
		: result?.sample_values || [];
}
const displayedSamples = computed(() => samplesFor(check.value));
const detailLinkAvailable = computed(
	() =>
		!!check.value &&
		(check.value.link_sample_values
			? check.value.link_sample_values.some((value) => !!value)
			: check.value.elements.some((element) => !!element.attributes?.href?.trim()))
);
const checkValid = computed(
	() =>
		!!check.value &&
		evidenceStateValid(check.value) &&
		checkStamp.value === contextStamp() &&
		check.value.locator.strategy === strategy.value &&
		check.value.locator.expression === expression.value.trim()
);
// Local, bounded drafts for four goals on the list/detail page. Never persisted as validated evidence.
type PendingSelection = {
	selection?: ElementInspection;
	check?: LocatorCheck;
	scope?: EditorLocator;
	scopeSource: string;
	strategy: 'css' | 'xpath';
	expression: string;
	fieldName: string;
	extractKind: FieldRule['extract'];
	nextKind: 'link' | 'click' | 'load_more';
	childIndex: number;
	attributeName: string;
};
const pendingSelections = new Map<string, PendingSelection>();
const scopeSource = () => JSON.stringify(props.recordScope) || '';
let appliedScopeSource = scopeSource();
const defaultScope = () =>
	props.detailPage || props.target === 'record' || props.target === 'next' ? undefined : props.recordScope ? { ...props.recordScope } : undefined;
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
	clearTimeout(recheckTimer);
	pendingComparison.value = undefined;
	rechecking.value = false;
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
	clearExamples();
	selectionStamp.value = checkStamp.value = '';
	dom.value = undefined;
	staleReason.value = reason;
	hideEvidence();
}
function recheckAvailable() {
	return !!props.recheckEnabled && !!props.session?.available && !props.recheckBlocked;
}
function queueRecheck() {
	clearTimeout(recheckTimer);
	if (!recheckAvailable() || !check.value?.state || !check.value.fingerprint || !expression.value.trim() || recheckRunning) return;
	rechecking.value = true;
	staleReason.value = '';
	recheckTimer = setTimeout(
		() => {
			void recheckRule();
		},
		Math.max(600, recheckAfter - Date.now())
	);
}
async function recheckRule() {
	const session = props.session,
		previous = check.value;
	if (!recheckAvailable() || !session?.state || !previous?.state || !previous.fingerprint || recheckRunning) {
		rechecking.value = false;
		return;
	}
	if (previous.state.document_id !== session.state.document_id) {
		rechecking.value = false;
		return;
	}
	const locator = { strategy: strategy.value, expression: expression.value.trim() };
	if (previous.locator.strategy !== locator.strategy || previous.locator.expression !== locator.expression) {
		rechecking.value = false;
		return;
	}
	const epoch = generation,
		context = contextStamp();
	recheckRunning = true;
	rechecking.value = true;
	recheckAfter = Date.now() + 3000;
	try {
		const result = await appApi.recheckLocator(session.session_id, {
			...extractionInput(),
			page_state_id: session.page_state_id,
			document_id: previous.state.document_id,
			locator,
			...(scope.value ? { scope: scope.value } : {}),
			previous_fingerprint: previous.fingerprint,
		});
		if (disposed || epoch !== generation || context !== contextStamp() || !recheckAvailable()) return;
		if (!result.state || result.state.document_id !== previous.state.document_id) throw new Error('document mismatch');
		adoptingRecheck = true;
		emit('verified-state', result.state);
		await nextTick();
		adoptingRecheck = false;
		if (
			disposed ||
			epoch !== generation ||
			!evidenceStateValid(result) ||
			locator.expression !== expression.value.trim() ||
			locator.strategy !== strategy.value
		)
			return;
		if (result.comparison === 'unchanged' && result.fingerprint === previous.fingerprint && !result.truncated && result.match_count > 0) {
			check.value = result;
			checkStamp.value = contextStamp();
			staleReason.value = '';
			message.value = '';
			// Element references stay invalid until explicitly inspected; only locator evidence is refreshed.
			const key = JSON.stringify([locator, scope.value]);
			evidence.set(key, result.fingerprint);
			highlight(result.elements, result.page_state_id, result.match_count === 1 ? 'selected' : 'similar');
		} else {
			pendingComparison.value = result;
			comparisonBefore.value = [...samplesFor(previous)];
			staleReason.value = '命中元素、记录范围或关键链接属性已变化，请核对新样例后确认。';
		}
	} catch {
		if (!disposed && epoch === generation) staleReason.value = '后台复核暂未完成；配置已保留，可手动重新验证。';
	} finally {
		adoptingRecheck = false;
		recheckRunning = false;
		if (epoch === generation) rechecking.value = false;
		else if (!disposed && !checkValid.value && check.value?.state?.document_id === props.session?.state?.document_id) queueRecheck();
	}
}
function acceptComparison() {
	const result = pendingComparison.value;
	if (
		!result ||
		!recheckAvailable() ||
		!evidenceStateValid(result) ||
		result.page_state_id !== props.session?.page_state_id ||
		!result.match_count ||
		result.truncated
	)
		return;
	check.value = result;
	checkStamp.value = contextStamp();
	pendingComparison.value = undefined;
	staleReason.value = '';
	const key = JSON.stringify([result.locator, scope.value]);
	evidence.set(key, result.fingerprint);
	highlight(result.elements, result.page_state_id, result.match_count === 1 ? 'selected' : 'similar');
}
watch(
	() => props.recheckBlocked,
	(blocked) => {
		if (!blocked && !checkValid.value && check.value?.state?.document_id === props.session?.state?.document_id) queueRecheck();
	}
);

function cancelSelection() {
	invalidateEvidence('');
	selection.value = check.value = undefined;
	expression.value = fieldName.value = message.value = '';
	attributeName.value = '';
	strategy.value = 'css';
	extractKind.value = 'text';
	nextKind.value = 'link';
	childIndex.value = 1;
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
function failure(cause: any, hover = false, action = '检查点选元素') {
	const code = cause?.error?.code;
	const hints: Record<string, string> = {
		INVALID_SAMPLES: '请选择不同记录中同一用途、同一标签的元素；最多 5 个样本。同一记录不要重复添加。',
		NO_ELEMENT: '此位置没有可选元素，请选择可见内容。',
		OUTSIDE_RECORD_SCOPE: '请选择记录范围内部的字段，或先调整记录范围。',
		RECORD_SCOPE_EMPTY: '当前记录范围未匹配元素，请重新选择记录范围。',
		ELEMENT_REFERENCE_STALE: '元素已失效，请重新点选或使用当前定位规则验证。',
		INVALID_LOCATOR: '定位规则语法无效，请检查 CSS / XPath；相对 XPath 需以点开头。',
		INSPECTION_LIMIT: '检查超过数量限制，请缩小规则范围或重新打开会话。',
		STALE_FRAME: '截图依据已更新，配置已保留；请更新画面后重新点选。',
		STALE_PAGE_STATE: '页面已变化，配置已保留；请更新画面后重新验证。',
		INSPECTION_BUSY: '页面暂时忙碌，请等待当前操作结束后重试。',
		COMMAND_PENDING: '上一动作结果尚未确认，请先查询回执。',
		SESSION_GONE: '会话已结束，配置已保留；恢复或重新打开后请重新验证。',
		FORBIDDEN: '当前账号没有此会话的访问权限，请检查登录账号。',
		INVALID_ARGUMENT: '服务拒绝了本次检查，尚未细分具体原因。',
	};
	if (code === 'STALE_FRAME' || code === 'STALE_PAGE_STATE' || code === 'SESSION_GONE' || code === 'ELEMENT_REFERENCE_STALE')
		invalidateEvidence(hints[code]);
	if (hover && ['NO_ELEMENT', 'OUTSIDE_RECORD_SCOPE', 'RECORD_SCOPE_EMPTY', 'ELEMENT_REFERENCE_STALE', 'INVALID_ARGUMENT'].includes(code)) return;
	const problem = editorProblem(cause, action);
	if (hints[code] && code !== 'INVALID_ARGUMENT') problem.reason = hints[code];
	message.value = problemMessage(problem);
	failureDetails.value = problem;
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
	let inspectedSelection: ElementInspection | undefined;
	try {
		const result = await appApi.inspectElement(session.session_id, {
			...input,
			...extractionInput(),
			...(!hover && addingExample.value && sampleElements.value.length
				? { sample_element_ids: sampleElements.value.map((el) => el.element_id) }
				: {}),
			...(input.position && props.frameState?.frame_sequence ? { frame_sequence: props.frameState.frame_sequence } : {}),
			page_state_id: pageState,
			mode: targetMode.value,
			...(targetMode.value === 'field' && scope.value ? { scope: scope.value } : {}),
		});
		if (!accept(session.session_id, pageState, epoch)) return;
		if (!hover && addingExample.value && !result.sample_elements) {
			message.value = '当前浏览器服务尚不支持多样本校准，请同步升级 scrapio-browser；原选择已保留。';
			return;
		}
		if (hover) {
			if (hoverEpoch === hoverGeneration) highlight([result.element], pageState, 'hover');
		} else {
			const firstSelection = !hasPending.value;
			if (!addingExample.value) sampleElements.value = [];
			selection.value = result;
			if (addingExample.value && result.sample_elements) {
				sampleElements.value = result.sample_elements;
				addingExample.value = false;
			}
			reviewedRisks.value = false;
			inspectedSelection = selection.value;
			selectionStamp.value = contextStamp();
			checkStamp.value = '';
			selectingAgain.value = false;
			check.value = undefined;
			message.value = '';
			if (!result.locators.length) message.value = '没有找到同时覆盖这些样本的规则；请核对它们是否属于同一字段，或清空样本后调整记录范围。';
			highlight([result.element], pageState, 'selected');
			highlight([], pageState, 'similar');
			highlight([], pageState, 'hover');
			recommended =
				(props.target === 'record' || input.expand_similar
					? result.locators.find((item) => item.match_count > 1 && !item.truncated && item.kind !== 'structural')
					: undefined) || result.locators.find((item) => item.match_count > 0 && !item.truncated);
			if (firstSelection && props.target === 'field' && !props.replacingField) {
				extractKind.value = result.element.tag.toLowerCase() === 'img' ? 'image' : 'text';
			}
			if (firstSelection && props.target === 'next') nextKind.value = result.element.attributes.href ? 'link' : 'click';
		}
	} catch (cause: any) {
		if (accept(session.session_id, pageState, epoch) && (!hover || hoverEpoch === hoverGeneration)) {
			if (hover) highlight([], pageState, 'hover');
			failure(cause, hover, input.element_id ? '检查元素或所属链接' : '检查网页点选位置');
		}
	} finally {
		busy.value = false;
		drainQueued();
	}
	if (recommended && !busy.value && accept(session.session_id, pageState, epoch)) await chooseCandidate(recommended);
	if (
		inspectedSelection &&
		selection.value === inspectedSelection &&
		(props.target === 'detail' || (props.target === 'field' && extractKind.value === 'link')) &&
		selectionValid.value &&
		selection.value?.enclosing_link &&
		!busy.value &&
		accept(session.session_id, pageState, generation)
	)
		await selectEnclosingLink();
	if (props.target === 'detail' && checkValid.value && !detailLinkAvailable.value)
		message.value = '详情入口须指向带 href 的链接。请选择 a 标签；旧浏览器服务可用“扩大到父元素”找到链接。';
}
async function selectEnclosingLink() {
	const result = selection.value;
	if (!result?.enclosing_link || !selectionValid.value || !available.value || busy.value || props.target === 'record') return;
	if (props.target === 'field' && !props.replacingField) extractKind.value = 'link';
	stopInspection();
	await inspect({ element_id: result.enclosing_link.element_id }, result.page_state_id, false, generation);
}
async function onExtractChanged() {
	clearExamples();
	if (extractKind.value !== 'link') {
		await validateLocator();
		return;
	}
	if (selectionValid.value && selection.value?.enclosing_link) await selectEnclosingLink();
	else if (selection.value && selection.value.element.tag.toLowerCase() !== 'a')
		message.value = '请选中带 href 的 a 标签；旧浏览器服务可用“扩大到父元素”找到链接。';
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
	const intent = JSON.stringify(extractionInput());
	busy.value = true;
	checkStamp.value = '';
	hideEvidence();
	message.value = '';
	try {
		const result = await appApi.checkLocator(session.session_id, {
			...extractionInput(),
			...(sampleElements.value.length ? { sample_element_ids: sampleElements.value.map((el) => el.element_id) } : {}),
			page_state_id: pageState,
			locator,
			...(activeScope ? { scope: activeScope } : {}),
			previous_fingerprint: evidence.get(key),
		});
		if (
			!accept(session.session_id, pageState, epoch) ||
			intent !== JSON.stringify(extractionInput()) ||
			expression.value.trim() !== locator.expression ||
			strategy.value !== locator.strategy
		)
			return;
		evidence.set(key, result.fingerprint);
		if (evidence.size > 64) evidence.delete(evidence.keys().next().value!);
		check.value = result;
		reviewedRisks.value = false;
		checkStamp.value = contextStamp();
		staleReason.value = '';
		selectingAgain.value = false;
		highlight([], pageState, result.match_count === 1 ? 'similar' : 'selected');
		highlight(result.elements, pageState, result.match_count === 1 ? 'selected' : 'similar');
	} catch (cause: any) {
		if (accept(session.session_id, pageState, epoch)) failure(cause, false, '验证当前定位规则');
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
	if (!canConfirm.value || !checkValid.value || !check.value || !check.value.match_count || check.value.truncated || busy.value) return;
	if (sampleMismatch.value || check.value.record_check?.truncated || (recordRisks.value && !reviewedRisks.value)) return;
	if (target === 'detail' && !detailLinkAvailable.value) {
		message.value = '当前规则没有可用的详情链接，请选中所在链接或扩大到 a 标签，再重新验证。';
		return;
	}
	if (target === 'next' && (scope.value || check.value.match_count !== 1)) return;
	if (props.detailPage && target !== 'field') return;
	emit('rule', {
		target,
		locator: { ...check.value.locator },
		...(targetMode.value === 'field' && scope.value ? { scope: { ...scope.value } } : {}),
		extract: extractKind.value,
		...(extractKind.value === 'attribute' ? { attribute: attributeName.value } : {}),
		...(target === 'field' ? { name: fieldName.value.trim() } : {}),
		...(target === 'next' ? { next_kind: nextKind.value } : {}),
	});
	message.value = '已提交到采集流程的记录配置，请核对后保存并预览。';
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
		if (accept(session.session_id, pageState, epoch)) failure(cause, false, '读取网页 DOM 层级');
	} finally {
		busy.value = false;
		drainQueued();
	}
}
function openDOM(event: Event) {
	if ((event.target as HTMLDetailsElement).open && !dom.value) void loadDOM();
}
watch(
	() => [props.session?.session_id, props.session?.page_state_id, props.session?.available, props.revision, props.session?.state] as const,
	(next, previous) => {
		const current = props.session?.state;
		const prior = previous?.[4];
		if (
			previous &&
			next.slice(0, 4).every((value, index) => value === previous[index]) &&
			current?.document_id === prior?.document_id &&
			current?.frame_state_id === prior?.frame_state_id &&
			current?.validation_state_id === prior?.validation_state_id
		)
			return;
		const change = stateChange(prior, current);
		const reason =
			current && prior
				? change === 'document'
					? '已切换网页文档，旧元素引用已停用；配置已保留，请重新点选或验证。'
					: change === 'validation'
						? '网页 DOM 已变化，旧验证证据已停用；配置已保留，请重新验证。'
						: '网页画面或滚动位置已变化，旧坐标已停用；规则保留，请重新验证。'
				: '页面、会话或草稿已变化；配置已保留，请重新验证。';
		// Same-document mutations request scoped read-only revalidation, not global user interruption.
		const canRecheck =
			props.recheckEnabled &&
			current &&
			prior &&
			current.document_id === prior.document_id &&
			[0, 2, 3].every((index) => next[index] === previous?.[index]) &&
			check.value?.state?.document_id === current.document_id;
		if (adoptingRecheck && canRecheck) return;
		if (canRecheck) {
			invalidateEvidence('');
			if (recheckRunning) rechecking.value = true;
			else queueRecheck();
		} else invalidateEvidence(reason);
	},
	{ flush: 'sync' }
);
watch(
	() => props.blocked,
	() => {
		if (props.recheckEnabled && !props.recheckBlocked) leaveFrame();
		else stopInspection();
		hideEvidence();
		if (available.value && checkValid.value && check.value)
			highlight(check.value.elements, props.session!.page_state_id, check.value.match_count === 1 ? 'selected' : 'similar');
	},
	{ flush: 'sync' }
);
watch(
	() => `${props.target}:${!!props.detailPage}`,
	(key, previous) => {
		if (previous)
			pendingSelections.set(previous, {
				selection: selection.value,
				check: check.value,
				scope: scope.value,
				scopeSource: appliedScopeSource,
				strategy: strategy.value,
				expression: expression.value,
				fieldName: fieldName.value,
				extractKind: extractKind.value,
				nextKind: nextKind.value,
				childIndex: childIndex.value,
				attributeName: attributeName.value,
			});
		const cached = pendingSelections.get(key);
		selection.value = cached?.selection;
		check.value = cached?.check;
		scope.value = cached && cached.scopeSource === scopeSource() ? cached.scope : defaultScope();
		appliedScopeSource = scopeSource();
		strategy.value = cached?.strategy || 'css';
		expression.value = cached?.expression || '';
		fieldName.value = cached?.fieldName || '';
		extractKind.value = cached?.extractKind || 'text';
		nextKind.value = cached?.nextKind || 'link';
		childIndex.value = cached?.childIndex || 1;
		attributeName.value = cached?.attributeName || '';
		message.value = '';
		selectingAgain.value = false;
		invalidateEvidence('已切换选择目标，请在当前页面重新验证保留的规则。');
	},
	{ immediate: true, flush: 'sync' }
);
watch(
	scopeSource,
	() => {
		scope.value = defaultScope();
		appliedScopeSource = scopeSource();
		invalidateEvidence('记录范围已更新，请重新验证。');
	},
	{ flush: 'sync' }
);
watch(
	[strategy, expression],
	() => {
		checkStamp.value = '';
		stopInspection();
		selectingAgain.value = false;
		hideEvidence();
	},
	{ flush: 'sync' }
);
watch(
	[fieldName, extractKind, nextKind],
	() => {
		selectingAgain.value = false;
		leaveFrame();
	},
	{ flush: 'sync' }
);
watch(
	[extractKind, attributeName, nextKind],
	() => {
		checkStamp.value = '';
		reviewedRisks.value = false;
		if (!busy.value) stopInspection();
		clearExamples();
	},
	{ flush: 'sync' }
);
onBeforeUnmount(() => {
	clearTimeout(recheckTimer);
	disposed = true;
	generation++;
	if (hoverTimer) clearTimeout(hoverTimer);
});
function prepareField(field: FieldRule) {
	cancelSelection();
	fieldName.value = field.name;
	extractKind.value = field.extract;
	attributeName.value = field.attribute || '';
	message.value = '请重新点选此字段；名称、提取方式和高级设置保持原配置。';
}
defineExpose({ framePick, frameHover, leaveFrame, hasPending, prepareField });
</script>
