<template>
	<section class="app-card run-panel">
		<div class="pane-heading">
			<h2>正式运行 · v{{ version.number }}</h2>
			<button :disabled="busy" @click="loadHistory(false)">刷新运行历史</button>
		</div>
		<p class="muted">使用此发布版本的固定规则和数据表，当前草稿修改不会影响运行。确认后会访问网站并写入正式记录及来源观察。</p>
		<p class="status-warn">
			当前支持有界运行：最多两页列表，连续分页在 C01
			扩展。有限覆盖会明确标记；失败路径或无效/冲突候选不提交本批数据。浏览器使用独立会话，不继承编辑登录状态。
		</p>
		<fieldset :disabled="busy || !!pendingKey" class="run-budget">
			<div class="command-form-grid">
				<label>时间（秒）<input v-model.number="seconds" type="number" min="5" max="120" /></label>
				<label>文档页<input v-model.number="pages" type="number" min="1" max="10" /></label>
				<label>输出候选<input v-model.number="records" type="number" min="1" max="20" /></label>
				<label>详情访问<input v-model.number="details" type="number" min="0" max="5" /></label>
			</div>
			<label>允许页面来源（每行完整 origin）<textarea v-model="origins" class="full-input" rows="2" /></label>
			<p class="muted">预算默认取发布时配置；调整后须重新确认。来源检查约束导航目标及动作后页面，不能保证资源级网络隔离或撤销已发生的点击/POST。</p>
			<label><input v-model="confirmed" type="checkbox" />我确认此版本、范围和预算，允许本次真实动作/HTTP 请求及正式数据写入</label>
		</fieldset>
		<div class="editor-actions">
			<button :disabled="busy || !confirmed || !!pendingKey || !version.run_available" @click="start">运行已发布 v{{ version.number }}</button>
			<button v-if="pendingKey" :disabled="busy" @click="recover">查询原运行请求</button>
			<button v-if="pendingKey" :disabled="busy" @click="clearPending">结束本地等待</button>
		</div>
		<p v-if="message" class="app-error" role="status">{{ message }}</p>
		<p class="muted">响应未知时只查询原请求，不自动重发。服务中断或租约失效不会自动重放点击/POST；排队任务保留。每人最多三个活动运行。</p>
		<ul>
			<li v-for="item in history" :key="item.run_id">
				<button :disabled="busy" @click="selectRun(item)">
					v{{ item.version_number }} · {{ label(item.status) }} · {{ new Date(item.created_at).toLocaleString() }}
				</button>
			</li>
		</ul>
		<button v-if="historyCursor" :disabled="busy" @click="loadHistory(true)">加载更多运行</button>
		<section v-if="selected">
			<RunCompletion :run="selected" />
			<router-link :to="`/app/runs/${selected.run_id}`">打开运行详情与页面证据</router-link>
			<h3>{{ label(selected.status) }} · v{{ selected.version_number }}</h3>
			<p>
				运行 {{ selected.run_id }} · attempt {{ selected.attempt }} · {{ selected.current_step_id || '入口' }} /
				{{ selected.current_stage || '排队' }}
			</p>
			<p v-if="selected.version_id !== version.version_id" class="status-warn">以下结果属于另一个固定发布版本；当前运行配置不会改变它。</p>
			<p>
				停止原因 {{ selected.summary.stop_reason || '执行中' }} · 文档 {{ selected.summary.pages || documents.length }} · 候选
				{{ selected.summary.candidates || 0 }}
			</p>
			<p :class="selected.summary.committed ? 'status-good' : 'status-warn'">
				{{ selected.summary.committed ? '本批数据已原子提交' : terminal(selected.status) ? '本批未提交正式数据' : '执行中，尚未提交本批数据' }} · 新增
				{{ selected.summary.counts?.created || 0 }} / 更新 {{ selected.summary.counts?.updated || 0 }} / 未变
				{{ selected.summary.counts?.unchanged || 0 }}
			</p>
			<p v-for="warning in selected.summary.warnings || []" :key="warning" class="status-warn">{{ warning }}</p>
			<div class="editor-actions">
				<button :disabled="busy" @click="refreshSelected">刷新状态和结果</button
				><button v-if="!terminal(selected.status)" :disabled="busy || selected.cancel_requested" @click="cancel">
					{{ selected.cancel_requested ? '正在停止…' : '取消运行' }}
				</button>
			</div>
			<p class="muted">取消停止后续派生及写入，不能撤销已发生的网站动作；若完成事务先提交，取消返回已完成状态。</p>
			<details v-if="selected.summary.writes?.length" open>
				<summary>本次正式保存结果</summary>
				<div class="record-preview-table">
					<table>
						<thead>
							<tr>
								<th>决策</th>
								<th>记录 / 修订</th>
								<th>来源</th>
								<th>最终数据</th>
							</tr>
						</thead>
						<tbody>
							<tr v-for="write in selected.summary.writes" :key="write.observation_id">
								<td>
									{{ write.decision }}<small>{{ write.changed_fields.join('、') }}</small>
								</td>
								<td><router-link :to="`/app/records/${write.record_id}`">{{ write.record_id }}</router-link> / {{ write.record_revision }}</td>
								<td><router-link :to="`/app/pages/${write.document_id}`">{{ write.source_url }}</router-link> · {{ write.stage }} #{{ write.record_index + 1 }}</td>
								<td>
									<pre>{{ write.values_json }}</pre>
								</td>
							</tr>
						</tbody>
					</table>
				</div>
			</details>
			<details open>
				<summary>运行事件</summary>
				<ol>
					<li v-for="event in events" :key="event.sequence">
						#{{ event.sequence }} {{ event.payload.step_id || '入口' }} / {{ event.payload.stage }} · {{ event.payload.status }}
						{{ event.payload.code }}
					</li>
				</ol>
			</details>
			<section v-for="doc in documents" :key="doc.document_id" class="trial-document">
				<h4>{{ doc.step_id }}/{{ doc.stage }} · 列表页 {{ doc.list_page }} · 来源记录 {{ doc.parent_record_index + 1 }}</h4>
				<p class="muted">{{ doc.source_url }} · {{ doc.content_hash }}</p>
				<details>
					<summary>共用解释器字段诊断</summary>
					<div v-for="record in doc.extraction.records" :key="record.index">
						<p>记录 {{ record.index + 1 }} · {{ record.valid ? '有效' : '无效' }}</p>
						<div v-for="field in record.fields" :key="field.field_key">
							<strong>{{ field.name }} · {{ field.errors.join('、') || '通过' }}</strong>
							<pre>
原值 {{ field.raw_json }}
输出 {{ field.value_json }}</pre>
						</div>
					</div>
				</details>
			</section>
			<button v-if="resultCursor" :disabled="resultsBusy" @click="loadResults(true)">加载更多文档</button>
		</section>
	</section>
</template>
<script setup lang="ts">
import { onMounted, onBeforeUnmount, ref, watch } from 'vue';
import RunCompletion from './run-completion.vue';
import { appApi, type PublishedVersion, type FormalRun, type TrialDocument, type TrialEvent, type RunInput } from './api';
const props = defineProps<{ version: PublishedVersion }>();
const seconds = ref(60),
	pages = ref(5),
	records = ref(20),
	details = ref(2),
	origins = ref(''),
	confirmed = ref(false);
const history = ref<FormalRun[]>([]),
	historyCursor = ref(''),
	selected = ref<FormalRun>(),
	documents = ref<TrialDocument[]>([]),
	events = ref<TrialEvent[]>([]),
	resultCursor = ref(''),
	pendingKey = ref(''),
	busy = ref(false),
	resultsBusy = ref(false),
	message = ref('');
let disposed = false,
	epoch = 0,
	polling = false,
	controller: AbortController | undefined,
	pollTimer: ReturnType<typeof setInterval> | undefined;
const keyStorage = () => `scrapio:v22:run-pending:${props.version.collector_id}`;
function error(cause: any) {
	return cause?.error?.message || cause?.message || '运行响应未确认，请查询原请求';
}
function terminal(status: string) {
	return !['queued', 'running'].includes(status);
}
function label(status: string) {
	return (
		(
			{
				queued: '排队中',
				running: '执行中',
				succeeded: '已完成',
				limited: '有限覆盖',
				partial: '未完整通过',
				failed: '失败',
				cancelled: '已取消',
			} as Record<string, string>
		)[status] || status
	);
}
function clear() {
	pendingKey.value = '';
	sessionStorage.removeItem(keyStorage());
}
function clearPending() {
	if (window.confirm('结束本地等待不会取消服务器运行。请先查询原请求或刷新历史，确认？')) clear();
}
function stop() {
	controller?.abort();
	controller = undefined;
	if (pollTimer) clearInterval(pollTimer);
	pollTimer = undefined;
	resultsBusy.value = false;
	epoch++;
}
async function loadHistory(more = false) {
	if (busy.value) return;
	busy.value = true;
	try {
		const result = await appApi.runs(props.version.collector_id, more ? historyCursor.value : '');
		if (!disposed) {
			history.value = more ? [...history.value, ...result.items] : result.items;
			historyCursor.value = result.next_cursor;
		}
	} catch (cause) {
		message.value = error(cause);
	} finally {
		busy.value = false;
	}
}
async function selectRun(value: FormalRun) {
	stop();
	selected.value = value;
	documents.value = [];
	events.value = [];
	resultCursor.value = '';
	const token = epoch;
	await refreshSelected();
	if (disposed || token !== epoch) return;
	startEvents(value.run_id, token);
	if (!terminal(selected.value.status))
		pollTimer = setInterval(() => {
			if (!polling) {
				polling = true;
				void refreshSelected().finally(() => (polling = false));
			}
		}, 2000);
}
async function loadResults(more = false, token = epoch) {
	if (!selected.value || resultsBusy.value) return;
	resultsBusy.value = true;
	try {
		const result = await appApi.runResults(selected.value.run_id, more ? resultCursor.value : '');
		if (!disposed && token === epoch) {
			documents.value = more ? [...documents.value, ...result.items] : result.items;
			resultCursor.value = result.next_cursor;
		}
	} catch (cause) {
		if (token === epoch) message.value = error(cause);
	} finally {
		if (token === epoch) resultsBusy.value = false;
	}
}
async function refreshSelected() {
	if (!selected.value) return;
	const id = selected.value.run_id,
		token = epoch;
	try {
		const value = await appApi.run(id);
		if (!disposed && token === epoch && value.event_seq >= (selected.value?.event_seq || 0)) {
			selected.value = value;
			const index = history.value.findIndex((item) => item.run_id === id);
			if (index >= 0) history.value[index] = value;
			if (terminal(value.status) && pollTimer) {
				clearInterval(pollTimer);
				pollTimer = undefined;
			}
			await loadResults(false, token);
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
				await appApi.streamRun(id, after, own.signal, (event) => {
					if (token !== epoch || disposed) return;
					if (!events.value.some((item) => item.sequence === event.sequence)) events.value.push(event);
					failures = 0;
				});
				await refreshSelected();
				if (selected.value && terminal(selected.value.status)) return;
			} catch (cause: any) {
				if (own.signal.aborted) return;
				if (cause?.terminal || ++failures >= 3) {
					message.value = '事件流暂不可用；状态轮询继续，可手动刷新结果。';
					return;
				}
			}
			await new Promise((resolve) => setTimeout(resolve, 1500));
		}
	})();
}
async function start() {
	if (busy.value || pendingKey.value || !confirmed.value || !props.version.run_available) return;
	busy.value = true;
	message.value = '';
	const key = crypto.randomUUID();
	pendingKey.value = key;
	sessionStorage.setItem(keyStorage(), key);
	const input: RunInput = {
		version_id: props.version.version_id,
		confirmed: confirmed.value,
		allowed_origins: origins.value
			.split(/\r?\n/)
			.map((origin) => origin.trim())
			.filter(Boolean),
		budget: { seconds: seconds.value, pages: pages.value, records: records.value, details: details.value },
	};
	try {
		const value = await appApi.createRun(props.version.collector_id, input, key);
		if (disposed) return;
		clear();
		confirmed.value = false;
		history.value = [value, ...history.value];
		await selectRun(value);
	} catch (cause: any) {
		if (cause?.error && !['INTERNAL', 'RUN_TIMEOUT'].includes(cause.error.code)) clear();
		message.value = error(cause);
	} finally {
		busy.value = false;
	}
}
async function recover() {
	if (busy.value || !pendingKey.value) return;
	busy.value = true;
	try {
		const value = await appApi.runByKey(pendingKey.value);
		if (disposed) return;
		clear();
		if (!history.value.some((item) => item.run_id === value.run_id)) history.value.unshift(value);
		await selectRun(value);
	} catch (cause) {
		message.value = error(cause);
	} finally {
		busy.value = false;
	}
}
async function cancel() {
	if (busy.value || !selected.value) return;
	busy.value = true;
	const id = selected.value.run_id,
		token = epoch;
	try {
		const value = await appApi.cancelRun(id);
		if (!disposed && token === epoch) selected.value = value;
	} catch (cause) {
		message.value = error(cause);
	} finally {
		busy.value = false;
	}
	await refreshSelected();
}
watch(
	() => props.version.version_id,
	() => {
		const config = props.version.runtime_config;
		seconds.value = config?.budget.seconds || 60;
		pages.value = config?.budget.pages || 5;
		records.value = config?.budget.records || 20;
		details.value = config?.budget.details ?? 2;
		origins.value = config?.allowed_origins.join('\n') || '';
		confirmed.value = false;
	},
	{ immediate: true }
);
watch([seconds, pages, records, details, origins], () => (confirmed.value = false));
onMounted(async () => {
	pendingKey.value = sessionStorage.getItem(keyStorage()) || '';
	await loadHistory(false);
	if (!disposed && history.value[0]) await selectRun(history.value[0]);
});
onBeforeUnmount(() => {
	disposed = true;
	stop();
});
defineExpose({ hasUnsavedChanges: () => busy.value || !!pendingKey.value });
</script>
