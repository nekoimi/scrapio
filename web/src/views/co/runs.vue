<template>
	<section class="co-page">
		<header class="co-heading">
			<div>
				<h1>任务运行</h1>
				<p>试采与正式运行分别保存证据，刷新不会重复触发。</p>
			</div>
			<el-button @click="load(false)">刷新</el-button>
		</header>
		<el-alert v-if="problem" :title="problem" type="error" :closable="false" /><el-table :data="rows"
			><el-table-column label="运行"
				><template #default="{ row }"
					><router-link :to="'/app/co/runs/' + row.id">{{ row.id.slice(0, 8) }}</router-link></template
				></el-table-column
			><el-table-column prop="namespace" label="模式" /><el-table-column label="状态"
				><template #default="{ row }">{{ statusLabel(row.status) }}</template></el-table-column
			><el-table-column label="输入进度"
				><template #default="{ row }">{{ row.preparing_cursor }} / {{ row.input_count }}</template></el-table-column
			><el-table-column prop="output_count" label="有效输出" /><el-table-column prop="retry_of" label="补采来源" /><el-table-column
				prop="created_at"
				label="创建时间" /></el-table
		><el-button v-if="cursor" :loading="busy" @click="load(true)">加载更多</el-button>
	</section>
</template>
<script setup lang="ts">
import { onMounted, ref } from 'vue';
import { taskApi, type Run } from './tasks-api';
import { describe } from './api';
import { statusLabel } from './status';
const rows = ref<Run[]>([]),
	cursor = ref(''),
	busy = ref(false),
	problem = ref('');
async function load(more = false) {
	busy.value = true;
	try {
		const p = await taskApi.runs(more ? cursor.value : '');
		rows.value = more ? [...rows.value, ...p.items] : p.items;
		cursor.value = p.next_cursor || '';
		problem.value = '';
	} catch (e) {
		problem.value = describe(e);
	} finally {
		busy.value = false;
	}
}
onMounted(() => load());
</script>
<style src="./style.css"></style>
