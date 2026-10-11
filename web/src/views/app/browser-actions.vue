<template>
	<div
		ref="workbenchRoot"
		tabindex="-1"
		class="editor-action-workspace"
		:class="{ 'workbench-focused': focused, 'panel-is-collapsed': panelCollapsed }"
		:style="{ '--workbench-panel-height': panelHeight + 'px' }"
		@focusin="enterConfiguration"
		@focusout="leaveConfiguration"
		@keydown="handleWorkbenchKeydown"
	>
		<header class="workbench-toolbar">
			<div class="workbench-mode-group">
				<div class="editor-mode-tabs" role="tablist" aria-label="网页操作模式">
					<button role="tab" :aria-selected="mode === 'browse'" :disabled="busy || recordWorking || replaying" @click="mode = 'browse'; openPanel('selection')">
						浏览网页
					</button>
					<button role="tab" :aria-selected="selecting" :disabled="busy || recordWorking || replaying" @click="activateSelection(selectionTarget)">
						网页点选
					</button>
					<button role="tab" :aria-selected="mode === 'record'" :disabled="busy || recordWorking || replaying" @click="mode = 'record'; openPanel('selection')">
						录制操作
					</button>
				</div>
				<p class="mode-description">
					{{
						selecting
							? '选择模式：点选只检查元素，不操作网站。'
							: mode === 'record'
								? '录制中：成功动作加入草稿；失败动作只保留回执。点击画面准备动作，确认后执行。'
								: '浏览模式：执行真实操作，默认不加入草稿。点击画面准备动作，确认后执行。'
					}}
				</p>
			</div>
			<div class="workbench-tools">
				<div class="browser-viewport-toolbar" role="group" aria-label="远端网页浏览">
					<button class="secondary-action" :disabled="previewStreaming ? busy || previewScrollBusy || !supported.includes('scroll') : !canScroll" @click="previewStreaming ? emit('preview-scroll', -600) : scrollPage(-1)">向上滚动</button>
					<button class="secondary-action" :disabled="previewStreaming ? busy || previewScrollBusy || !supported.includes('scroll') : !canScroll" @click="previewStreaming ? emit('preview-scroll', 600) : scrollPage(1)">向下滚动</button>
					<button class="secondary-action" :disabled="!canRefreshFrame" @click="requestFrame">
						{{ updatingFrame ? '更新画面中…' : '更新画面' }}
					</button>
					<label v-if="mode === 'record'" class="record-scroll-toggle"
						><input type="checkbox" v-model="recordScroll" :disabled="busy || replaying" />记录滚动</label
					>
				</div>
				<details class="workbench-display-settings">
					<summary>显示设置</summary>
					<div class="workbench-settings-content">
						<slot name="display-settings" />
						<p class="muted">滚动按钮每次约 600 CSS 像素；更新画面不会重新加载网站。</p>
					</div>
				</details>
				<button :aria-pressed="focused" @click="toggleFocus">{{ focused ? '退出专注' : '专注网页' }}</button>
			</div>
		</header>
		<div v-show="selecting" class="selection-goals" aria-label="点选目标">
			<button
				v-for="goal in selectionGoals"
				:key="goal.target"
				:aria-pressed="selecting && selectionTarget === goal.target"
				:disabled="busy || recordWorking || replaying || (!!recordPanel?.detailPage && goal.target !== 'field')"
				@click="activateSelection(goal.target)"
			>
				<strong>{{ goal.label }}</strong><small>{{ recordPanel?.summary?.[goal.target] || goal.empty }}</small>
			</button>
		</div>
		<p class="muted">{{ selecting ? selectionHelp[selectionTarget] : '浏览或录制模式：点击网页后，在底部确认执行。' }}</p>
		<section class="editor-page-pane" aria-label="采集网页">
			<p v-if="session?.available && !supported.includes('scroll')" class="status-warn">当前服务未支持滚动；请检查浏览器服务版本。</p>
			<slot name="page" />
		</section>
		<div class="workbench-command-status" role="status" aria-live="polite">
			<span v-if="unknownRequest">上一请求结果未确认，确认前不能执行新动作。</span>
			<span v-else-if="pending">{{ labels[pending.type] }} · {{ statusLabel(pending.status) }}</span>
			<span v-else-if="submitting || replaying">{{ replaying ? '正在重放步骤…' : '正在执行动作…' }}</span>
			<span v-else-if="last"
				>{{ labels[last.type] }} · {{ statusLabel(last.status) }}{{ last.result.error ? ' · ' + last.result.error : ''
				}}{{ last.result.recording_error ? ' · ' + last.result.recording_error : '' }}</span
			>
			<span v-else>点选只检查元素；网页动作需确认后执行。</span>
			<button v-if="replaying" @click="stopReplay = true">停止后续步骤</button>
			<button v-if="pending" :disabled="querying" @click="queryPending">查询回执</button>
			<button v-if="unknownRequest" :disabled="querying" @click="resolveUnknown">确认上一请求（沿用请求键）</button>
			<button v-if="last || pending || unknownRequest" @click="openPanel('history')">查看操作记录</button>
			<span v-if="dirty || recordPanel?.localDirty" class="status-warn">规则有未保存修改</span>
			<EditorErrorNotice v-if="failureDetails" :problem="failureDetails" />
			<p v-else-if="notice" class="app-error">{{ notice }}</p>
		</div>
		<nav class="workbench-panel-tabs" aria-label="工作台面板">
			<button
				v-for="item in panelTabs"
				:key="item.key"
				:aria-pressed="!panelCollapsed && panelTab === item.key"
				aria-controls="workbench-bottom-panel"
				@click="openPanel(item.key)"
			>
				{{ item.label }}{{ item.key === 'flow' ? '（' + steps.length + '）' : '' }}
			</button>
			<label class="workbench-panel-size"
				>面板高度<input v-model.number="panelHeight" type="range" min="200" max="640" step="20" aria-label="底部面板高度"
			/></label>
			<button :aria-expanded="!panelCollapsed" aria-controls="workbench-bottom-panel" @click="panelCollapsed = !panelCollapsed">
				{{ panelCollapsed ? '展开' : '收起' }}
			</button>
		</nav>
		<div v-show="!panelCollapsed" id="workbench-bottom-panel" class="workbench-bottom-panel">
			<div v-show="panelTab === 'flow' && flowEditing" class="flow-edit-heading">
				<button @click="flowEditing = false">返回流程总览</button>
				<span>{{ selectedStep >= 0 ? stepLabel(steps[selectedStep]) : '当前采集规则' }} · {{ recordPanel?.localDirty ? '有未保存修改' : '草稿配置' }}</span>
			</div>
			<section v-show="panelTab === 'flow' && !flowEditing" class="editor-step-pane">
				<div class="pane-heading">
					<h3>采集步骤</h3>
					<span>{{ steps.length }} 步</span>
				</div>
				<p class="muted">查看执行顺序，选中一项再配置；字段、详情与翻页归属于对应采集步骤。</p>
				<button :disabled="busy || recordWorking || replaying" @click="flowEditing = true; selectedStep = -1">{{ recordPanel?.localDirty ? '继续编辑未保存规则' : '配置采集规则' }}</button>
				<ol class="step-list">
					<li v-for="(step, index) in steps" :key="step.step_id || index" :class="{ selected: selectedStep === index }">
						<button class="step-title" :disabled="busy || recordWorking || replaying || !!recordPanel?.localDirty || !!recordPanel?.detailPage" @click="selectStep(index)">
							{{ index + 1 }}. {{ stepLabel(step) }}
						</button>
						<small>{{ stepSummary(step) }}</small>
						<ul v-if="step.type === 'record_set'" class="record-step-outline">
							<li>{{ step.config?.fields?.length || 0 }} 个列表字段{{ step.config?.fields?.length ? '：' + step.config.fields.slice(0, 3).map((field: FieldRule) => field.name).join('、') + (step.config.fields.length > 3 ? '…' : '') : '' }}</li>
							<li v-if="step.config?.detail">
								发现详情 → 打开 → 提取 {{ step.config?.detail_fields?.length || 0 }} 个字段 →
								{{ step.config.detail.return_strategy === 'back' ? '后退列表' : '重开列表' }}
							</li>
							<li v-if="step.config?.next_page">{{ nextLabels[step.config.next_page.kind] || '翻页' }} · 最多 {{ step.config.next_page.max_pages }} 轮</li>
						</ul>
						<details class="step-controls"><summary>更多操作</summary>
							<button :disabled="!canEdit || !!recordPanel?.detailPage || index === 0" @click="moveStep(index, -1)" aria-label="上移步骤">↑</button>
							<button :disabled="!canEdit || !!recordPanel?.detailPage || index === steps.length - 1" @click="moveStep(index, 1)" aria-label="下移步骤">↓</button>
							<button class="danger-action" :disabled="!canEdit || !!recordPanel?.detailPage" @click="removeStep(index)">删除</button>
							<button :disabled="!canExecute || !executable(step)" @click="runStep(step)">单步执行</button>
						</details>
					</li>
				</ol>
				<p v-if="!steps.length" class="record-empty-state">尚未配置流程。从网页选记录或字段开始；单页可以直接选字段。需要点击或输入准备网页时，再使用录制操作。</p>
				<details class="editor-advanced">
					<summary>调试工具 · 检查点与重放</summary>
					<button class="secondary-action" :disabled="!canEdit || !steps.length" @click="saveCheckpoint">保存检查点</button>
					<p class="muted">保存至选中步骤，重放使用这份快照。</p>
					<div v-for="checkpoint in checkpoints" :key="checkpoint.checkpoint_id" class="checkpoint-row">
						<strong>{{ checkpoint.name }}</strong
						><small>revision {{ checkpoint.draft_revision }} · {{ checkpoint.snapshot.steps.length }} 步</small>
						<button :disabled="!canEdit || replaying || !supported.length" @click="replay(checkpoint)">从入口重放</button>
						<button :disabled="busy || replaying" @click="removeCheckpoint(checkpoint)">删除</button>
					</div>
					<button v-if="replaying" class="secondary-action" @click="stopReplay = true">停止后续步骤</button>
				</details>
			</section>
			<section
				v-show="panelTab === 'selection' || (panelTab === 'flow' && flowEditing && !selecting && selectedStep >= 0 && executable(steps[selectedStep]))"
				class="editor-command-pane"
			>
				<BrowserSelection
					v-show="selecting && panelTab === 'selection'"
					ref="selectionPanel"
					:session="session"
					:frame-state="frameState"
					:mode="mode === 'field-select' ? 'field' : 'record'"
					:enabled="inspectionSupported"
					:recheck-enabled="recheckSupported"
					:recheck-blocked="busy || recordWorking || replaying || !selecting || dirty || saving || !!recordPanel?.selectionBlocked"
					@verified-state="emit('verified-state', $event)"
					:target="selectionTarget"
					:replacing-field="replacingField"
					:revision="draft.revision"
					:blocked="busy || recordWorking || replaying || !selecting || dirty || saving || !frameReady || !!recordPanel?.selectionBlocked"
					:record-scope="recordPanel?.recordScope"
					:detail-page="recordPanel?.detailPage"
					@highlight="emit('highlight', $event)"
					@working="inspectionWorking = $event"
					@action-locator="applyLocator"
					@rule="applySelection"
				/>
				<form
					v-show="!selecting && (panelTab === 'selection' || (panelTab === 'flow' && selectedStep >= 0 && executable(steps[selectedStep])))"
					@submit.prevent="executeForm"
				>
					<div class="command-form-grid">
						<label
							>动作<select v-model="actionType" :disabled="busy || replaying" @change="onActionTypeChanged">
								<option v-for="action in actions" :key="action" :value="action">{{ labels[action] }}</option>
							</select></label
						>
						<label>最长等待（毫秒）<input v-model.number="timeout" type="number" min="100" max="30000" step="100" /></label>
					</div>
					<label v-if="['navigate', 'input', 'scroll', 'wait'].includes(actionType)"
						>{{
							actionType === 'navigate'
								? '目标地址'
								: actionType === 'input'
									? '普通文本（不支持密码/验证码）'
									: actionType === 'scroll'
										? '滚动像素（负数向上）'
										: '等待毫秒；填写定位器时改为等待元素'
						}}<input
							v-model="value"
							:type="actionType === 'navigate' ? 'url' : 'text'"
							:placeholder="actionType === 'navigate' ? draft.entry_url : ''"
							maxlength="4096"
					/></label>
					<div v-if="['click', 'input', 'wait'].includes(actionType) && !position" class="command-form-grid">
						<label
							>定位方式<select v-model="strategy">
								<option value="css">CSS</option>
								<option value="xpath">XPath</option>
							</select></label
						>
						<label>定位器<input v-model="expression" placeholder="例如 #load-more 或 //button[@id='more']" maxlength="2048" /></label>
					</div>
					<p v-if="position" class="position-notice">
						画面点击坐标 {{ Math.round(position.x) }}, {{ Math.round(position.y) }}。<button type="button" @click="position = undefined">
							改用定位器
						</button>
					</p>
					<div class="editor-actions workbench-confirm-actions">
						<button class="primary-action" type="submit" :disabled="!canExecute || !supported.includes(actionType)">
							{{ busy ? '等待动作回执…' : mode === 'record' ? '执行并录制' : '执行动作' }}
						</button>
						<button
							type="button"
							class="secondary-action"
							:disabled="!canEdit || !formValid || !!position || actionType === 'input'"
							@click="addConfiguredStep"
						>
							只加入步骤
						</button>
						<button
							v-if="selectedStep >= 0"
							type="button"
							class="secondary-action"
							:disabled="!canEdit || !formValid || !!position || actionType === 'input' || !executable(steps[selectedStep])"
							@click="updateConfiguredStep"
						>
							更新选中步骤
						</button>
						<button
							v-if="last?.status === 'succeeded' && last.result.recording_status !== 'recorded'"
							type="button"
							class="secondary-action"
							:disabled="!canEdit || last.type === 'input'"
							@click="addLastStep"
						>
							将成功动作加入步骤
						</button>
					</div>
				</form>
				<p v-if="actionType === 'input'" class="muted">
					输入步骤须在录制模式执行成功后保存；密码与验证码输入框会被拒绝。请勿在普通文本或 JSON 草稿中填写秘密。
				</p>
				<p v-if="!supported.length" class="muted">动作服务尚未就绪或版本不支持；草稿编辑仍可使用。</p>
				<p v-if="dirty" class="status-warn">有未保存修改，请先保存再执行。</p>
			</section>
			<section v-show="panelTab === 'history'" class="workbench-history-pane">
				<section v-if="pending || last" class="command-receipt" aria-live="polite">
					<h3>动作回执 · {{ statusLabel((pending || last)!.status) }}</h3>
					<p>{{ labels[(pending || last)!.type] }} · {{ (pending || last)!.result.duration_ms || 0 }} ms</p>
					<p v-if="(pending || last)!.result.final_url" class="muted">{{ (pending || last)!.result.final_url }}</p>
					<p v-if="(pending || last)!.result.error">{{ (pending || last)!.result.error }}</p>
					<p v-if="(pending || last)!.result.recording_error">{{ (pending || last)!.result.recording_error }}</p>
					<small>前：{{ (pending || last)!.result.before_page_state_id }}<br />后：{{ (pending || last)!.result.after_page_state_id }}</small>
					<button v-if="pending" :disabled="querying" @click="queryPending">查询回执</button>
				</section>
				<button v-if="unknownRequest" :disabled="querying" @click="resolveUnknown">确认上一请求（沿用请求键）</button>
				<details class="command-history" open>
					<summary>最近动作（最多 50 条）</summary>
					<div v-for="command in history" :key="command.command_id">
						<span>{{ labels[command.type] }} · {{ statusLabel(command.status) }}</span
						><button @click="last = command">查看回执</button>
					</div>
				</details>
			</section>
			<RecordDesigner
				ref="recordPanel"
				v-show="(panelTab === 'flow' && flowEditing && (selectedStep < 0 || steps[selectedStep]?.type === 'record_set')) || panelTab === 'preview'"
				:view="panelTab === 'preview' ? 'preview' : 'rules'"
				@preview-requested="openPanel('preview')"
				class="editor-record-pane"
				@select="activateSelection"
				:draft="draft"
				:session="session"
				:selected-step="steps[selectedStep]"
				:enabled="recordsSupported"
				:blocked="busy || replaying || dirty || saving"
				:execute="executePathAction"
				@working="recordWorking = $event"
				@draft-saved="emit('draft-saved', $event)"
				@highlight="emit('highlight', $event)"
			/>
			<p v-if="panelTab === 'flow' && flowEditing && selectedStep >= 0 && steps[selectedStep]?.type !== 'record_set' && !executable(steps[selectedStep])" class="status-warn">当前步骤使用高级配置，请在方案的高级定义中编辑；当前表单不会覆盖它。</p>
		</div>
	</div>
</template>

<script setup lang="ts">
import EditorErrorNotice from './editor-error-notice.vue';
import { editorProblem, problemMessage, type EditorProblem } from './editor-feedback';
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import {
	appApi,
	type BrowserSession,
	type Collector,
	type EditorAction,
	type EditorCheckpoint,
	type EditorCommand,
	type EditorLocator,
	type PageHighlight,
	type FieldRule,
	type SelectionRule,
} from './api';
import BrowserSelection from './browser-selection.vue';
import { createWheelQueue, wheelPixels } from './browser-workbench';
import RecordDesigner from './record-designer.vue';

const props = withDefaults(defineProps<{ draft: Collector; session?: BrowserSession; dirty: boolean; saving: boolean; frameReady: boolean; frameRefreshing: boolean; frameFailed?: boolean; previewStreaming?: boolean; previewScrollBusy?: boolean; frameState?: BrowserSession['state']; active?: boolean; wheelEnabled?: boolean; refreshFrame: (state?: BrowserSession) => Promise<void> }>(), { active: true, wheelEnabled: true });
const emit = defineEmits<{
	(event: 'draft-saved', draft: Collector): void;
	(event: 'session-state', session: BrowserSession): void;
	(event: 'highlight', value: PageHighlight): void;
	(event: 'frame-invalidated'): void;
	(event: 'workbench-busy', busy: boolean): void;
	(event: 'preview-scroll', distance: number): void;
	(event: 'view-mode', mode: 'browse' | 'configure'): void;
	(event: 'verified-state', state: NonNullable<BrowserSession['state']>): void;
}>();
const labels: Record<string, string> = {
	navigate: '打开网址',
	back: '后退',
	forward: '前进',
	refresh: '刷新页面',
	click: '点击元素',
	input: '输入文本',
	wait: '等待',
	scroll: '滚动',
};
const actions = Object.keys(labels);
type PanelTab = 'selection' | 'flow' | 'preview' | 'history';
const panelTabs: { key: PanelTab; label: string }[] = [
	{ key: 'selection', label: '当前选择 / 动作' },
	{ key: 'flow', label: '采集流程' },
	{ key: 'preview', label: '数据预览' },
	{ key: 'history', label: '操作记录' },
];
const panelTab = ref<PanelTab>('selection'),
	panelCollapsed = ref(false),
	panelHeight = ref(360),
	focused = ref(false);
const flowEditing = ref(false), replacingField = ref(false);
function openPanel(tab: PanelTab) {
	panelTab.value = tab;
	panelCollapsed.value = false;
}
const workbenchRoot = ref<HTMLElement>();
let focusOrigin: HTMLElement | undefined;
let previouslyCollapsed = false;
function exitFocus() {
	if (!focused.value) return;
	focused.value = false;
	panelCollapsed.value = previouslyCollapsed;
	void nextTick(() => focusOrigin?.focus());
}
function handleWorkbenchKeydown(event: KeyboardEvent) {
 if (!focused.value) return;
 if (event.key === 'Escape') { event.preventDefault(); exitFocus(); return; }
 if (event.key !== 'Tab') return;
 const controls = [...(workbenchRoot.value?.querySelectorAll<HTMLElement>('button:not(:disabled), input:not(:disabled), select:not(:disabled), textarea:not(:disabled), summary, a[href], [tabindex="0"]') || [])].filter(el => el.offsetParent !== null);
 const first = controls[0], last = controls[controls.length - 1];
 if (!first) { event.preventDefault(); return; }
 if (event.shiftKey && (document.activeElement === first || document.activeElement === workbenchRoot.value)) { event.preventDefault(); last.focus(); }
 else if (!event.shiftKey && (document.activeElement === last || document.activeElement === workbenchRoot.value)) { event.preventDefault(); first.focus(); }
}
function toggleFocus() {
	if (focused.value) {
		exitFocus();
		return;
	}
	focusOrigin = document.activeElement instanceof HTMLElement ? document.activeElement : undefined;
	previouslyCollapsed = panelCollapsed.value;
	focused.value = true;
	panelCollapsed.value = true;
	void nextTick(() => workbenchRoot.value?.focus());
}
watch(
	() => props.active,
	(active) => {
		if (active === false) focused.value = false;
	}
);
const mode = ref<'browse' | 'record' | 'record-select' | 'field-select'>('browse');
watch(mode, value => emit('view-mode', value === 'browse' ? 'browse' : 'configure'), { immediate: true, flush: 'sync' });
const selectionTarget = ref<SelectionRule['target']>('record');
const selectionGoals: { target: SelectionRule['target']; label: string; empty: string }[] = [
	{ target: 'record', label: '记录范围', empty: '整页一条 / 重复容器' },
	{ target: 'field', label: '提取字段', empty: '从网页添加字段' },
	{ target: 'detail', label: '详情入口', empty: '未启用 · 可选' },
	{ target: 'next', label: '翻页方式', empty: '未启用 · 可选' },
];
const selectionHelp: Record<SelectionRule['target'], string> = {
	record: '点一条记录，扩大到记录容器并选择同类；核对高亮范围后使用。',
	field: '在记录内点选标题、图片等字段；底部可命名并确认提取内容。',
	detail: '在一条记录内点选详情标题，自动定位所在链接；核对地址后使用。旧服务可扩大到带链接的父元素。',
	next: '点选列表页的下一页或加载更多；确认采用链接导航还是按钮点击。',
};
async function activateSelection(target: SelectionRule['target'], field?: FieldRule) {
	if (busy.value || recordWorking.value || replaying.value) return;
	if (recordPanel.value?.selectionBlocked) {
		notice.value = '详情页尚未确认或已离开示例详情，请先检查页面位置并恢复路径。';
		return;
	}
	if (recordPanel.value?.detailPage && target !== 'field') return;
	recordPanel.value?.prepareSelection(target, field);
	replacingField.value = !!field;
	selectionTarget.value = target;
	openPanel('selection');
	mode.value = target === 'field' || target === 'detail' ? 'field-select' : 'record-select';
	await nextTick();
	if (field) selectionPanel.value?.prepareField(field);
	workbenchRoot.value?.querySelector('.editor-page-pane')?.scrollIntoView({ block: 'nearest' });
}
function applySelection(rule: SelectionRule) {
	if (!recordPanel.value?.applySelection(rule)) {
		notice.value = recordPanel.value?.selectionError || '当前规则无法采用，请检查页面、记录范围和字段名称。';
		return;
	}
	notice.value = '';
	replacingField.value = false;
	selectedStep.value = -1;
	flowEditing.value = true;
	openPanel('flow');
}
watch([panelTab, panelCollapsed], () => { configurationFocused.value = false; });
const selecting = computed(() => mode.value === 'record-select' || mode.value === 'field-select');
const selectionPanel = ref<InstanceType<typeof BrowserSelection>>();
const inspectionSupported = ref(false), recheckSupported = ref(false);
const recordsSupported = ref(false),
	recordWorking = ref(false);
const recordPanel = ref<InstanceType<typeof RecordDesigner>>();
const supported = ref<string[]>([]);
const actionType = ref('navigate');
const value = ref('');
const strategy = ref<'css' | 'xpath'>('css');
const expression = ref('');
const timeout = ref(10000);
const position = ref<{ x: number; y: number }>();
const positionPageStateID = ref('');
const positionFrameSequence = ref<number>();
const selectedStep = ref(-1);
const checkpoints = ref<EditorCheckpoint[]>([]);
const history = ref<EditorCommand[]>([]);
const pending = ref<EditorCommand>();
const last = ref<EditorCommand>();
const savingSteps = ref(false);
const submitting = ref(false);
const querying = ref(false);
const replaying = ref(false);
const stopReplay = ref(false);
const notice = ref('');
const failureDetails = ref<EditorProblem>();
watch(notice, () => { failureDetails.value = undefined; }, { flush: 'sync' });
function reportFailure(cause: any, action: string) {
	const problem = editorProblem(cause, action);
	notice.value = problemMessage(problem);
	failureDetails.value = problem;
}
const unknownRequest = ref<{ sessionId: string; key: string; input: EditorAction }>();
const recordScroll = ref(false), updatingFrame = ref(false);
let disposed = false;
let generation = 0;
const steps = computed<Record<string, any>[]>(() => (Array.isArray(props.draft.definition.steps) ? props.draft.definition.steps : []));
const busy = computed(() => submitting.value || savingSteps.value || !!pending.value || !!unknownRequest.value);
const canEdit = computed(
	() =>
		Array.isArray(props.draft.definition.steps) &&
		!props.dirty &&
		!props.saving &&
		!busy.value &&
		!recordWorking.value &&
		!replaying.value &&
		!recordPanel.value?.localDirty
);
const canExecute = computed(() => props.active !== false && canEdit.value && !!props.session?.available && props.frameReady && !props.frameRefreshing && !updatingFrame.value);
const inspectionWorking = ref(false), configurationFocused = ref(false);
const reviewing = computed(() => !panelCollapsed.value && ((selecting.value && panelTab.value === 'selection' && !!selectionPanel.value?.hasPending) || panelTab.value === 'preview'));
const workbenchBusy = computed(() => busy.value || reviewing.value || inspectionWorking.value || configurationFocused.value || recordWorking.value || replaying.value || updatingFrame.value || !!recordPanel.value?.localDirty || !!position.value);
watch(workbenchBusy, value => emit('workbench-busy', value), { flush: 'sync', immediate: true });
function enterConfiguration(event: FocusEvent) {
	configurationFocused.value = !!(event.target as HTMLElement).closest('.editor-command-pane, .editor-record-pane');
	if (configurationFocused.value) leaveFrame();
}
function leaveConfiguration(event: FocusEvent) {
	configurationFocused.value = event.relatedTarget instanceof Element && !!event.relatedTarget.closest('.editor-command-pane, .editor-record-pane');
}
const canScroll = computed(() => canExecute.value && !inspectionWorking.value && supported.value.includes('scroll'));
const wheelQueue = createWheelQueue({ ready: () => canScroll.value, dispatch: scrollPixels });
watch(() => props.frameFailed, failed => { if (failed) wheelQueue.clear(); });
watch(canScroll, ready => { if (ready) void wheelQueue.pump(); });
watch(() => [props.session?.session_id, mode.value, recordScroll.value, props.active, props.wheelEnabled, props.dirty, props.saving, props.session?.available], () => wheelQueue.clear());
function frameWheel(event: WheelEvent, height: number) {
	const delta = wheelPixels(event, height);
	if (props.wheelEnabled === false || props.active === false || props.frameFailed || !delta || (!canScroll.value && !wheelQueue.running) || unknownRequest.value || pending.value?.status === 'uncertain') return false;
	return wheelQueue.add(delta);
}
const canRefreshFrame = computed(() => !!props.session?.available && !busy.value && !recordWorking.value && !replaying.value && !props.frameRefreshing && !updatingFrame.value);
async function requestFrame() {
	wheelQueue.clear();
	if (!canRefreshFrame.value) return;
	updatingFrame.value = true;
	selectionPanel.value?.leaveFrame();
	try { await props.refreshFrame(); }
	catch { notice.value = '画面更新失败，请确认服务连接后重试。'; }
	finally { updatingFrame.value = false; }
}
async function scrollPage(direction: -1 | 1) {
	wheelQueue.clear();
	if (direction === -1 || direction === 1) await scrollPixels(direction * 600);
}
async function scrollPixels(distance: number): Promise<boolean> {
	if (!canScroll.value || !Number.isFinite(distance)) return false;
	const record = mode.value === 'record' && recordScroll.value;
	submitting.value = true;
	notice.value = '';
	selectionPanel.value?.leaveFrame();
	try {
		const result = await send({ type: 'scroll', value: String(Math.round(Math.max(-1200, Math.min(1200, distance)))), timeout_ms: 10000, page_state_id: props.session!.page_state_id }, record);
		return result.status === 'succeeded' && !props.frameFailed && !unknownRequest.value;
	} catch (cause: any) {
		reportFailure(cause, '滚动远端网页');
		return false;
	} finally { submitting.value = false; }
}
const formValid = computed(
	() =>
		Number.isInteger(timeout.value) &&
		timeout.value >= 100 &&
		timeout.value <= 30000 &&
		(!(actionType.value === 'input' || actionType.value === 'click') || !!position.value || !!expression.value.trim())
);

function statusLabel(status: string) {
	return (
		({ queued: '排队中', running: '执行中', succeeded: '成功', failed: '失败', uncertain: '结果未确认' } as Record<string, string>)[status] || status
	);
}
function executable(step?: Record<string, any>) {
	return !!step && (step.type === 'navigate' || (step.type === 'action' && actions.includes(step.config?.action)));
}
function stepLabel(step?: Record<string, any>) {
	if (!step) return '当前采集规则';
	return step.type === 'record_set'
		? '采集记录'
		: step.type === 'navigate'
			? `打开 ${step.config?.url || '网址'}`
			: labels[step.config?.action] || step.type;
}
const nextLabels: Record<string, string> = { link: '下一页链接', click: '下一页按钮', load_more: '加载更多' };
function stepSummary(step: Record<string, any>) {
	return step.type === 'record_set' ? `${step.config?.mode === 'repeated' ? '重复记录' : '整页一条'} · 规则已配置，页面检查另行核对`
		: step.type === 'navigate' ? '打开网址' : '网页动作 · 按流程顺序执行';
}
function selectStep(index: number) {
	if (busy.value || recordWorking.value || replaying.value || recordPanel.value?.localDirty || recordPanel.value?.detailPage) return;
	openPanel('flow');
	selectedStep.value = index;
	flowEditing.value = true;
	const step = steps.value[index];
	if (!executable(step)) return;
	mode.value = 'browse';
	actionType.value = step.type === 'navigate' ? 'navigate' : step.config.action;
	value.value = step.type === 'navigate' ? step.config.url : step.config.value || '';
	expression.value = step.config.locator?.expression || '';
	strategy.value = step.config.locator?.strategy || 'css';
	timeout.value = step.config.timeout_ms || 10000;
	position.value = undefined;
}
function onActionTypeChanged() {
	position.value = undefined;
	value.value =
		actionType.value === 'navigate' ? props.draft.entry_url : actionType.value === 'scroll' ? '600' : actionType.value === 'wait' ? '1000' : '';
}
function configuredAction(): Partial<EditorAction> {
	const action: Partial<EditorAction> = { type: actionType.value, value: value.value, timeout_ms: timeout.value };
	if (position.value) {
		action.position = { ...position.value };
		action.page_state_id = positionPageStateID.value;
		action.frame_sequence = positionFrameSequence.value;
	} else if (['click', 'input', 'wait'].includes(action.type!) && expression.value.trim())
		action.locator = { strategy: strategy.value, expression: expression.value.trim() };
	return action;
}
function stepAction(step: Record<string, any>): Partial<EditorAction> {
	if (!executable(step)) throw new Error('这一步不是可执行的网页动作');
	return {
		type: step.type === 'navigate' ? 'navigate' : step.config.action,
		value: step.type === 'navigate' ? step.config.url : step.config.value || '',
		locator: step.config.locator,
		timeout_ms: step.config.timeout_ms || 10000,
	};
}
function actionStep(action: Partial<EditorAction>) {
	return action.type === 'navigate'
		? { step_id: `step-${crypto.randomUUID()}`, type: 'navigate', config: { url: action.value, timeout_ms: action.timeout_ms } }
		: {
				step_id: `step-${crypto.randomUUID()}`,
				type: 'action',
				config: {
					action: action.type,
					value: action.value || '',
					timeout_ms: action.timeout_ms,
					...(action.locator ? { locator: action.locator } : {}),
				},
			};
}
async function saveSteps(next: Record<string, any>[]) {
	if (!canEdit.value) return;
	savingSteps.value = true;
	notice.value = '';
	try {
		const draft = await appApi.updateCollector(props.draft.id, {
			name: props.draft.name,
			expected_revision: props.draft.revision,
			definition: { ...props.draft.definition, steps: next },
		}, true);
		if (!disposed) emit('draft-saved', draft);
	} catch (cause: any) {
		reportFailure(cause, '保存流程步骤');
	} finally {
		savingSteps.value = false;
	}
}
async function moveStep(index: number, offset: number) {
	if (!canEdit.value || recordPanel.value?.detailPage || index + offset < 0 || index + offset >= steps.value.length) return;
	const next = [...steps.value];
	[next[index], next[index + offset]] = [next[index + offset], next[index]];
	await saveSteps(next);
	selectedStep.value = index + offset;
}
async function removeStep(index: number) {
	if (!canEdit.value || recordPanel.value?.detailPage || index < 0 || index >= steps.value.length) return;
	if (!window.confirm(`删除“${stepLabel(steps.value[index])}”及其关联配置？保存到草稿后生效。`)) return;
	await saveSteps(steps.value.filter((_, i) => i !== index));
	selectedStep.value = -1;
}
async function addConfiguredStep() {
	if (formValid.value && !position.value && actionType.value !== 'input') await saveSteps([...steps.value, actionStep(configuredAction())]);
}
async function updateConfiguredStep() {
	if (!canEdit.value || !formValid.value || position.value || actionType.value === 'input' || selectedStep.value < 0) return;
	const next = [...steps.value];
	next[selectedStep.value] = { ...next[selectedStep.value], ...actionStep(configuredAction()), step_id: next[selectedStep.value].step_id };
	await saveSteps(next);
}
async function addLastStep() {
	if (!last.value || last.value.status !== 'succeeded' || last.value.type === 'input') return;
	const action = { ...last.value.request, locator: last.value.result.locator || last.value.request.locator };
	await saveSteps([...steps.value, actionStep(action)]);
}
function frameClick(x: number, y: number, pageStateID: string) {
	wheelQueue.clear();
	if (!props.frameReady || props.frameRefreshing) return;
	if (selecting.value) {
		openPanel('selection');
		selectionPanel.value?.framePick(x, y, pageStateID);
		return;
	}
	if (!canExecute.value || !pageStateID) return;
	openPanel('selection');
	positionPageStateID.value = pageStateID;
	positionFrameSequence.value = props.frameState?.frame_sequence;
	position.value = { x, y };
	actionType.value = 'click';
	notice.value = '已准备点击动作，请在底部确认执行；网页还未被点击。';
}
function frameHover(x: number, y: number, pageStateID: string) {
	if (!configurationFocused.value && props.frameReady && !props.frameRefreshing && selecting.value) selectionPanel.value?.frameHover(x, y, pageStateID);
}
function leaveFrame() {
	selectionPanel.value?.leaveFrame();
}
function applyLocator(locator: EditorLocator) {
	openPanel('selection');
	mode.value = 'browse';
	actionType.value = 'click';
	position.value = undefined;
	strategy.value = locator.strategy;
	expression.value = locator.expression;
	notice.value = '已填入验证过的定位器；执行前仍需确认。';
}
watch(mode, () => {
	recordScroll.value = false;
	selectionPanel.value?.leaveFrame();
	emit('highlight', { elements: [], page_state_id: props.session?.page_state_id || '', kind: 'selected' });
});
watch(() => [props.session?.page_state_id, props.frameReady], () => {
	if (!props.frameReady || positionPageStateID.value !== props.session?.page_state_id) {
		position.value = undefined;
		positionPageStateID.value = '';
	}
	selectionPanel.value?.leaveFrame();
});
function hasUnsavedChanges() {
	return !!recordPanel.value?.localDirty || busy.value || recordWorking.value || replaying.value;
}
async function focusField(stepID: string, key: string, role: string) {
	selectedStep.value = -1;
	openPanel('flow');
	flowEditing.value = true;
	await nextTick();
	recordPanel.value?.focusField(stepID, key, role);
}
defineExpose({ activateSelection, scrollPixels, frameWheel, frameClick, frameHover, leaveFrame, hasUnsavedChanges, focusField });

async function refreshHistory(id = props.session?.session_id) {
	if (!id) {
		history.value = [];
		return;
	}
	try {
		const result = await appApi.browserCommands(id);
		if (disposed || id !== props.session?.session_id) return;
		history.value = result.items;
		const active = result.items.find((item) => ['queued', 'running', 'uncertain'].includes(item.status));
		if (active && !pending.value) pending.value = active;
	} catch {
		notice.value = '无法读取动作历史，继续操作前请检查服务连接。';
	}
}
async function refreshCheckpoints() {
	try {
		checkpoints.value = (await appApi.checkpoints(props.draft.id)).items;
	} catch {
		notice.value = '检查点暂时无法读取。';
	}
}
async function refreshState(id: string) {
	const state = await appApi.browserSession(id, true);
	if (!disposed && id === props.session?.session_id) emit('session-state', state);
	return state;
}
async function settle(command: EditorCommand) {
	if (disposed || command.session_id !== props.session?.session_id) return;
	if (command.status === 'queued' || command.status === 'running' || command.status === 'uncertain') {
		pending.value = command;
		return;
	}
	pending.value = undefined;
	last.value = command;
	if (command.result.recording_status === 'recorded') {
		const latest = await appApi.collector(props.draft.id);
		if (!disposed) emit('draft-saved', latest);
	}
	await refreshState(command.session_id).then(state => props.refreshFrame(state)).catch(() => {
		notice.value = '动作回执已保存；页面状态暂时无法更新。';
	});
	await refreshHistory(command.session_id);
	if (command.result.recording_error) notice.value = command.result.recording_error;
}
async function poll(command: EditorCommand, epoch: number): Promise<EditorCommand> {
	let current = command;
	// Bounded UI polling; receipt can always be queried manually afterwards.
	for (let i = 0; i < 45 && ['queued', 'running'].includes(current.status); i++) {
		await new Promise((resolve) => setTimeout(resolve, 1000));
		if (disposed || epoch !== generation || command.session_id !== props.session?.session_id) throw new Error('操作面板已离开；回执仍保留在会话历史');
		current = await appApi.browserCommand(command.session_id, command.command_id);
		pending.value = current;
	}
	await settle(current);
	return current;
}
async function send(action: Partial<EditorAction>, record: boolean, confirmed = false): Promise<EditorCommand> {
	if (action.type !== 'scroll') wheelQueue.clear();
	const session = props.session;
	if (!session?.available || props.dirty || props.saving || pending.value || unknownRequest.value)
		throw new Error('会话不可用、草稿未同步或上一动作尚未确认');
	if (!supported.value.includes(action.type!)) throw new Error('当前浏览器服务不支持这个动作');
	if (
		!confirmed &&
		['click', 'input'].includes(action.type!) &&
		!window.confirm(`即将在目标网站执行“${labels[action.type!]}”。可能触发表单提交或外部操作，确认执行？`)
	)
		throw new Error('已取消，未执行网页动作');
	const epoch = generation;
	const current = await refreshState(session.session_id);
	if (disposed || epoch !== generation || !current.available) throw new Error('会话已变化，未派发动作');
	if (action.page_state_id && action.page_state_id !== current.page_state_id) throw new Error('动作依据的页面已过期，请重新检查后操作');
	const input: EditorAction = {
		...action,
		type: action.type!,
		timeout_ms: action.timeout_ms || 10000,
		page_state_id: current.page_state_id,
		expected_revision: props.draft.revision,
		confirmed: true,
		record,
		...(record ? { step_id: `step-${crypto.randomUUID()}` } : {}),
	};
	const key = crypto.randomUUID();
	emit('frame-invalidated');
	let command: EditorCommand;
	try {
		command = await appApi.createBrowserCommand(session.session_id, input, key);
	} catch (cause: any) {
		if (!cause?.error && !disposed && epoch === generation) unknownRequest.value = { sessionId: session.session_id, key, input };
		throw cause;
	}
	if (disposed || epoch !== generation) return command;
	pending.value = command;
	position.value = undefined;
	return poll(command, epoch);
}
async function executePathAction(action: Partial<EditorAction>) {
	return send(action, false, true);
}
async function executeForm() {
	if (!canExecute.value || !formValid.value) return;
	submitting.value = true;
	notice.value = '';
	try {
		await send(configuredAction(), mode.value === 'record');
	} catch (cause: any) {
		reportFailure(cause, `执行网页动作“${labels[actionType.value] || actionType.value}”`);
	} finally {
		submitting.value = false;
	}
}
async function runStep(step: Record<string, any>) {
	if (!canExecute.value) return;
	submitting.value = true;
	notice.value = '';
	try {
		await send(stepAction(step), false);
	} catch (cause: any) {
		reportFailure(cause, '执行当前流程步骤');
	} finally {
		submitting.value = false;
	}
}
async function queryPending() {
	const command = pending.value;
	if (!command || querying.value) return;
	querying.value = true;
	try {
		await settle(await appApi.browserCommand(command.session_id, command.command_id));
	} catch {
		notice.value = '回执查询失败；不会自动重发动作。';
	} finally {
		querying.value = false;
	}
}
async function resolveUnknown() {
	const previous = unknownRequest.value;
	if (!previous || querying.value) return;
	querying.value = true;
	try {
		const command = await appApi.createBrowserCommand(previous.sessionId, previous.input, previous.key);
		unknownRequest.value = undefined;
		pending.value = command;
		await poll(command, generation);
	} catch (cause: any) {
		reportFailure(cause, '查询上一动作回执');
	} finally {
		querying.value = false;
	}
}
async function saveCheckpoint() {
	if (!canEdit.value || !steps.value.length) return;
	const through = steps.value[selectedStep.value >= 0 ? selectedStep.value : steps.value.length - 1];
	const name = window.prompt('检查点名称（保存至当前选中步骤）', `检查点 ${checkpoints.value.length + 1}`);
	if (!name) return;
	try {
		await appApi.createCheckpoint(props.draft.id, { name, expected_revision: props.draft.revision, through_step_id: through.step_id });
		await refreshCheckpoints();
	} catch (cause: any) {
		reportFailure(cause, '保存流程检查点');
	}
}
async function removeCheckpoint(checkpoint: EditorCheckpoint) {
	try {
		await appApi.deleteCheckpoint(props.draft.id, checkpoint.checkpoint_id);
		await refreshCheckpoints();
	} catch {
		notice.value = '检查点删除失败。';
	}
}
async function replay(checkpoint: EditorCheckpoint) {
	if (
		!canEdit.value ||
		!supported.value.length ||
		!window.confirm(
			`从入口重放“${checkpoint.name}”的 ${checkpoint.snapshot.steps.length} 步快照。会关闭当前会话并重新打开；点击/输入可能产生外部操作，不会自动重试失败动作。确认？`
		)
	)
		return;
	replaying.value = true;
	stopReplay.value = false;
	notice.value = '';
	const oldID = props.session?.session_id;
	try {
		if (oldID) await appApi.closeBrowserSession(oldID);
		const session = await appApi.createBrowserSession(props.draft.id, props.draft.revision, crypto.randomUUID());
		if (disposed) return;
		emit('session-state', session);
		// Wait for parent prop propagation before sending bounded sequential actions.
		await new Promise((resolve) => setTimeout(resolve, 0));
		const sequence: Partial<EditorAction>[] = [
			{ type: 'navigate', value: checkpoint.snapshot.entry_url, timeout_ms: 10000 },
			...checkpoint.snapshot.steps.map(stepAction),
		];
		for (let i = 0; i < sequence.length; i++) {
			if (disposed || stopReplay.value) break;
			if (props.session?.session_id !== session.session_id) throw new Error('会话已改变，检查点重放中断');
			notice.value = `正在重放 ${i + 1}/${sequence.length}；检查点 revision ${checkpoint.draft_revision}`;
			const result = await send(sequence[i], false, true);
			if (result.status !== 'succeeded') throw new Error('重放已停止：动作失败或结果未确认，请查看回执');
		}
		if (!stopReplay.value) notice.value = '检查点重放完成。当前页面可继续配置；没有写入正式采集数据。';
		else notice.value = '已停止派发后续步骤；当前动作已发出的请求仍需查看回执。';
	} catch (cause: any) {
		reportFailure(cause, '重放流程');
	} finally {
		replaying.value = false;
	}
}
watch(
	() => props.session?.session_id,
	async (id, old) => {
		generation++;
		pending.value = undefined;
		unknownRequest.value = undefined;
		last.value = undefined;
		position.value = undefined;
		positionPageStateID.value = '';
		if (!replaying.value) stopReplay.value = true;
		await refreshHistory(id);
	}
);
onMounted(async () => {
	value.value = props.draft.entry_url;
	try {
		const capabilities = await appApi.capabilities();
		supported.value = capabilities.supported_actions;
		inspectionSupported.value = !!capabilities.supports_live_inspection;
		recheckSupported.value = !!capabilities.supports_locator_recheck;
		recordsSupported.value = !!capabilities.supports_record_preview;
	} catch {
		supported.value = [];
	}
	await Promise.all([refreshCheckpoints(), refreshHistory()]);
});
onBeforeUnmount(() => {
	disposed = true;
	wheelQueue.dispose();
	generation++;
	stopReplay.value = true;
});
</script>
