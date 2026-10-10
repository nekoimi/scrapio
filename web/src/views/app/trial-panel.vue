<template>
	<section class="app-card trial-panel">
		<div class="pane-heading">
			<h2>全链路试采</h2>
			<button :disabled="busy" @click="loadHistory">刷新历史</button>
		</div>
		<p class="muted">冻结已保存草稿，验证动作、提取与输出。实时使用独立会话；不写正式记录，不触发插件。</p>
		<p v-if="!draft.definition.output" class="status-warn">先在保存步骤确认数据表和输出配置。</p>
		<div class="command-form-grid">
			<label
				>模式<select v-model="mode" :disabled="busy">
					<option value="offline">离线 · 固定输入</option>
					<option value="live">实时 · 从入口执行</option>
				</select></label
			>
			<label>时间预算（秒）<input v-model.number="seconds" type="number" min="5" max="120" /></label>
			<label>文档页预算<input v-model.number="pages" type="number" min="1" max="10" /></label>
			<label>输出候选预算<input v-model.number="records" type="number" min="1" max="20" /></label>
			<label>详情访问预算<input v-model.number="details" type="number" min="0" max="5" /></label>
		</div>
		<template v-if="mode === 'live'">
			<label>允许页面来源（每行一个完整 origin）<textarea v-model="origins" class="full-input" rows="3" :disabled="busy" /></label>
			<p class="status-warn">
				会执行保存的点击、输入、HTTP POST，网站可能产生副作用；不复制编辑会话登录状态。来源检查限制导航目标及动作后的页面，不拦截网站所有网络请求。
			</p>
			<label><input v-model="confirmed" type="checkbox" :disabled="busy" />我确认本次访问范围及保存的动作，可以执行实时试采</label>
		</template>
		<template v-else>
			<p class="muted">每个提取步骤需 list 输入，可附 detail 输入。离线不执行动作、详情访问或翻页；存在这些路径时标为有限验证。</p>
			<div v-for="step in recordSteps" :key="step.step_id" class="trial-input-row">
				<strong>{{ step.step_id }}</strong>
				<label
					>list 快照<select v-model="inputs[step.step_id + ':list']">
						<option value="">选择固定输入</option>
						<option v-for="item in availableInputs" :key="item.capture_id" :value="item.capture_id">{{ item.label }}</option>
					</select></label
				>
				<label v-if="step.config?.detail"
					>detail 快照<select v-model="inputs[step.step_id + ':detail']">
						<option value="">不提供详情输入</option>
						<option v-for="item in availableInputs" :key="item.capture_id" :value="item.capture_id">{{ item.label }}</option>
					</select></label
				>
			</div>
			<button v-if="sampleCursor" @click="loadSamples">加载更多样例输入</button>
		</template>
		<div class="editor-actions">
			<button :disabled="busy || blocked || !draft.definition.output || !!pendingKey || (mode === 'live' && !confirmed)" @click="start">
				创建试采</button
			><button v-if="pendingKey" :disabled="busy" @click="recover">查询原创建请求</button
			><button v-if="pendingKey" :disabled="busy" @click="clearPending">结束本地等待</button
			><button v-if="selected && !terminal(selected.status)" :disabled="busy || selected.cancel_requested" @click="cancelTrial">
				{{ selected.cancel_requested ? '正在停止…' : '取消试采' }}</button
			><button v-if="selected" @click="refreshSelected">刷新状态</button>
		</div>
		<p v-if="message" class="app-error" role="status">{{ message }}</p>
		<p class="muted">最近 20 次；每人最多 3 个活动试采、100 次历史。响应丢失时查询原请求，不自动重复执行。</p>
		<ul>
			<li v-for="item in history" :key="item.trial_id">
				<button @click="selectTrial(item)">
					{{ label(item.status) }} · revision {{ item.collector_revision }} · {{ item.input.mode === 'live' ? '实时' : '离线' }} ·
					{{ new Date(item.created_at).toLocaleString() }}</button
				><button v-if="terminal(item.status)" :disabled="busy" @click="deleteTrial(item)">删除证据</button>
			</li>
		</ul>
		<section v-if="selected">
			<h3>{{ label(selected.status) }} · {{ selected.trial_id }}</h3>
			<p>
				冻结 revision {{ selected.collector_revision }} · {{ selected.current_step_id || '入口' }} / {{ selected.current_stage || '排队' }} · 正式写入
				0
			</p>
			<p v-if="selected.collector_revision !== draft.revision" class="status-warn">草稿已变化，以下证据属于旧 revision，不能用来放行当前草稿。</p>
			<p v-if="selected.summary.stop_reason">
				停止原因：{{ selected.summary.stop_reason }} · {{ selected.summary.failed_step_id }} / {{ selected.summary.failed_stage }}
			</p>
			<button v-if="selected.summary.failed_step_id" :disabled="selected.collector_revision !== draft.revision" @click="focusFailure">
				返回失败步骤
			</button>
			<p v-for="warning in selected.summary.warnings || []" :key="warning" class="status-warn">{{ warning }}</p>
			<details v-if="selected.summary.output" open>
				<summary>保存预判（正式运行会重新判断数据库状态）</summary>
				<pre>{{ JSON.stringify(selected.summary.output, null, 2) }}</pre>
			</details>
			<details open>
				<summary>步骤事件（有序、可恢复）</summary>
				<ol>
					<li v-for="event in events" :key="event.sequence">
						#{{ event.sequence }} {{ event.payload.step_id || '入口' }} / {{ event.payload.stage }} · {{ event.payload.status }}
						{{ event.payload.code }}
					</li>
				</ol>
			</details>
			<section v-for="document in documents" :key="document.document_id" class="trial-document">
				<h4>{{ document.step_id }} / {{ document.stage }} · 列表页 {{ document.list_page }} · 来源记录 {{ document.parent_record_index + 1 }}</h4>
				<p class="muted">{{ document.source_url }} · hash {{ document.content_hash }} · {{ document.extraction.interpreter_version }}</p>
				<div class="record-preview-table">
					<table>
						<thead>
							<tr>
								<th>记录</th>
								<th>字段</th>
								<th>原值</th>
								<th>提取值</th>
								<th>诊断</th>
							</tr>
						</thead>
						<tbody>
							<template v-for="record in document.extraction.records" :key="record.index"
								><tr v-for="field in record.fields" :key="field.field_key">
									<td>{{ record.index + 1 }}</td>
									<td>
										<button
											:disabled="selected.collector_revision !== draft.revision"
											@click="emit('focus-field', document.step_id, field.field_key, document.stage)"
										>
											{{ field.name }}
										</button>
									</td>
									<td>
										<pre>{{ field.raw_json }}</pre>
									</td>
									<td>
										<pre>{{ field.value_json }}</pre>
									</td>
									<td>{{ field.errors.join('、') || '通过' }}</td>
								</tr></template
							>
						</tbody>
					</table>
				</div>
			</section>
			<button v-if="nextCursor" :disabled="resultsBusy" @click="loadResults(true)">加载更多文档</button>
		</section>
	</section>
</template>
<script setup lang="ts">
import { computed, onMounted, onBeforeUnmount, ref, watch } from 'vue';
import { appApi, type Collector, type InputCapture, type SavedSample, type Trial, type TrialDocument, type TrialEvent, type TrialInput } from './api';
const props = defineProps<{ draft: Collector; capture?: InputCapture; blocked: boolean }>();
const emit = defineEmits<{ (event: 'focus-field', step: string, key: string, stage: string): void }>();
const mode = ref<'offline' | 'live'>('offline'),
	seconds = ref(60),
	pages = ref(5),
	records = ref(20),
	details = ref(2),
	origins = ref(''),
	confirmed = ref(false),
	inputs = ref<Record<string, string>>({});
const history = ref<Trial[]>([]),
	samples = ref<SavedSample[]>([]),
	sampleCursor = ref(''),
	selected = ref<Trial>(),
	documents = ref<TrialDocument[]>([]),
	events = ref<TrialEvent[]>([]),
	nextCursor = ref(''),
	pendingKey = ref(''),
	busy = ref(false),
	resultsBusy = ref(false),
	message = ref('');
let disposed = false,
	epoch = 0,
	controller: AbortController | undefined,
	pollTimer: ReturnType<typeof setInterval> | undefined,
	polling = false;
const recordSteps = computed(() => (props.draft.definition.steps || []).filter((step: any) => ['record_set', 'json_records'].includes(step.type)));
const availableInputs = computed(() => {
	const items = samples.value.map((sample) => ({ capture_id: sample.capture_id, label: `${sample.name} · ${sample.step_id}/${sample.stage}` }));
	if (props.capture?.status === 'succeeded') items.unshift({ capture_id: props.capture.capture_id, label: `当前快照 · ${props.capture.capture_id}` });
	return items.filter((item, index) => items.findIndex((other) => other.capture_id === item.capture_id) === index);
});
const keyStorage = () => `scrapio:v22:trial-pending:${props.draft.id}`;
function terminal(status: string) {
	return !['queued', 'running'].includes(status);
}
function label(status: string) {
	return (
		(
			{
				queued: '排队中',
				running: '执行中',
				succeeded: '完成',
				partial: '部分通过',
				limited: '有限验证',
				failed: '失败',
				cancelled: '已取消',
			} as Record<string, string>
		)[status] || status
	);
}
function error(cause: any) {
	return cause?.error?.message || cause?.message || '请求失败，请查询原试采状态';
}
async function loadSamples() {
	try {
		const page = await appApi.samples(props.draft.id, sampleCursor.value);
		if (!disposed) {
			samples.value.push(...page.items);
			sampleCursor.value = page.next_cursor;
		}
	} catch (cause) {
		message.value = error(cause);
	}
}
async function loadHistory() {
	try {
		const page = await appApi.trials(props.draft.id);
		if (!disposed) history.value = page.items;
	} catch (cause) {
		message.value = error(cause);
	}
}
function stop() {
	controller?.abort();
	controller = undefined;
	if (pollTimer) clearInterval(pollTimer);
	pollTimer = undefined;
	epoch++;
}
async function selectTrial(value: Trial) {
	stop();
	selected.value = value;
	documents.value = [];
	events.value = [];
	nextCursor.value = '';
	const token = epoch;
	await loadResults(false, token);
	if (disposed || token !== epoch) return;
	startEvents(value.trial_id, token);
	if (!terminal(value.status))
		pollTimer = setInterval(() => {
			if (!polling) {
				polling = true;
				void refreshSelected().finally(() => (polling = false));
			}
		}, 2000);
}
async function loadResults(more = false, token = epoch) {
	if (!selected.value) return;
	resultsBusy.value = true;
	try {
		const result = await appApi.trialResults(selected.value.trial_id, more ? nextCursor.value : '');
		if (!disposed && token === epoch) {
			documents.value = more ? [...documents.value, ...result.items] : result.items;
			nextCursor.value = result.next_cursor;
		}
	} catch (cause) {
		if (token === epoch) message.value = error(cause);
	} finally {
		if (token === epoch) resultsBusy.value = false;
	}
}
async function refreshSelected() {
	if (!selected.value) return;
	const id = selected.value.trial_id,
		token = epoch;
	try {
		const value = await appApi.trial(id);
		if (!disposed && token === epoch) {
			selected.value = value;
			const index = history.value.findIndex((item) => item.trial_id === id);
			if (index >= 0) history.value[index] = value;
			if (terminal(value.status)) {
				if (pollTimer) clearInterval(pollTimer);
				pollTimer = undefined;
				await loadResults(false, token);
			}
		}
	} catch (cause) {
		if (token === epoch) message.value = error(cause);
	}
}
function startEvents(id: string, token: number) {
	controller = new AbortController();
	const own = controller;
	void (async () => {
		let failures = 0;
		while (!disposed && !own.signal.aborted && token === epoch) {
			try {
				const after = events.value.length ? events.value[events.value.length - 1].sequence : 0;
				await appApi.streamTrial(id, after, own.signal, (event: TrialEvent) => {
					if (token !== epoch || disposed) return;
					if (!events.value.some((item) => item.sequence === event.sequence)) events.value.push(event);
					failures = 0;
				});
				await refreshSelected();
				if (selected.value && terminal(selected.value.status)) return;
			} catch (cause: any) {
				if (own.signal.aborted) return;
				if (cause?.terminal || ++failures >= 3) {
					message.value = '事件流连接停止；可刷新状态和结果，运行继续。';
					return;
				}
			}
			await new Promise((resolve) => setTimeout(resolve, 1500));
		}
	})();
}
async function start() {
	if (busy.value || props.blocked || pendingKey.value) return;
	busy.value = true;
	message.value = '';
	const key = crypto.randomUUID();
	pendingKey.value = key;
	sessionStorage.setItem(keyStorage(), key);
	try {
		const refs = Object.entries(inputs.value)
			.filter(([, capture]) => capture)
			.map(([name, capture_id]) => {
				const index = name.lastIndexOf(':');
				return { step_id: name.slice(0, index), stage: name.slice(index + 1) as 'list' | 'detail', capture_id };
			});
		const input: TrialInput = {
			expected_revision: props.draft.revision,
			mode: mode.value,
			confirmed: mode.value === 'live' && confirmed.value,
			allowed_origins:
				mode.value === 'live'
					? origins.value
							.split(/\r?\n/)
							.map((origin) => origin.trim())
							.filter(Boolean)
					: [],
			budget: { seconds: seconds.value, pages: pages.value, records: records.value, details: details.value },
			inputs: mode.value === 'offline' ? refs : [],
		};
		const value = await appApi.createTrial(props.draft.id, input, key);
		if (disposed) return;
		pendingKey.value = '';
		sessionStorage.removeItem(keyStorage());
		history.value = [value, ...history.value];
		confirmed.value = false;
		await selectTrial(value);
	} catch (cause: any) {
		if (cause?.error && cause.error.code !== 'INTERNAL') {
			pendingKey.value = '';
			sessionStorage.removeItem(keyStorage());
		}
		message.value = error(cause);
	} finally {
		busy.value = false;
	}
}
async function recover() {
	if (!pendingKey.value || busy.value) return;
	busy.value = true;
	try {
		const value = await appApi.trialByKey(pendingKey.value);
		if (disposed) return;
		pendingKey.value = '';
		sessionStorage.removeItem(keyStorage());
		await loadHistory();
		await selectTrial(value);
	} catch (cause) {
		message.value = error(cause);
	} finally {
		busy.value = false;
	}
}
function clearPending() {
	if (window.confirm('结束本地等待不会取消服务器上可能已创建的试采，请先检查历史。确认？')) {
		pendingKey.value = '';
		sessionStorage.removeItem(keyStorage());
	}
}
async function cancelTrial() {
	if (!selected.value || busy.value) return;
	busy.value = true;
	try {
		selected.value = await appApi.cancelTrial(selected.value.trial_id);
		await refreshSelected();
	} catch (cause) {
		message.value = error(cause);
	} finally {
		busy.value = false;
	}
}
async function deleteTrial(value: Trial) {
	if (!window.confirm('删除这次试采的文档与事件证据？不能恢复。')) return;
	busy.value = true;
	try {
		await appApi.deleteTrial(value.trial_id);
		if (selected.value?.trial_id === value.trial_id) {
			stop();
			selected.value = undefined;
			documents.value = [];
			events.value = [];
		}
		await loadHistory();
	} catch (cause) {
		message.value = error(cause);
	} finally {
		busy.value = false;
	}
}
function focusFailure() {
	if (selected.value?.summary.failed_step_id)
		emit('focus-field', selected.value.summary.failed_step_id, '', selected.value.summary.failed_stage === 'detail' ? 'detail' : 'list');
}
watch(origins, () => (confirmed.value = false));
onMounted(async () => {
	try {
		origins.value = new URL(props.draft.entry_url).origin;
	} catch {}
	pendingKey.value = sessionStorage.getItem(keyStorage()) || '';
	await Promise.all([loadHistory(), loadSamples()]);
	if (disposed) return;
	if (history.value[0]) await selectTrial(history.value[0]);
});
onBeforeUnmount(() => {
	disposed = true;
	stop();
});
defineExpose({ hasUnsavedChanges: () => busy.value || !!pendingKey.value });
</script>
