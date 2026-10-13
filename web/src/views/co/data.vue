<template>
	<section class="co-page">
		<header class="co-heading">
			<div>
				<h1>任务数据表</h1>
				<p>仅展示独立任务正式写入的数据；试采输出在运行详情中查看。</p>
			</div>
			<el-button @click="load">刷新数据表</el-button>
		</header>
		<el-alert v-if="problem" :title="problem" type="error" :closable="false" /><el-select
			v-model="tableID"
			placeholder="选择数据表"
			@change="records(false)"
			><el-option v-for="t in tables" :key="t.id" :label="t.name + ' · Schema ' + t.schema_version" :value="t.id" /></el-select
		><el-button v-if="tableCursor" link @click="moreTables">加载更多表</el-button
		><el-table :data="rows" empty-text="此表尚无正式记录"
			><el-table-column v-for="f in table?.schema.fields || []" :key="f.key" :label="f.label || f.key"
				><template #default="{ row }">{{
					typeof row.values[f.key] === 'object' ? JSON.stringify(row.values[f.key]) : row.values[f.key]
				}}</template></el-table-column
			><el-table-column prop="revision" label="修订" width="80" /><el-table-column label="来源"
				><template #default="{ row }"><el-button link type="primary" @click="history(row.id)">来源与历史</el-button></template></el-table-column
			></el-table
		><el-button v-if="cursor" @click="records(true)">加载更多</el-button
		><el-drawer v-model="drawer" title="记录来源与修订" size="min(760px, 94vw)"
			><template v-if="detail"
				><h3>当前记录</h3>
				<pre>{{ JSON.stringify(detail.record, null, 2) }}</pre>
				<h3>最近100次观察</h3>
				<el-card v-for="o in detail.observations" :key="o.id"
					><router-link :to="'/app/co/runs/' + o.run_id + '?item_id=' + o.item_id">运行 {{ o.run_id.slice(0, 8) }}</router-link>
					<p>{{ o.decision }} · {{ o.source_key }}</p>
					<pre>{{ JSON.stringify(o.values, null, 2) }}</pre>
				</el-card>
				<h3>最近100个修订</h3>
				<pre>{{ JSON.stringify(detail.revisions, null, 2) }}</pre>
			</template></el-drawer
		>
	</section>
</template>
<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import { useRoute } from 'vue-router';
import { taskApi, type DataTable } from './tasks-api';
import { describe } from './api';
const route = useRoute(),
	tables = ref<DataTable[]>([]),
	tableID = ref(''),
	tableCursor = ref(''),
	rows = ref<any[]>([]),
	cursor = ref(''),
	problem = ref(''),
	drawer = ref(false),
	detail = ref<any>();
const table = computed(() => tables.value.find((t) => t.id === tableID.value));
async function moreTables() {
	try {
		const p = await taskApi.tables(tableCursor.value);
		tables.value.push(...p.items);
		tableCursor.value = p.next_cursor || '';
	} catch (e) {
		problem.value = describe(e);
	}
}
async function load() {
	tables.value = [];
	tableCursor.value = '';
	await moreTables();
	if (!tableID.value) tableID.value = tables.value[0]?.id || '';
	await records(false);
}
async function records(more = false) {
	if (!tableID.value) return;
	try {
		const p = await taskApi.records(tableID.value, more ? cursor.value : '');
		rows.value = more ? [...rows.value, ...p.items] : p.items;
		cursor.value = p.next_cursor || '';
		problem.value = '';
	} catch (e) {
		problem.value = describe(e);
	}
}
async function history(id: string) {
	try {
		detail.value = await taskApi.history(id);
		drawer.value = true;
	} catch (e) {
		problem.value = describe(e);
	}
}
onMounted(async () => {
	await load();
	if (route.query.record_id) await history(String(route.query.record_id));
});
</script>
<style src="./style.css"></style>
