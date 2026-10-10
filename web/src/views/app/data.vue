<template>
	<div class="app-page">
		<div class="eyebrow">SCRAPIO / DATA</div>
		<h1>{{ table?.name || '数据' }}</h1>
		<p class="lead">正式采集保存的记录，以及每一条记录的真实来源。</p>
		<div class="editor-actions">
			<router-link v-if="tableID" to="/app/data">所有数据表</router-link><router-link v-if="tableID" :to="`/app/data/${tableID}/fields`">字段设置与 Schema 历史</router-link><button :disabled="loading" @click="reload">刷新</button>
		</div>
		<p v-if="message" class="app-error" role="alert">{{ message }}</p>
		<p v-if="loading" role="status">读取中…</p>
		<template v-if="tableID && table"
			><p>
				{{ stats[tableID]?.record_count ?? '—' }} 条记录 · 最近有效更新
				{{ stats[tableID]?.last_changed_at || (stats[tableID] ? '尚无数据' : '统计不可用') }} · Schema
				{{ table.schema_version }}
			</p>
			<DataQuery ref="queryPanel" :table-id="tableID" :schema="table.schema" :query="applied" :blocked="loading" @apply="applyQuery" />
			<p v-if="snapshot" class="muted">
				当前筛选 {{ snapshot.total }} 条 · 冻结于
				{{ new Date(snapshot.captured_at).toLocaleString() }}。刷新或重新应用会取得新快照；记录详情展示当前修订，可能已与快照不同。
			</p>
			<DataExport ref="exportPanel" :table-id="tableID" :snapshot="snapshot" :blocked="loading || !!queryPanel?.hasUnappliedChanges()" />
			<div class="record-preview-table">
				<table>
					<thead>
						<tr>
							<th v-for="f in visibleFields" :key="f.field_key">{{ f.name }}</th>
							<th>修订</th>
							<th>最近观察</th>
							<th>来源</th>
						</tr>
					</thead>
					<tbody>
						<tr v-for="r in records" :key="r.record_id">
							<td v-for="f in visibleFields" :key="f.field_key">
								<pre>{{ value(r, f.field_key) }}</pre>
							</td>
							<td>v{{ r.revision }}</td>
							<td>{{ r.last_observed_at }}</td>
							<td><button @click="openRecord(r.record_id)">详情与追溯</button></td>
						</tr>
					</tbody>
				</table>
			</div>
			<p v-if="!loading && snapshot && !records.length" class="app-card">当前筛选没有记录。可重置条件；首次采集请回到方案发布并运行。</p></template
		>
		<div v-else class="app-grid">
			<article v-for="t in tables" :key="t.table_id" class="app-card">
				<h2>
					<router-link :to="`/app/data/${t.table_id}`">{{ t.name }}</router-link>
				</h2>
				<p>{{ stats[t.table_id]?.record_count ?? '—' }} 条记录 · Schema {{ t.schema_version }}</p>
				<p class="muted">最近有效更新 {{ stats[t.table_id]?.last_changed_at || '尚无数据' }}</p>
				<div class="editor-actions">
					<router-link v-for="c in collectors(t.table_id)" :key="c.collector_id" :to="`/app/collectors/${c.collector_id}`">{{ c.name }}</router-link>
				</div>
			</article>
		</div>
		<section v-if="!tableID && !loading && !message && !tables.length" class="app-card">
			<h2>还没有数据表</h2>
			<p>配置字段并确认输出后创建数据表，正式运行后即可在这里查看记录。</p>
			<router-link to="/app/collectors">打开采集方案</router-link>
		</section>
		<button v-if="cursor" :disabled="loading" @click="load(true)">加载更多</button>
		<RecordDetail v-if="recordID" :id="recordID" @close="closeRecord" />
	</div>
</template>
<script setup lang="ts">
import { ref, computed, watch, onBeforeUnmount, onMounted } from 'vue';
import { useRoute, useRouter, onBeforeRouteLeave, onBeforeRouteUpdate } from 'vue-router';
import { appApi, type LogicalTable, type DataRow, type RecordQuery, type QueryPage } from './api';
import RecordDetail from './record-detail.vue';
import DataQuery from './data-query.vue';
import DataExport from './data-export.vue';
import { readError, displayJSON, parseJSON } from './data-read';
const route = useRoute(),
	router = useRouter(),
	tableID = computed(() => String(route.params.id || '')),
	recordID = computed(() => String(route.params.recordID || route.query.record || ''));
const tables = ref<LogicalTable[]>([]),
	table = ref<LogicalTable>(),
	stats = ref<Record<string, DataRow>>({}),
	records = ref<DataRow[]>([]),
	cursor = ref(''),
	loading = ref(false),
	message = ref('');
let epoch = 0;
const queryPanel = ref<InstanceType<typeof DataQuery>>(),
	exportPanel = ref<InstanceType<typeof DataExport>>(),
	snapshot = ref<QueryPage>();
const applied = ref<RecordQuery>({ filters: [], sort: { field: '@first_observed_at', direction: 'desc' }, columns: [] });
const visibleFields = computed(
	() => snapshot.value?.schema.fields.filter((f) => applied.value.columns.includes(f.field_key)) || table.value?.schema.fields || []
);
function routeQuery(): RecordQuery {
	if (!route.query.q) return { filters: [], sort: { field: '@first_observed_at', direction: 'desc' }, columns: [] };
	const q = JSON.parse(String(route.query.q));
	if (!Array.isArray(q.filters) || !q.sort || !Array.isArray(q.columns)) throw Error('地址中的查询格式无效，请清除查询条件');
	return q;
}
function applyQuery(q: RecordQuery) {
	const serialized = JSON.stringify(q);
	if (serialized === String(route.query.q || '')) {
		reload();
		return;
	}
	void router.replace({ query: { ...route.query, q: serialized, record: undefined } });
}
function leave() {
	return (
		(!queryPanel.value?.hasUnsavedChanges() && !exportPanel.value?.hasUnsavedChanges()) ||
		window.confirm('有未应用条件或未确认请求，确认离开？本地未确认请求可在原数据表恢复。')
	);
}
onBeforeRouteLeave(leave);
onBeforeRouteUpdate((to, from) => (to.params.id !== from.params.id ? leave() : true));
const collectors = (id: string) => parseJSON<DataRow[]>(stats.value[id]?.collectors_json || '[]', []);
function value(row: DataRow, key: string) {
	const fields = parseJSON<{ field_key: string; value_json: string }[]>(row.fields_json || '[]', []);
	return displayJSON(fields.find((f) => f.field_key === key)?.value_json || 'null');
}
function openRecord(id: string) {
	void router.replace({ query: { ...route.query, record: id } });
}
function closeRecord() {
	if (route.params.recordID) {
		void router.push('/app/data');
		return;
	}
	const q = { ...route.query };
	delete q.record;
	void router.replace({ query: q });
}
async function load(more = false) {
	if (loading.value) return;
	const token = epoch,
		id = tableID.value;
	loading.value = true;
	message.value = '';
	try {
		if (id) {
			const [tableResult, statsResult] = await Promise.allSettled([appApi.table(id), appApi.tableStats(id)]);
			if (token !== epoch) return;
			if (tableResult.status === 'rejected') throw tableResult.reason;
			const t = tableResult.value;
			table.value = t;
			if (statsResult.status === 'fulfilled') stats.value[id] = statsResult.value;
			else {
				delete stats.value[id];
				message.value = '统计暂不可用：' + readError(statsResult.reason);
			}
			const q = more ? applied.value : routeQuery();
			if (!more) {
				applied.value = { ...q, columns: q.columns.length ? q.columns : t.schema.fields.map((f) => f.field_key) };
				snapshot.value = undefined;
				records.value = [];
				cursor.value = '';
			}
			const r = await appApi.queryRecords(id, q, more ? cursor.value : '');
			if (token !== epoch) return;
			snapshot.value = r;
			applied.value = r.query;
			records.value = more ? [...records.value, ...r.items] : r.items;
			cursor.value = r.next_cursor;
		} else {
			const r = await appApi.tables(more ? cursor.value : '');
			if (token !== epoch) return;
			tables.value = more ? [...tables.value, ...r.items] : r.items;
			cursor.value = r.next_cursor;
			for (const t of r.items) {
				if (t.statistics) stats.value[t.table_id] = t.statistics;
			}
		}
	} catch (e) {
		if (token === epoch) message.value = readError(e);
	} finally {
		if (token === epoch) loading.value = false;
	}
}
function reload() {
	epoch++;
	loading.value = false;
	tables.value = [];
	records.value = [];
	if (table.value?.table_id !== tableID.value) table.value = undefined;
	snapshot.value = undefined;
	cursor.value = '';
	void load();
}
watch([tableID, () => route.query.q], reload, { immediate: true });
function beforeUnload(event: BeforeUnloadEvent) {
	if (queryPanel.value?.hasUnsavedChanges() || exportPanel.value?.hasUnsavedChanges()) {
		event.preventDefault();
		event.returnValue = '';
	}
}
onMounted(() => window.addEventListener('beforeunload', beforeUnload));
onBeforeUnmount(() => {
	epoch++;
	window.removeEventListener('beforeunload', beforeUnload);
});
</script>
