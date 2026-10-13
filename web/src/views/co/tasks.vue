<template>
	<section class="co-page">
		<header class="co-heading">
			<div>
				<h1>编排任务</h1>
				<p>固定采集器版本，批量执行独立输入并保存到数据表。</p>
			</div>
			<el-button type="primary" @click="router.push('/app/co/tasks/new')">创建任务</el-button>
		</header>
		<el-alert v-if="problem" :title="problem" type="error" :closable="false" />
		<el-button :loading="busy" @click="load(false)">刷新</el-button>
		<el-table :data="rows" empty-text="还没有任务；先发布独立采集器，再创建采集任务。">
			<el-table-column prop="name" label="任务" /><el-table-column prop="revision" label="草稿 revision" width="140" />
			<el-table-column prop="stage_count" label="阶段" width="80" /><el-table-column label="输入"
				><template #default="{ row }">{{ row.input_count }} 条手动输入</template></el-table-column
			>
			<el-table-column label="入口保存表"
				><template #default="{ row }">表 {{ row.table_id.slice(0, 8) }} · Schema {{ row.schema_version }}</template></el-table-column
			>
			<el-table-column label="操作"
				><template #default="{ row }"
					><el-button link type="primary" @click="router.push('/app/co/tasks/' + row.id)">打开任务</el-button></template
				></el-table-column
			> </el-table
		><el-button v-if="cursor" :loading="busy" @click="load(true)">加载更多</el-button>
	</section>
</template>
<script setup lang="ts">
import { onMounted, ref } from 'vue';
import { useRouter } from 'vue-router';
import { taskApi, type TaskSummary } from './tasks-api';
import { describe } from './api';
const router = useRouter(),
	rows = ref<TaskSummary[]>([]),
	cursor = ref(''),
	busy = ref(false),
	problem = ref('');
async function load(more = false) {
	busy.value = true;
	problem.value = '';
	try {
		const page = await taskApi.list(more ? cursor.value : '');
		rows.value = more ? [...rows.value, ...page.items] : page.items;
		cursor.value = page.next_cursor || '';
	} catch (e) {
		problem.value = describe(e);
	} finally {
		busy.value = false;
	}
}
onMounted(() => load());
</script>
<style src="./style.css"></style>
