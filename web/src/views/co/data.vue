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
		><el-button v-if="tableCursor" link @click="moreTables">加载更多表</el-button>
		<div class="co-toolbar">
			<el-button :disabled="!selectedRows.length" @click="sendToCollector">选中记录交给采集器处理</el-button
			><el-button :disabled="!tableID || exporting" @click="exportRecords"
				>导出{{ selectedRows.length ? '选中记录' : '当前表（最多1000条）' }}</el-button
			><el-button :disabled="!table" @click="openSchema">查看 / 升级Schema</el-button>
		</div>
		<el-table :data="rows" row-key="id" @selection-change="selectedRows = $event" empty-text="此表尚无正式记录"
			><el-table-column type="selection" width="50" /> ><el-table-column
				v-for="f in table?.schema.fields || []"
				:key="f.key"
				:label="f.label || f.key"
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
		<el-dialog v-model="sendDialog" title="交给独立采集器处理" width="min(620px, 94vw)"
			><p>选中 {{ selectedRows.length }} 条。将创建正常单阶段任务草稿；确认映射、输出表并发布后才能执行。</p>
			<el-select v-model="collectorID" placeholder="选择已有采集器"
				><el-option v-for="c in collectors" :key="c.id" :label="c.name" :value="c.id" /></el-select
			><el-button v-if="collectorCursor" link @click="loadCollectors">加载更多</el-button
			><template #footer
				><el-button @click="sendDialog = false">取消</el-button
				><el-button type="primary" :disabled="!collectorID" @click="createFromRows">配置单阶段任务</el-button></template
			></el-dialog
		>
		<el-dialog v-model="schemaDialog" title="Schema升级" width="min(720px, 94vw)"
			><p>首版支持新增可缺省字段；已有字段及唯一键保持。活动运行结束后才能升级，随后需重新预演任务绑定。历史记录保留原Schema版本。</p>
			<el-input v-model="schemaText" type="textarea" :rows="12" /><template #footer
				><el-button @click="schemaDialog = false">关闭</el-button
				><el-button :disabled="exporting" @click="upgradeSchema">确认升级</el-button></template
			></el-dialog
		>
	</section>
</template>
<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { ElMessageBox } from 'element-plus';
import { taskApi, taskMutation, type DataTable } from './tasks-api';
import { call, coApi, describe, type Collector } from './api';
const route = useRoute(),
	tables = ref<DataTable[]>([]),
	tableID = ref(String(route.query.table_id || '')),
	tableCursor = ref(''),
	rows = ref<any[]>([]),
	cursor = ref(''),
	problem = ref(''),
	drawer = ref(false),
	detail = ref<any>();
const router = useRouter(),
	selectedRows = ref<any[]>([]),
	sendDialog = ref(false),
	collectors = ref<Collector[]>([]),
	collectorID = ref(''),
	collectorCursor = ref(''),
	exporting = ref(false),
	schemaDialog = ref(false),
	schemaText = ref('');
async function loadCollectors() {
	try {
		const p = await coApi.list(collectorCursor.value);
		collectors.value.push(...p.items.filter((c) => !c.archived));
		collectorCursor.value = p.next_cursor || '';
	} catch (e) {
		problem.value = describe(e);
	}
}
async function sendToCollector() {
	sendDialog.value = true;
	collectors.value = [];
	collectorCursor.value = '';
	collectorID.value = '';
	await loadCollectors();
}
async function createFromRows() {
	sessionStorage.setItem('co-source-handoff', JSON.stringify({ table_id: tableID.value, record_ids: selectedRows.value.map((r) => r.id) }));
	await router.push({ path: '/app/co/tasks/new', query: { collector_id: collectorID.value, source_table: tableID.value } });
}
async function exportRecords() {
	exporting.value = true;
	try {
		const value = await call<any>('/tables/' + tableID.value + '/export', 'post', { record_ids: selectedRows.value.map((r) => r.id), limit: 1000 });
		const blob = new Blob([JSON.stringify(value, null, 2)], { type: 'application/json' });
		const url = URL.createObjectURL(blob),
			a = document.createElement('a');
		a.href = url;
		a.download = 'scrapio-' + tableID.value + '.json';
		a.click();
		URL.revokeObjectURL(url);
	} catch (e) {
		problem.value = describe(e);
	} finally {
		exporting.value = false;
	}
}
function openSchema() {
	schemaText.value = JSON.stringify(table.value?.schema, null, 2);
	schemaDialog.value = true;
}
async function upgradeSchema() {
	try {
		await ElMessageBox.confirm('升级后旧绑定需要重新预演；不会改写历史记录。确认继续？', '确认升级', { customClass: 'co-confirm', type: 'warning' });
	} catch {
		return;
	}
	exporting.value = true;
	try {
		await taskMutation('upgradeSchema', tableID.value, '/tables/' + tableID.value + '/schema', {
			expected_version: table.value?.schema_version,
			schema: JSON.parse(schemaText.value),
			confirmed: true,
		});
		schemaDialog.value = false;
		await load();
	} catch (e) {
		problem.value = describe(e);
	} finally {
		exporting.value = false;
	}
}
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
	if (!more) selectedRows.value = [];
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
