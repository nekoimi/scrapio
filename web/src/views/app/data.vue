<template>
	<div class="app-page">
		<div class="eyebrow">SCRAPIO / DATA</div>
		<h1>{{ table?.name || '数据' }}</h1>
		<p class="lead">正式采集保存的记录，以及每一条记录的真实来源。</p>
		<div class="editor-actions">
			<router-link v-if="tableID" to="/app/data">所有数据表</router-link><button :disabled="loading" @click="reload">刷新</button>
		</div>
		<p v-if="message" class="app-error" role="alert">{{ message }}</p>
		<p v-if="loading" role="status">读取中…</p>
		<template v-if="tableID && table"
			><p>
				{{ stats[tableID]?.record_count || 0 }} 条记录 · 最近有效更新 {{ stats[tableID]?.last_changed_at || '尚无数据' }} · Schema
				{{ table.schema_version }}
			</p>
			<div class="record-preview-table">
				<table>
					<thead>
						<tr>
							<th v-for="f in table.schema.fields" :key="f.field_key">{{ f.name }}</th>
							<th>最近观察</th>
							<th>来源</th>
						</tr>
					</thead>
					<tbody>
						<tr v-for="r in records" :key="r.record_id">
							<td v-for="f in table.schema.fields" :key="f.field_key">
								<pre>{{ value(r, f.field_key) }}</pre>
							</td>
							<td>{{ r.last_observed_at }}</td>
							<td><button @click="openRecord(r.record_id)">详情与追溯</button></td>
						</tr>
					</tbody>
				</table>
			</div>
			<p v-if="!loading && !records.length" class="app-card">这张数据表还没有正式记录。回到采集方案，发布后运行一次。</p></template
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
		<section v-if="!tableID && !loading && !tables.length" class="app-card">
			<h2>还没有数据表</h2>
			<p>配置字段并确认输出后创建数据表，正式运行后即可在这里查看记录。</p>
			<router-link to="/app/collectors">打开采集方案</router-link>
		</section>
		<button v-if="cursor" :disabled="loading" @click="load(true)">加载更多</button>
		<RecordDetail v-if="recordID" :id="recordID" @close="closeRecord" />
	</div>
</template>
<script setup lang="ts">
import { ref, computed, watch, onBeforeUnmount } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { appApi, type LogicalTable, type DataRow } from './api';
import RecordDetail from './record-detail.vue';
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
			const [t, s, r] = await Promise.all([appApi.table(id), appApi.tableStats(id), appApi.records(id, more ? cursor.value : '')]);
			if (token !== epoch) return;
			table.value = t;
			stats.value[id] = s;
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
	table.value = undefined;
	cursor.value = '';
	void load();
}
watch(tableID, reload, { immediate: true });
onBeforeUnmount(() => epoch++);
</script>
