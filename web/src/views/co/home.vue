<template>
	<section class="co-page">
		<header class="co-heading">
			<div>
				<h1>采集工作台</h1>
				<p>查看任务进度，处理运行问题，继续使用已采集的数据。</p>
			</div>
			<el-button :loading="busy" @click="load">刷新</el-button>
		</header>
		<el-alert v-if="problem" :title="problem" type="error" :closable="false" />
		<div class="co-toolbar">
			<router-link to="/app/co/tasks/new"><el-button type="primary">创建编排任务</el-button></router-link
			><router-link to="/app/co/collectors">管理独立采集器</router-link><router-link to="/app/home">打开旧采集方案工作台 ↗</router-link>
		</div>
		<template v-if="dashboard"
			><div class="co-toolbar">
				<el-tag v-for="c in dashboard.counts" :key="c.status">{{ statusLabel(c.status) }} {{ c.count }}</el-tag>
			</div>
			<el-card
				><template #header>需要处理 · 确认、暂停、失败或部分覆盖</template
				><el-table :data="issues"
					><el-table-column prop="name" label="任务" /><el-table-column label="问题"
						><template #default="{ row }">{{ statusLabel(row.status) }}</template></el-table-column
					><el-table-column label="操作"
						><template #default="{ row }"><router-link :to="'/app/co/runs/' + row.id">查看阶段与具体输入</router-link></template></el-table-column
					></el-table
				></el-card
			>
			<el-card
				><template #header>最近运行 · 最多20次</template
				><el-table :data="dashboard.runs"
					><el-table-column prop="name" label="任务" /><el-table-column label="状态"
						><template #default="{ row }"
							>{{ statusLabel(row.status) }} · {{ row.namespace === 'trial' ? '试采' : '正式' }}</template
						></el-table-column
					><el-table-column label="输入 / 输出 / 积压"
						><template #default="{ row }"
							>{{ row.preparing_cursor }}/{{ row.input_count }} · 输出 {{ row.output_count }} · 积压 {{ row.backlog }}</template
						></el-table-column
					><el-table-column label="操作"
						><template #default="{ row }"><router-link :to="'/app/co/runs/' + row.id">打开运行</router-link></template></el-table-column
					></el-table
				></el-card
			>
			<el-card
				><template #header>近期数据表</template
				><el-table :data="dashboard.tables"
					><el-table-column prop="name" label="表" /><el-table-column prop="record_count" label="正式记录" /><el-table-column label="最近采集观察"
						><template #default="{ row }">{{ row.last_seen_at ? new Date(row.last_seen_at).toLocaleString() : '暂无' }}</template></el-table-column
					><el-table-column label="使用数据"
						><template #default="{ row }"
							><router-link :to="'/app/co/data?table_id=' + row.id">查看 / 交给采集器</router-link></template
						></el-table-column
					></el-table
				></el-card
			>
			<el-card
				><template #header>触发记录 · 最多20次</template
				><el-table :data="dashboard.triggers"
					><el-table-column prop="kind" label="来源" /><el-table-column label="状态"
						><template #default="{ row }">{{ statusLabel(row.status) }}</template></el-table-column
					><el-table-column label="触发时间"
						><template #default="{ row }">{{ new Date(row.created_at).toLocaleString() }}</template></el-table-column
					><el-table-column label="任务"
						><template #default="{ row }"
							><router-link :to="'/app/co/tasks/' + row.task_id">计划与任务版本</router-link>
							<details v-if="row.status === 'failed'">
								<summary>诊断</summary>
								<pre>{{ JSON.stringify(row.data, null, 2) }}</pre>
							</details></template
						></el-table-column
					></el-table
				></el-card
			>
			<el-card
				><template #header>容量与证据保留</template>
				<p>
					HTTP槽位 {{ dashboard.capacity.http_slots }} · 浏览器槽位 {{ dashboard.capacity.browser_slots }} · 同站点新工作项间隔
					{{ dashboard.capacity.site_interval_ms }}ms · 活动运行上限 {{ dashboard.capacity.max_active_runs }}
				</p>
				<p>
					待处理工作项 {{ dashboard.usage[0]?.pending_items }} · 待派发元素 {{ dashboard.usage[0]?.pending_dispatches }} · 来源证据
					{{ Math.ceil((dashboard.usage[0]?.source_bytes || 0) / 1024) }}KiB / {{ dashboard.capacity.source_storage_bytes / 1024 / 1024 }}MiB
				</p>
				<p>
					不可变输出JSON证据 {{ Math.ceil((dashboard.usage[0]?.output_bytes || 0) / 1024) }}KiB /
					{{ dashboard.capacity.output_storage_bytes / 1024 / 1024 }}MiB。此处为来源/输出JSON配额，数据库文件、索引及其他历史证据需另做磁盘监控。
				</p>
				<p>
					未引用来源
					{{ dashboard.capacity.retention_days }} 天后清理；发布引用、运行输入、输出、outbox及来源链保留。单次来源最多1000行/4MiB，文件768KiB。
				</p>
				<p class="co-muted">
					当前并发与速率为保守默认值，尚未经过部署环境吞吐实测。任务独立配置队列、输出与运行预算；环境变量调整全局容量，修改后重启。
				</p></el-card
			>
		</template>
	</section>
</template>
<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import { call, describe } from './api';
import { statusLabel } from './status';
const dashboard = ref<any>(),
	busy = ref(false),
	problem = ref('');
const issues = computed(() =>
	(dashboard.value?.runs || []).filter((r: any) => ['awaiting_confirmation', 'paused', 'failed', 'partial', 'limited'].includes(r.status))
);
async function load() {
	busy.value = true;
	problem.value = '';
	try {
		dashboard.value = await call('/governance');
	} catch (e) {
		problem.value = describe(e);
	} finally {
		busy.value = false;
	}
}
onMounted(load);
</script>
<style src="./style.css"></style>
