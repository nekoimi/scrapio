<template>
	<div class="app-page">
		<router-link :to="`/app/collectors/${id}`">← 采集方案</router-link>
		<div class="eyebrow">SCRAPIO / SCHEDULE</div>
		<h1>调度与 API</h1>
		<p class="lead">固定发布版本、访问范围与预算。计划和凭据独立配置，发布新版本后不会自动切换。</p>
		<p v-if="message" class="app-error" role="status">{{ message }}</p>
		<section class="app-card">
			<h2>当前计划</h2>
			<p v-if="saved">
				{{ saved.enabled ? '已启用' : '已暂停' }} · revision {{ saved.revision }} · {{ saved.timezone }} · 下次：{{
					saved.next_at ? new Date(saved.next_at).toLocaleString() : '无'
				}}（当前设备时区）
			</p>
			<p v-else>尚未配置计划。</p>
			<p v-if="saved">
				最近决定：{{ decision(saved.last_decision) }}
				<router-link v-if="saved.last_run_id" :to="`/app/runs/${saved.last_run_id}`">查看运行</router-link>
			</p>
			<div class="editor-actions">
				<button :disabled="busy || dirty || !!pendingSave" @click="reload">刷新服务器状态</button>
				<button v-if="saved" :disabled="busy || dirty || !!pendingSave" @click="toggle">{{ saved.enabled ? '暂停后续触发' : '恢复计划' }}</button>
				<button v-if="pendingSave" :disabled="busy" @click="retrySave">查询并重试原保存</button>
				<button v-if="dirty || pendingSave" :disabled="busy" @click="discard">放弃本地编辑并读取服务器</button>
			</div>
			<p class="muted">暂停不取消已排队或执行中的运行；停机期间遗漏时点合并为一次，恢复计划从当前时间计算，不批量补跑。</p>
		</section>
		<details v-if="pendingSave">
			<summary>未确认的原保存请求</summary>
			<pre>{{ JSON.stringify(pendingSave, null, 2) }}</pre>
		</details>
		<section class="app-card">
			<h2>版本与运行范围</h2>
			<p v-if="!versions.length">需要先发布至少一个可执行版本。</p>
			<label for="schedule-version">固定发布版本</label>
			<select id="schedule-version" v-model="versionID" :disabled="busy || !!pendingSave" @change="chooseVersion">
				<option value="">选择版本</option>
				<option v-for="v in versions" :key="v.version_id" :value="v.version_id" :disabled="!v.run_available">
					v{{ v.number }} · {{ v.name }} {{ v.run_available ? '' : '（不可执行）' }}
				</option>
			</select>
			<button v-if="versionCursor" :disabled="busy" @click="moreVersions">更多版本</button>
			<label for="schedule-origins">允许访问的来源（每行一个，含协议与端口）</label>
			<textarea id="schedule-origins" v-model="origins" class="full-input" :disabled="busy || !!pendingSave" />
			<div class="schedule-budget">
				<label>秒数 5～900<input v-model.number="budget.seconds" type="number" min="5" max="900" :disabled="busy || !!pendingSave" /></label>
				<label>文档 1～100<input v-model.number="budget.pages" type="number" min="1" max="100" :disabled="busy || !!pendingSave" /></label>
				<label>记录 1～1000<input v-model.number="budget.records" type="number" min="1" max="1000" :disabled="busy || !!pendingSave" /></label>
				<label>详情 0～100<input v-model.number="budget.details" type="number" min="0" max="100" :disabled="busy || !!pendingSave" /></label>
			</div>
			<label><input v-model="confirmed" type="checkbox" :disabled="busy || !!pendingSave" />确认后台运行将真实访问以上来源，使用这个版本与预算</label>
			<p class="muted">以下保存计划或创建凭据都绑定这里的版本与范围。预算是上限，不承诺完整采集；修改范围后需要重新确认。</p>
		</section>
		<section class="app-card">
			<h2>定时计划</h2>
			<label for="schedule-cron">Cron（分 时 日 月 周）</label
			><input id="schedule-cron" v-model="cron" placeholder="0 9 * * *" :disabled="busy || !!pendingSave" />
			<p class="muted">例：0 9 * * * 每天 09:00；*/30 * * * * 每 30 分钟。使用下方时区，不支持秒、@every 或表达式内时区。</p>
			<label for="schedule-zone">IANA 时区</label
			><input id="schedule-zone" v-model="timezone" placeholder="Asia/Shanghai" :disabled="busy || !!pendingSave" />
			<label for="schedule-overlap">方案已有运行时</label
			><select id="schedule-overlap" v-model="overlap" :disabled="busy || !!pendingSave">
				<option value="skip">跳过本次</option>
				<option value="queue">排队（最多等待一个）</option>
			</select>
			<label><input v-model="enabled" type="checkbox" :disabled="busy || !!pendingSave" />启用后续定时触发</label>
			<button :disabled="busy || !versionID || !confirmed || !!pendingSave" @click="save">保存计划</button>
		</section>
		<section class="app-card">
			<h2>方案 API 凭据</h2>
			<p>凭据只能触发此方案的固定版本；允许按次调低预算。每个方案最多 5 个有效凭据。</p>
			<label for="api-key-name">凭据名称</label
			><input id="api-key-name" v-model="keyName" maxlength="80" placeholder="每日自动采集" :disabled="busy" />
			<button :disabled="busy || !versionID || !confirmed || !keyName.trim() || !!pendingKey" @click="createKey">创建凭据</button>
			<p v-if="pendingKey">创建结果未确认，凭据 ID：{{ pendingKey }}。<button :disabled="busy" @click="recoverKey">查询原凭据</button></p>
			<div v-if="secret" class="conflict-box">
				<strong>请立即保存凭据，仅本次显示</strong>
				<pre class="api-secret">{{ secret }}</pre>
				<button @click="copySecret">复制</button><button @click="secret = ''">已保存，隐藏</button>
				<p>页面关闭后无法再次读取。未收到密钥或丢失时，撤销原凭据后创建新的凭据。</p>
			</div>
			<article v-for="key in keys" :key="key.id" class="trace-item">
				<strong>{{ key.name }}</strong> · {{ key.revoked_at ? '已撤销' : '有效' }} · 版本 {{ key.input.version_id.slice(0, 8) }}
				<p>
					预算 {{ key.input.budget.seconds }} 秒 / {{ key.input.budget.pages }} 文档 / {{ key.input.budget.records }} 记录 /
					{{ key.input.budget.details }} 详情 · {{ key.id }}
				</p>
				<button v-if="!key.revoked_at" :disabled="busy" @click="revoke(key)">撤销凭据</button>
			</article>
			<details>
				<summary>API 使用说明</summary>
				<pre>{{ example }}</pre>
				<p>
					请求体为 {} 或完整的 budget 对象，只能调低上限。重试使用同一个 Idempotency-Key（1～80 字符）及相同请求体，返回同一
					run_id；修改请求体必须换键。每个凭据的键独立，API 遇到同方案已有等待任务时不会继续累积；每个账户最多三个活跃任务；容量冲突返回
					409。触发凭据不能读取其他管理接口。
				</p>
				<p>
					202 表示已入队，实际执行与写入结果请在运行中心查看；能力变化由执行器报告失败。撤销阻止后续请求，不取消已有运行。凭据放在 Authorization
					请求头中，不放 URL。
				</p>
			</details>
		</section>
		<section class="app-card">
			<h2>最近 100 条计划记录</h2>
			<p v-if="!events.length">暂无记录。</p>
			<article v-for="e in events" :key="e.id" class="trace-item">
				{{ e.created_at }} · revision {{ e.revision }} · {{ decision(e.decision) }} · 应触发 {{ e.due_at || '配置操作' }}
				{{ e.coalesced === 'true' ? '（已合并遗漏时点）' : '' }} <router-link v-if="e.run_id" :to="`/app/runs/${e.run_id}`">查看运行</router-link>
			</article>
		</section>
	</div>
</template>
<script setup lang="ts">
import { ref, computed, watch, onMounted, onBeforeUnmount } from 'vue';
import { useRoute, onBeforeRouteLeave, onBeforeRouteUpdate } from 'vue-router';
import { appApi, type CollectorSchedule, type ScheduleConfig, type CollectorAPIKey, type PublishedVersion, type RunInput, type DataRow } from './api';
import { readError } from './data-read';
const route = useRoute(),
	id = computed(() => String(route.params.id));
const saved = ref<CollectorSchedule | null>(null),
	versions = ref<PublishedVersion[]>([]),
	versionCursor = ref(''),
	keys = ref<CollectorAPIKey[]>([]),
	events = ref<DataRow[]>([]),
	busy = ref(false),
	message = ref(''),
	secret = ref(''),
	keyName = ref(''),
	pendingKey = ref(''),
	pendingSave = ref<ScheduleConfig>();
const versionID = ref(''),
	origins = ref(''),
	budget = ref({ seconds: 60, pages: 5, records: 20, details: 2 }),
	confirmed = ref(false),
	cron = ref('0 9 * * *'),
	timezone = ref('Asia/Shanghai'),
	overlap = ref<'skip' | 'queue'>('skip'),
	enabled = ref(false);
let baseline = '',
	disposed = false,
	epoch = 0,
	applying = false;
const storage = (kind: string) => `scrapio:v22:schedule:${id.value}:${kind}`;
function input(): RunInput {
	return {
		version_id: versionID.value,
		confirmed: confirmed.value,
		allowed_origins: origins.value
			.split('\n')
			.map((v) => v.trim())
			.filter(Boolean),
		budget: { ...budget.value },
	};
}
function form(): ScheduleConfig {
	return {
		expected_revision: saved.value?.revision || 0,
		enabled: enabled.value,
		cron: cron.value,
		timezone: timezone.value,
		overlap: overlap.value,
		input: input(),
	};
}
const snapshot = () => JSON.stringify(form());
const dirty = computed(() => !!baseline && snapshot() !== baseline);
const example = computed(
	() =>
		`curl -X POST 'https://YOUR-SCRAPIO/api/v3/collectors/${id.value}/api-runs' \\\n  -H 'Authorization: Bearer YOUR_TOKEN' \\\n  -H 'Idempotency-Key: request-001' \\\n  -H 'Content-Type: application/json' \\\n  -d '{}'`
);
function decision(value: string) {
	return (
		(
			{
				saved_paused: '已保存（暂停）',
				saved_enabled: '已保存（启用）',
				created: '已创建运行',
				accepted_queue: '按排队策略接受',
				skipped_overlap: '已有运行，本次跳过',
				skipped_capacity: '容量已满，本次跳过',
				disabled_unavailable: '版本或方案不可用，计划已停用',
				disabled_invalid: '版本或目标不兼容，计划已停用',
			} as Record<string, string>
		)[value] || value
	);
}
function apply(c: CollectorSchedule | null) {
	applying = true;
	saved.value = c;
	enabled.value = c?.enabled || false;
	cron.value = c?.cron || '0 9 * * *';
	timezone.value = c?.timezone || 'Asia/Shanghai';
	overlap.value = c?.overlap || 'skip';
	if (c) {
		versionID.value = c.input.version_id;
		origins.value = c.input.allowed_origins.join('\n');
		budget.value = { ...c.input.budget };
		confirmed.value = c.input.confirmed;
	}
	baseline = snapshot();
	applying = false;
}
watch(
	[versionID, origins, () => JSON.stringify(budget.value)],
	() => {
		if (!applying) confirmed.value = false;
	},
	{ flush: 'sync' }
);
async function chooseVersion() {
	if (busy.value || !versionID.value) return;
	busy.value = true;
	const token = epoch;
	const wanted = versionID.value;
	try {
		const v = await appApi.version(wanted);
		if (disposed || epoch !== token || versionID.value !== wanted) return;
		applying = true;
		origins.value = v.runtime_config?.allowed_origins.join('\n') || '';
		budget.value = { seconds: 60, pages: 5, records: 20, details: 2, ...v.runtime_config?.budget };
		confirmed.value = false;
		applying = false;
	} catch (cause) {
		message.value = readError(cause);
	} finally {
		if (!disposed && token === epoch) busy.value = false;
	}
}
async function reload() {
	if (busy.value) return;
	busy.value = true;
	const token = epoch;
	try {
		const [c, v, k, e] = await Promise.all([
			appApi.schedule(id.value),
			appApi.versions(id.value),
			appApi.apiKeys(id.value),
			appApi.scheduleEvents(id.value),
		]);
		if (disposed || token !== epoch) return;
		versions.value = v.items;
		versionCursor.value = v.next_cursor;
		keys.value = k.items;
		events.value = e.items;
		apply(c);
		if (c && !versions.value.some((v) => v.version_id === c.input.version_id)) {
			const old = await appApi.version(c.input.version_id);
			if (!disposed && token === epoch) versions.value.unshift(old);
		}
	} catch (cause) {
		message.value = readError(cause);
	} finally {
		if (!disposed && token === epoch) busy.value = false;
	}
}
async function moreVersions() {
	if (busy.value) return;
	busy.value = true;
	try {
		const v = await appApi.versions(id.value, versionCursor.value);
		if (!disposed) {
			versions.value = [...versions.value, ...v.items.filter((v) => !versions.value.some((old) => old.version_id === v.version_id))];
			versionCursor.value = v.next_cursor;
		}
	} catch (cause) {
		message.value = readError(cause);
	} finally {
		busy.value = false;
	}
}
async function performSave(c: ScheduleConfig) {
	busy.value = true;
	pendingSave.value = c;
	sessionStorage.setItem(storage('save'), JSON.stringify(c));
	try {
		const result = await appApi.saveSchedule(id.value, c);
		if (disposed) return;
		pendingSave.value = undefined;
		sessionStorage.removeItem(storage('save'));
		apply(result);
		message.value = '计划已保存。';
		events.value = (await appApi.scheduleEvents(id.value)).items;
	} catch (cause) {
		message.value = readError(cause) + '；本地配置保留，请查询并重试原保存。';
	} finally {
		busy.value = false;
	}
}
async function save() {
	if (busy.value || pendingSave.value || !confirmed.value) return;
	await performSave(form());
}
async function toggle() {
	if (busy.value || !saved.value || dirty.value || pendingSave.value) return;
	const c = saved.value;
	await performSave({ expected_revision: c.revision, enabled: !c.enabled, cron: c.cron, timezone: c.timezone, overlap: c.overlap, input: c.input });
}
async function retrySave() {
	if (busy.value || !pendingSave.value) return;
	await performSave(pendingSave.value);
}
async function discard() {
	if (busy.value || !window.confirm('服务器可能已完成保存。放弃本地编辑并读取当前计划？')) return;
	pendingSave.value = undefined;
	sessionStorage.removeItem(storage('save'));
	await reload();
}
async function createKey() {
	if (busy.value || pendingKey.value || !confirmed.value) return;
	const keyID = crypto.randomUUID();
	pendingKey.value = keyID;
	sessionStorage.setItem(storage('key'), keyID);
	busy.value = true;
	try {
		const r = await appApi.createAPIKey(id.value, { id: keyID, name: keyName.value, input: input() });
		if (disposed) return;
		secret.value = r.token;
		keys.value.unshift(r.credential);
		pendingKey.value = '';
		sessionStorage.removeItem(storage('key'));
		message.value = '凭据已创建，请保存仅显示一次的密钥。';
	} catch (cause: any) {
		message.value = readError(cause);
		if (cause?.error && !['INTERNAL', 'RUN_TIMEOUT'].includes(cause.error.code)) {
			pendingKey.value = '';
			sessionStorage.removeItem(storage('key'));
		}
	} finally {
		busy.value = false;
	}
}
async function recoverKey() {
	if (busy.value || !pendingKey.value) return;
	busy.value = true;
	try {
		const r = await appApi.apiKeys(id.value);
		if (disposed) return;
		keys.value = r.items;
		if (r.items.some((k) => k.id === pendingKey.value)) {
			message.value = '原凭据已创建。密钥无法再次读取，请撤销原凭据后重新创建。';
			pendingKey.value = '';
			sessionStorage.removeItem(storage('key'));
		} else {
			message.value = '原凭据尚未出现，请稍后再查；只有确认后才结束本地等待。';
			if (window.confirm('原请求可能仍在处理。结束本地等待后请继续检查凭据列表，撤销不再使用的凭据。确认结束？')) {
				pendingKey.value = '';
				sessionStorage.removeItem(storage('key'));
			}
		}
	} catch (cause) {
		message.value = readError(cause);
	} finally {
		busy.value = false;
	}
}
async function revoke(key: CollectorAPIKey) {
	if (busy.value || !window.confirm(`撤销 ${key.name}？不会取消已有运行。`)) return;
	busy.value = true;
	try {
		await appApi.revokeAPIKey(id.value, key.id);
		if (!disposed) {
			keys.value = (await appApi.apiKeys(id.value)).items;
			secret.value = '';
			message.value = '凭据已撤销。';
		}
	} catch (cause) {
		message.value = readError(cause);
	} finally {
		busy.value = false;
	}
}
async function copySecret() {
	try {
		await navigator.clipboard.writeText(secret.value);
		message.value = '凭据已复制。';
	} catch {
		message.value = '复制失败，请手动复制并保存。';
	}
}
function leave() {
	if (busy.value) return false;
	return (
		!(dirty.value || pendingSave.value || pendingKey.value || secret.value) ||
		window.confirm('存在未保存配置、未确认请求或仅显示一次的密钥，确认离开？')
	);
}
function unload(e: BeforeUnloadEvent) {
	if (busy.value || dirty.value || pendingSave.value || pendingKey.value || secret.value) {
		e.preventDefault();
		e.returnValue = '';
	}
}
async function initialize() {
	epoch++;
	applying = true;
	versionID.value = '';
	origins.value = '';
	budget.value = { seconds: 60, pages: 5, records: 20, details: 2 };
	confirmed.value = false;
	applying = false;
	secret.value = '';
	baseline = '';
	saved.value = null;
	message.value = '';
	pendingSave.value = undefined;
	pendingKey.value = sessionStorage.getItem(storage('key')) || '';
	await reload();
	const raw = sessionStorage.getItem(storage('save'));
	if (raw) {
		try {
			pendingSave.value = JSON.parse(raw);
			message.value = '恢复了未确认的保存请求，请查询并重试原保存。';
		} catch {
			message.value = '保存恢复记录损坏，请读取服务器状态。';
		}
	}
}
onBeforeRouteLeave(leave);
onBeforeRouteUpdate(leave);
watch(id, initialize);
onMounted(() => {
	window.addEventListener('beforeunload', unload);
	void initialize();
});
onBeforeUnmount(() => {
	disposed = true;
	epoch++;
	secret.value = '';
	window.removeEventListener('beforeunload', unload);
});
</script>
<style scoped>
.schedule-budget {
	display: grid;
	grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
	gap: 16px;
}
.schedule-budget label {
	display: flex;
	flex-direction: column;
	gap: 8px;
}
.api-secret {
	overflow-wrap: anywhere;
	white-space: pre-wrap;
}
label {
	display: block;
	margin: 12px 0 6px;
}
pre {
	white-space: pre-wrap;
	overflow-wrap: anywhere;
}
</style>
