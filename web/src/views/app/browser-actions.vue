<template>
  <div class="editor-action-workspace">
    <aside class="editor-step-pane">
      <div class="pane-heading"><h3>采集步骤</h3><span>{{ steps.length }} 步</span></div>
      <p class="muted">步骤修改只改草稿，不操作网页。</p>
      <ol class="step-list">
        <li v-for="(step, index) in steps" :key="step.step_id || index" :class="{ selected: selectedStep === index }">
          <button class="step-title" @click="selectStep(index)">{{ index + 1 }}. {{ stepLabel(step) }}</button>
          <small>{{ step.step_id }}</small>
          <div class="step-controls">
            <button :disabled="!canEdit || index === 0" @click="moveStep(index, -1)" aria-label="上移步骤">↑</button>
            <button :disabled="!canEdit || index === steps.length - 1" @click="moveStep(index, 1)" aria-label="下移步骤">↓</button>
            <button :disabled="!canEdit" @click="removeStep(index)">删除</button>
            <button :disabled="!canExecute || !executable(step)" @click="runStep(step)">单步执行</button>
          </div>
        </li>
      </ol>
      <p v-if="!steps.length" class="muted">切换“录制操作”，成功动作将自动加入步骤。</p>
      <button class="secondary-action" :disabled="!canEdit || !steps.length" @click="saveCheckpoint">保存检查点</button>
      <p class="muted">保存至选中步骤，重放使用这份快照。</p>
      <div v-for="checkpoint in checkpoints" :key="checkpoint.checkpoint_id" class="checkpoint-row">
        <strong>{{ checkpoint.name }}</strong><small>revision {{ checkpoint.draft_revision }} · {{ checkpoint.snapshot.steps.length }} 步</small>
        <button :disabled="!canEdit || replaying || !supported.length" @click="replay(checkpoint)">从入口重放</button>
        <button :disabled="busy || replaying" @click="removeCheckpoint(checkpoint)">删除</button>
      </div>
      <button v-if="replaying" class="secondary-action" @click="stopReplay = true">停止后续步骤</button>
    </aside>
    <section class="editor-command-pane">
      <div class="editor-mode-tabs" role="tablist" aria-label="网页操作模式">
        <button role="tab" :aria-selected="mode === 'browse'" :disabled="busy || replaying" @click="mode = 'browse'">浏览网页</button>
        <button role="tab" disabled title="A05 将提供元素选择；不执行网页点击">选择记录 · 待接入</button>
        <button role="tab" disabled title="A05 将提供字段选择；不执行网页点击">选择字段 · 待接入</button>
        <button role="tab" :aria-selected="mode === 'record'" :disabled="busy || replaying" @click="mode = 'record'">录制操作</button>
      </div>
      <p class="mode-description">{{ mode === 'record' ? '录制中：成功动作加入草稿；失败动作只保留回执。' : '浏览模式：执行真实操作，默认不加入草稿。' }} 点击画面会准备点击动作，确认后才执行。</p>
      <form @submit.prevent="executeForm">
        <div class="command-form-grid">
          <label>动作<select v-model="actionType" :disabled="busy || replaying" @change="onActionTypeChanged"><option v-for="action in actions" :key="action" :value="action">{{ labels[action] }}</option></select></label>
          <label>最长等待（毫秒）<input v-model.number="timeout" type="number" min="100" max="30000" step="100" /></label>
        </div>
        <label v-if="['navigate','input','scroll','wait'].includes(actionType)">{{ actionType === 'navigate' ? '目标地址' : actionType === 'input' ? '普通文本（不支持密码/验证码）' : actionType === 'scroll' ? '滚动像素（负数向上）' : '等待毫秒；填写定位器时改为等待元素' }}<input v-model="value" :type="actionType === 'navigate' ? 'url' : 'text'" :placeholder="actionType === 'navigate' ? draft.entry_url : ''" maxlength="4096" /></label>
        <div v-if="['click','input','wait'].includes(actionType) && !position" class="command-form-grid">
          <label>定位方式<select v-model="strategy"><option value="css">CSS</option><option value="xpath">XPath</option></select></label>
          <label>定位器<input v-model="expression" placeholder="例如 #load-more 或 //button[@id='more']" maxlength="2048" /></label>
        </div>
        <p v-if="position" class="position-notice">画面点击坐标 {{ Math.round(position.x) }}, {{ Math.round(position.y) }}。<button type="button" @click="position = undefined">改用定位器</button></p>
        <div class="editor-actions">
          <button type="submit" :disabled="!canExecute || !supported.includes(actionType)">{{ busy ? '等待动作回执…' : mode === 'record' ? '执行并录制' : '执行动作' }}</button>
          <button type="button" class="secondary-action" :disabled="!canEdit || !formValid || !!position || actionType === 'input'" @click="addConfiguredStep">只加入步骤</button>
          <button v-if="selectedStep >= 0" type="button" class="secondary-action" :disabled="!canEdit || !formValid || !!position || actionType === 'input' || !executable(steps[selectedStep])" @click="updateConfiguredStep">更新选中步骤</button>
          <button v-if="last?.status === 'succeeded' && last.result.recording_status !== 'recorded'" type="button" class="secondary-action" :disabled="!canEdit || last.type === 'input'" @click="addLastStep">将成功动作加入步骤</button>
        </div>
      </form>
      <p v-if="actionType === 'input'" class="muted">输入步骤须在录制模式执行成功后保存；密码与验证码输入框会被拒绝。请勿在普通文本或 JSON 草稿中填写秘密。</p>
      <p v-if="!supported.length" class="muted">动作服务尚未就绪或版本不支持；草稿编辑仍可使用。</p>
      <p v-if="dirty" class="status-warn">有未保存修改，请先保存再执行。</p>
      <p v-if="notice" class="app-error" role="status">{{ notice }}</p>
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
      <details class="command-history"><summary>最近动作（最多 50 条）</summary><div v-for="command in history" :key="command.command_id"><span>{{ labels[command.type] }} · {{ statusLabel(command.status) }}</span><button @click="last = command">查看回执</button></div></details>
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { appApi, type BrowserSession, type Collector, type EditorAction, type EditorCheckpoint, type EditorCommand } from './api';

const props = defineProps<{ draft: Collector; session?: BrowserSession; dirty: boolean; saving: boolean }>();
const emit = defineEmits<{ (event: 'draft-saved', draft: Collector): void; (event: 'session-state', session: BrowserSession): void }>();
const labels: Record<string, string> = { navigate: '打开网址', back: '后退', forward: '前进', refresh: '刷新页面', click: '点击元素', input: '输入文本', wait: '等待', scroll: '滚动' };
const actions = Object.keys(labels);
const mode = ref<'browse' | 'record'>('browse');
const supported = ref<string[]>([]);
const actionType = ref('navigate');
const value = ref('');
const strategy = ref<'css' | 'xpath'>('css');
const expression = ref('');
const timeout = ref(10000);
const position = ref<{ x: number; y: number }>();
const positionPageStateID = ref('');
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
const unknownRequest = ref<{ sessionId: string; key: string; input: EditorAction }>();
let disposed = false;
let generation = 0;
const steps = computed<Record<string, any>[]>(() => Array.isArray(props.draft.definition.steps) ? props.draft.definition.steps : []);
const busy = computed(() => submitting.value || savingSteps.value || !!pending.value || !!unknownRequest.value);
const canEdit = computed(() => Array.isArray(props.draft.definition.steps) && !props.dirty && !props.saving && !busy.value && !replaying.value);
const canExecute = computed(() => canEdit.value && !!props.session?.available);
const formValid = computed(() => Number.isInteger(timeout.value) && timeout.value >= 100 && timeout.value <= 30000 && (!(actionType.value === 'input' || actionType.value === 'click') || !!position.value || !!expression.value.trim()));

function statusLabel(status: string) { return ({ queued: '排队中', running: '执行中', succeeded: '成功', failed: '失败', uncertain: '结果未确认' } as Record<string, string>)[status] || status; }
function executable(step: Record<string, any>) { return step.type === 'navigate' || step.type === 'action' && actions.includes(step.config?.action); }
function stepLabel(step: Record<string, any>) { return step.type === 'navigate' ? `打开 ${step.config?.url || '网址'}` : labels[step.config?.action] || step.type; }
function selectStep(index: number) {
  selectedStep.value = index;
  const step = steps.value[index];
  if (!executable(step)) return;
  actionType.value = step.type === 'navigate' ? 'navigate' : step.config.action;
  value.value = step.type === 'navigate' ? step.config.url : step.config.value || '';
  expression.value = step.config.locator?.expression || '';
  strategy.value = step.config.locator?.strategy || 'css';
  timeout.value = step.config.timeout_ms || 10000;
  position.value = undefined;
}
function onActionTypeChanged() {
  position.value = undefined;
  value.value = actionType.value === 'navigate' ? props.draft.entry_url : actionType.value === 'scroll' ? '600' : actionType.value === 'wait' ? '1000' : '';
}
function configuredAction(): Partial<EditorAction> {
  const action: Partial<EditorAction> = { type: actionType.value, value: value.value, timeout_ms: timeout.value };
  if (position.value) { action.position = { ...position.value }; action.page_state_id = positionPageStateID.value; }
  else if (['click','input','wait'].includes(action.type!) && expression.value.trim()) action.locator = { strategy: strategy.value, expression: expression.value.trim() };
  return action;
}
function stepAction(step: Record<string, any>): Partial<EditorAction> {
  if (!executable(step)) throw new Error('这一步不是可执行的网页动作');
  return { type: step.type === 'navigate' ? 'navigate' : step.config.action, value: step.type === 'navigate' ? step.config.url : step.config.value || '', locator: step.config.locator, timeout_ms: step.config.timeout_ms || 10000 };
}
function actionStep(action: Partial<EditorAction>) {
  return action.type === 'navigate'
    ? { step_id: `step-${crypto.randomUUID()}`, type: 'navigate', config: { url: action.value, timeout_ms: action.timeout_ms } }
    : { step_id: `step-${crypto.randomUUID()}`, type: 'action', config: { action: action.type, value: action.value || '', timeout_ms: action.timeout_ms, ...(action.locator ? { locator: action.locator } : {}) } };
}
async function saveSteps(next: Record<string, any>[]) {
  if (!canEdit.value) return;
  savingSteps.value = true; notice.value = '';
  try {
    const draft = await appApi.updateCollector(props.draft.id, { name: props.draft.name, expected_revision: props.draft.revision, definition: { ...props.draft.definition, steps: next } });
    if (!disposed) emit('draft-saved', draft);
  } catch (cause: any) { notice.value = cause?.error?.message || '步骤保存失败，当前列表仍保留；请处理草稿冲突后重试。'; }
  finally { savingSteps.value = false; }
}
async function moveStep(index: number, offset: number) { const next = [...steps.value]; [next[index], next[index + offset]] = [next[index + offset], next[index]]; await saveSteps(next); selectedStep.value = index + offset; }
async function removeStep(index: number) { await saveSteps(steps.value.filter((_, i) => i !== index)); selectedStep.value = -1; }
async function addConfiguredStep() { if (formValid.value && !position.value && actionType.value !== 'input') await saveSteps([...steps.value, actionStep(configuredAction())]); }
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
  if (!canExecute.value || !pageStateID) return;
  positionPageStateID.value = pageStateID;
  position.value = { x, y }; actionType.value = 'click';
  notice.value = '已准备点击动作，请在右侧确认执行；网页还未被点击。';
}
defineExpose({ frameClick });

async function refreshHistory(id = props.session?.session_id) {
  if (!id) { history.value = []; return; }
  try {
    const result = await appApi.browserCommands(id);
    if (disposed || id !== props.session?.session_id) return;
    history.value = result.items;
    const active = result.items.find(item => ['queued','running','uncertain'].includes(item.status));
    if (active && !pending.value) pending.value = active;
  } catch { notice.value = '无法读取动作历史，继续操作前请检查服务连接。'; }
}
async function refreshCheckpoints() { try { checkpoints.value = (await appApi.checkpoints(props.draft.id)).items; } catch { notice.value = '检查点暂时无法读取。'; } }
async function refreshState(id: string) { const state = await appApi.browserSession(id); if (!disposed && id === props.session?.session_id) emit('session-state', state); return state; }
async function settle(command: EditorCommand) {
  if (disposed || command.session_id !== props.session?.session_id) return;
  if (command.status === 'queued' || command.status === 'running' || command.status === 'uncertain') { pending.value = command; return; }
  pending.value = undefined; last.value = command;
  if (command.result.recording_status === 'recorded') {
    const latest = await appApi.collector(props.draft.id);
    if (!disposed) emit('draft-saved', latest);
  }
  await refreshState(command.session_id).catch(() => { notice.value = '动作回执已保存；页面状态暂时无法更新。'; });
  await refreshHistory(command.session_id);
  if (command.result.recording_error) notice.value = command.result.recording_error;
}
async function poll(command: EditorCommand, epoch: number): Promise<EditorCommand> {
  let current = command;
  // Bounded UI polling; receipt can always be queried manually afterwards.
  for (let i = 0; i < 45 && ['queued','running'].includes(current.status); i++) {
    await new Promise(resolve => setTimeout(resolve, 1000));
    if (disposed || epoch !== generation || command.session_id !== props.session?.session_id) throw new Error('操作面板已离开；回执仍保留在会话历史');
    current = await appApi.browserCommand(command.session_id, command.command_id);
    pending.value = current;
  }
  await settle(current);
  return current;
}
async function send(action: Partial<EditorAction>, record: boolean, confirmed = false): Promise<EditorCommand> {
  const session = props.session;
  if (!session?.available || props.dirty || props.saving || pending.value || unknownRequest.value) throw new Error('会话不可用、草稿未同步或上一动作尚未确认');
  if (!supported.value.includes(action.type!)) throw new Error('当前浏览器服务不支持这个动作');
  if (!confirmed && ['click','input'].includes(action.type!) && !window.confirm(`即将在目标网站执行“${labels[action.type!]}”。可能触发表单提交或外部操作，确认执行？`)) throw new Error('已取消，未执行网页动作');
  const epoch = generation;
  const current = await refreshState(session.session_id);
  if (disposed || epoch !== generation || !current.available) throw new Error('会话已变化，未派发动作');
  if (action.position && action.page_state_id !== current.page_state_id) throw new Error('点击画面已过期，请在最新画面上重新选择位置');
  const input: EditorAction = { ...action, type: action.type!, timeout_ms: action.timeout_ms || 10000, page_state_id: current.page_state_id, expected_revision: props.draft.revision, confirmed: true, record, ...(record ? { step_id: `step-${crypto.randomUUID()}` } : {}) };
  const key = crypto.randomUUID();
  let command: EditorCommand;
  try { command = await appApi.createBrowserCommand(session.session_id, input, key); }
  catch (cause: any) {
    if (!cause?.error && !disposed && epoch === generation) unknownRequest.value = { sessionId: session.session_id, key, input };
    throw cause;
  }
  if (disposed || epoch !== generation) return command;
  pending.value = command;
  position.value = undefined;
  return poll(command, epoch);
}
async function executeForm() {
  if (!canExecute.value || !formValid.value) return;
  submitting.value = true; notice.value = '';
  try { await send(configuredAction(), mode.value === 'record'); }
  catch (cause: any) { notice.value = cause?.error?.message || cause?.message || '动作请求未确认，请查询回执。'; }
  finally { submitting.value = false; }
}
async function runStep(step: Record<string, any>) {
  if (!canExecute.value) return;
  submitting.value = true; notice.value = '';
  try { await send(stepAction(step), false); }
  catch (cause: any) { notice.value = cause?.error?.message || cause?.message || '单步执行未确认。'; }
  finally { submitting.value = false; }
}
async function queryPending() {
  const command = pending.value; if (!command || querying.value) return;
  querying.value = true;
  try { await settle(await appApi.browserCommand(command.session_id, command.command_id)); }
  catch { notice.value = '回执查询失败；不会自动重发动作。'; }
  finally { querying.value = false; }
}
async function resolveUnknown() {
  const previous = unknownRequest.value; if (!previous || querying.value) return;
  querying.value = true;
  try {
    const command = await appApi.createBrowserCommand(previous.sessionId, previous.input, previous.key);
    unknownRequest.value = undefined; pending.value = command; await poll(command, generation);
  } catch (cause: any) { notice.value = cause?.error?.message || '上一请求仍未确认，请检查页面或重新打开会话。'; }
  finally { querying.value = false; }
}
async function saveCheckpoint() {
  if (!canEdit.value || !steps.value.length) return;
  const through = steps.value[selectedStep.value >= 0 ? selectedStep.value : steps.value.length - 1];
  const name = window.prompt('检查点名称（保存至当前选中步骤）', `检查点 ${checkpoints.value.length + 1}`);
  if (!name) return;
  try { await appApi.createCheckpoint(props.draft.id, { name, expected_revision: props.draft.revision, through_step_id: through.step_id }); await refreshCheckpoints(); }
  catch (cause: any) { notice.value = cause?.error?.message || '检查点保存失败。'; }
}
async function removeCheckpoint(checkpoint: EditorCheckpoint) { try { await appApi.deleteCheckpoint(props.draft.id, checkpoint.checkpoint_id); await refreshCheckpoints(); } catch { notice.value = '检查点删除失败。'; } }
async function replay(checkpoint: EditorCheckpoint) {
  if (!canEdit.value || !supported.value.length || !window.confirm(`从入口重放“${checkpoint.name}”的 ${checkpoint.snapshot.steps.length} 步快照。会关闭当前会话并重新打开；点击/输入可能产生外部操作，不会自动重试失败动作。确认？`)) return;
  replaying.value = true; stopReplay.value = false; notice.value = '';
  const oldID = props.session?.session_id;
  try {
    if (oldID) await appApi.closeBrowserSession(oldID);
    const session = await appApi.createBrowserSession(props.draft.id, props.draft.revision, crypto.randomUUID());
    if (disposed) return;
    emit('session-state', session);
    // Wait for parent prop propagation before sending bounded sequential actions.
    await new Promise(resolve => setTimeout(resolve, 0));
    const sequence: Partial<EditorAction>[] = [{ type: 'navigate', value: checkpoint.snapshot.entry_url, timeout_ms: 10000 }, ...checkpoint.snapshot.steps.map(stepAction)];
    for (let i = 0; i < sequence.length; i++) {
      if (disposed || stopReplay.value) break;
      if (props.session?.session_id !== session.session_id) throw new Error('会话已改变，检查点重放中断');
      notice.value = `正在重放 ${i + 1}/${sequence.length}；检查点 revision ${checkpoint.draft_revision}`;
      const result = await send(sequence[i], false, true);
      if (result.status !== 'succeeded') throw new Error('重放已停止：动作失败或结果未确认，请查看回执');
    }
    if (!stopReplay.value) notice.value = '检查点重放完成。当前页面可继续配置；没有写入正式采集数据。';
    else notice.value = '已停止派发后续步骤；当前动作已发出的请求仍需查看回执。';
  } catch (cause: any) { notice.value = cause?.error?.message || cause?.message || '重放中断，不会自动继续。'; }
  finally { replaying.value = false; }
}
watch(() => props.session?.session_id, async (id, old) => {
  generation++;
  pending.value = undefined; unknownRequest.value = undefined; last.value = undefined;
  position.value = undefined; positionPageStateID.value = '';
  if (!replaying.value) stopReplay.value = true;
  await refreshHistory(id);
});
onMounted(async () => {
  value.value = props.draft.entry_url;
  try { supported.value = (await appApi.capabilities()).supported_actions; } catch { supported.value = []; }
  await Promise.all([refreshCheckpoints(), refreshHistory()]);
});
onBeforeUnmount(() => { disposed = true; generation++; stopReplay.value = true; });
</script>
