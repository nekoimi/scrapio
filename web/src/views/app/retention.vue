<template>
	<div class="app-page governance-page">
		<div class="eyebrow">SCRAPIO / SETTINGS / RETENTION</div>
		<h1>容量与保留</h1>
		<p class="lead">为编辑时使用的页面输入和样例截图设置容量；先预览，再确认清理可释放的资产。</p>
		<p v-if="error" class="app-error" role="alert">{{ error }}</p>
		<p v-if="notice" role="status">{{ notice }}</p>
		<p v-if="busy" role="status">处理中…</p>
		<div class="editor-actions"><button :disabled="busy" @click="reload">刷新使用量</button></div>
		<template v-if="state">
			<div class="app-grid">
				<article class="app-card">
					<h2>编辑资产池</h2>
					<p>
						{{ mib(state.asset_bytes) }} MiB 已用 + {{ mib(state.reserved_bytes) }} MiB 活动输入预留 /
						{{ state.policy.asset_limit_mib }} MiB
					</p>
					<p>
						输入正文 {{ state.usage.captures }} 份 · 样例 {{ state.usage.samples }} 份 · 手动保护 {{ state.usage.protected_samples }} 份
					</p>
					<p class="muted">所有样例输入与截图、输出检查和修复引用都会保留；保护资产也计入容量，达到上限时拒绝新增资产。</p>
				</article>
				<article class="app-card">
					<h2>查询与导出</h2>
					<p>查询快照 {{ mib(Number(state.usage.snapshot_bytes)) }} MiB · 导出文件 {{ mib(Number(state.usage.export_bytes)) }} MiB</p>
					<p>查询快照默认 30 分钟；导出文件 24 小时，元数据 7 天。单次查询最多 10000 条 / 16 MiB。</p>
				</article>
				<article class="app-card">
					<h2>长期保留</h2>
					<p>
						{{ state.usage.tables }} 个数据表 · {{ state.usage.versions }} 个版本 · {{ state.usage.runs }} 次正式运行 ·
						{{ state.usage.credentials }} 份凭据记录
					</p>
					<p class="muted">正式记录、页面证据、版本/试采/回归冻结证据及操作记录单独保留，不纳入编辑资产池，也不由此处清理。</p>
				</article>
			</div>
			<section class="app-card">
				<h2>保留规则</h2>
				<p>未被引用的输入超过保留天数后进入可清理范围；修改规则不会立即删除数据。</p>
				<label
					>输入保留天数 <input v-model.number="policy.capture_days" type="number" min="1" max="365" :disabled="busy || uncertain"
				/></label>
				<label
					>编辑资产容量（MiB）
					<input v-model.number="policy.asset_limit_mib" type="number" min="16" max="4096" :disabled="busy || uncertain"
				/></label>
				<div class="editor-actions">
					<button :disabled="busy || uncertain || !dirty" @click="save">保存规则</button
					><button :disabled="busy || uncertain || dirty" @click="preview">预览清理</button>
				</div>
				<p v-if="dirty" class="muted">有未保存规则，请先保存或刷新恢复已保存规则，再预览清理。</p>
			</section>
			<section v-if="check" class="app-card">
				<h2>清理预览 · {{ check.applied ? '已提交' : '待确认' }}</h2>
				<p>
					{{ check.items.length }} 个资产 · 预计释放 {{ mib(check.bytes) }} MiB · 使用规则修订 {{ check.policy_revision }} · 有效至
					{{ new Date(check.expires_at).toLocaleString() }}
				</p>
				<p>
					仅释放无引用输入正文、已过期的无引用查询快照和导出文件。输入请求键及原始哈希保留，旧请求不会自动重新发起网络访问。所有样例、正式记录和证据保持保留。
				</p>
				<details v-if="check.items.length">
					<summary>查看本批资产</summary>
					<ul>
						<li v-for="i in check.items" :key="i.kind + i.id">{{ assetKind(i.kind) }} · {{ i.id }} · {{ i.bytes }} 字节</li>
					</ul>
				</details>
				<p v-if="check.has_more" class="muted">本批最多 200 项，确认后再次预览可处理下一批。</p>
				<label v-if="!check.applied && check.items.length"
					><input v-model="confirmed" type="checkbox" :disabled="busy || dirty" />确认清理本批列出的资产。</label
				>
				<div class="editor-actions">
					<button v-if="!check.applied && check.items.length" :disabled="busy || dirty || !confirmed" @click="apply">
						{{ uncertain ? '查询并重试同一次清理' : '确认清理' }}
					</button>
				</div>
				<p v-if="!check.items.length" class="muted">
					当前没有可清理资产。可缩短保留天数，或先在样例/试采页面管理不再需要且未受保护的证据。
				</p>
			</section>
			<details class="app-card">
				<summary>当前固定上限</summary>
				<ul>
					<li v-for="(v, k) in state.limits" :key="k">{{ limitName(String(k)) }}：{{ v }}</li>
				</ul>
				<p>这些上限由各任务入口执行；本页不提供绕过任务预算的配置。</p>
			</details>
		</template>
	</div>
</template>
<script setup lang="ts">
import { computed, ref, watch, onMounted, onBeforeUnmount } from 'vue';
import { onBeforeRouteLeave, onBeforeRouteUpdate } from 'vue-router';
import { appApi, type RetentionState, type RetentionPolicy, type CleanupPreview } from './api';
import { readError } from './data-read';
const state = ref<RetentionState>(),
	policy = ref<RetentionPolicy>({ revision: 0, capture_days: 7, asset_limit_mib: 512 }),
	check = ref<CleanupPreview>();
const busy = ref(false),
	error = ref(''),
	notice = ref(''),
	confirmed = ref(false),
	uncertain = ref(false);
let epoch = 0;
const dirty = computed(() => !!state.value && JSON.stringify(policy.value) !== JSON.stringify(state.value.policy));
const mib = (v: number) => (v / 1024 / 1024).toFixed(2);
const assetKind = (v: string) => ({ capture: '输入正文', snapshot: '查询快照', export: '导出文件' })[v] || v;
function limitName(v: string) {
	return (
		{
			tables: '数据表',
			samples_per_collector: '每方案样例',
			credentials: '凭据（含撤销记录）',
			active_runs: '活动正式运行',
			run_history: '正式运行历史',
			active_regressions: '活动回归',
			regression_history: '回归历史',
			active_exports: '活动导出',
			valid_exports: '有效导出',
			export_history: '导出历史',
			snapshot_rows: '单次查询记录',
			snapshot_mib: '单次查询 MiB',
		}[v] || v
	);
}
watch(
	policy,
	() => {
		if (!uncertain.value) {
			check.value = undefined;
			confirmed.value = false;
			notice.value = '';
		}
	},
	{ deep: true, flush: 'sync' },
);
async function loadState() {
	const token = epoch,
		r = await appApi.retention();
	if (token === epoch) {
		state.value = r;
		policy.value = { ...r.policy };
	}
}
async function reload() {
	if ((dirty.value || uncertain.value) && !window.confirm('刷新会重置本地规则和清理预览，确认继续？')) return;
	busy.value = true;
	error.value = '';
	uncertain.value = false;
	check.value = undefined;
	const token = epoch;
	try {
		await loadState();
	} catch (e) {
		if (token === epoch) error.value = readError(e);
	} finally {
		if (token === epoch) busy.value = false;
	}
}
async function save() {
	busy.value = true;
	error.value = '';
	const token = epoch;
	try {
		await appApi.saveRetention({ ...policy.value });
		if (token === epoch) {
			await loadState();
			notice.value = '保留规则已保存；未执行清理。';
		}
	} catch (e) {
		if (token === epoch) error.value = readError(e);
	} finally {
		if (token === epoch) busy.value = false;
	}
}
async function preview() {
	busy.value = true;
	error.value = '';
	notice.value = '';
	check.value = undefined;
	confirmed.value = false;
	const token = epoch;
	try {
		const r = await appApi.cleanupCheck();
		if (token === epoch) check.value = r;
	} catch (e) {
		if (token === epoch) error.value = readError(e);
	} finally {
		if (token === epoch) busy.value = false;
	}
}
async function apply() {
	if (!check.value) return;
	busy.value = true;
	error.value = '';
	const token = epoch;
	try {
		const result = await appApi.confirmCleanup(check.value.check_id);
		if (token !== epoch) return;
		uncertain.value = false;
		await loadState();
		check.value = result;
		confirmed.value = false;
		notice.value = '本批清理已提交，可重新预览下一批。';
	} catch (e) {
		if (token === epoch) {
			error.value = readError(e);
			const known = (e as any)?.error?.code;
			uncertain.value = !known || known === 'INTERNAL';
			if (!uncertain.value) {
				confirmed.value = false;
				check.value = undefined;
			}
		}
	} finally {
		if (token === epoch) busy.value = false;
	}
}
function leave() {
	return (!dirty.value && !busy.value && !uncertain.value) || window.confirm('规则修改或清理结果尚未处理完，确认离开？');
}
onBeforeRouteLeave(leave);
onBeforeRouteUpdate(leave);
onMounted(() => void reload());
onBeforeUnmount(() => epoch++);
</script>
