<template>
	<el-card
		><template #header>日常运行 · 计划与版本升级</template> <el-alert v-if="problem" :title="problem" type="error" :closable="false" /><el-alert
			v-if="notice"
			:title="notice"
			type="info"
			:closable="false"
		/>
		<el-tabs
			><el-tab-pane label="计划触发">
				<el-form label-position="top"
					><el-form-item label="固定任务版本"
						><el-select v-model="scheduleVersion"
							><el-option v-for="v in versions" :key="v.id" :value="v.id" :label="'任务版本 ' + v.number" /></el-select></el-form-item
					><el-form-item label="间隔（秒，60秒..30天）"><el-input-number v-model="interval" :min="60" :max="2592000" /></el-form-item
					><el-checkbox v-model="enabled">启用计划</el-checkbox></el-form
				>
				<p>有活动运行时跳过。停机期间合并一次到期触发；UTC计算。表/文件复用确认快照，新增目标需重新取样并发布。</p>
				<p v-if="schedule">计划 revision {{ schedule.revision }} · 下一次 {{ new Date(schedule.next_run_at).toLocaleString() }}</p>
				<el-button :loading="busy" :disabled="!scheduleVersion" @click="saveSchedule">保存计划</el-button
				><el-button :disabled="busy" @click="load">刷新计划</el-button>
				<details>
					<summary>API触发示例</summary>
					<pre>
POST /api/v4/tasks/{{ task.id }}/runs
Authorization: Bearer …
Idempotency-Key: your-stable-request-key
{"task_version_id":"{{ scheduleVersion }}","namespace":"formal"}</pre
					>
					<p>响应丢失用原幂等键查询 /api/v4/operations/by-key?operation=startTask&amp;resource_id={{ task.id }}&amp;key=原键。</p>
				</details> </el-tab-pane
			><el-tab-pane label="持续数据订阅"><TaskSubscription :key="task.id" :task="task" :versions="versions" :dirty="dirty" /></el-tab-pane
			><el-tab-pane label="采集器版本升级">
				<el-select v-model="targetVersion" placeholder="所选阶段的新采集器版本"
					><el-option v-for="v in collectorVersions" :key="v.id" :value="v.id" :label="'版本 ' + v.number + ' · revision ' + v.revision" /></el-select
				><el-button :disabled="busy || !targetVersion || dirty" @click="previewUpgrade">预演 {{ selectedStage }} 升级</el-button>
				<p v-if="dirty">请先保存当前草稿再预演升级，防止丢失本地修改。</p>
				<template v-if="upgrade"
					><p>
						输入Schema {{ upgrade.input_changed ? '变化' : '相同' }} · 输出Schema {{ upgrade.output_changed ? '变化' : '相同' }} ·
						{{ upgrade.compatible ? '当前绑定兼容' : '需要调整输入/映射/Schema' }}
					</p>
					<el-alert v-if="upgrade.impact" :title="upgrade.impact.message" type="warning" :closable="false" />
					<details>
						<summary>输入、输出与样例差异</summary>
						<pre>{{
							JSON.stringify({ previous: upgrade.previous.definition, next: upgrade.next.definition, samples: upgrade.samples }, null, 2)
						}}</pre>
					</details>
					<el-button type="primary" :disabled="busy || dirty || !upgrade.compatible" @click="applyUpgrade">确认更新任务草稿</el-button></template
				>
				<p>确认后仍需发布新任务版本；已有运行和计划继续原版本。</p> </el-tab-pane
			><el-tab-pane label="归档"
				><p>检查活动运行并关闭计划。历史发布版本、来源及运行证据保留。</p>
				<el-button type="danger" :disabled="busy || dirty" @click="archive">归档任务</el-button></el-tab-pane
			></el-tabs
		>
		<el-button :disabled="busy" link @click="recover">查询上次计划 / 归档操作</el-button>
	</el-card>
</template>
<script setup lang="ts">
import { onMounted, ref, watch } from 'vue';
import { ElMessageBox } from 'element-plus';
import TaskSubscription from './task-subscription.vue';
import { call, coApi, describe, type Version } from './api';
import { taskApi, taskMutation, recoverTaskMutation, type Task, type TaskVersion } from './tasks-api';
const props = defineProps<{ task: Task; versions: TaskVersion[]; selectedStage: string; collectorVersion: string; dirty: boolean }>();
const emit = defineEmits<{ (e: 'upgraded'): void }>();
const schedule = ref<any>(),
	scheduleVersion = ref(''),
	interval = ref(3600),
	enabled = ref(false),
	busy = ref(false),
	problem = ref(''),
	notice = ref(''),
	targetVersion = ref(''),
	collectorVersions = ref<Version[]>([]),
	upgrade = ref<any>();
async function guard(fn: () => Promise<void>) {
	busy.value = true;
	problem.value = '';
	try {
		await fn();
	} catch (e) {
		problem.value = describe(e);
	} finally {
		busy.value = false;
	}
}
async function load() {
	await guard(async () => {
		schedule.value = (await call<any>('/tasks/' + props.task.id + '/schedule')).items[0];
		scheduleVersion.value = schedule.value?.task_version_id || props.versions[0]?.id || '';
		interval.value = schedule.value?.interval_seconds || 3600;
		enabled.value = schedule.value?.enabled || false;
	});
}
async function saveSchedule() {
	await guard(async () => {
		await taskMutation('saveSchedule', props.task.id, '/tasks/' + props.task.id + '/schedule', {
			task_version_id: scheduleVersion.value,
			interval_seconds: interval.value,
			enabled: enabled.value,
			expected_revision: schedule.value?.revision || 0,
		});
		await load();
		notice.value = '计划已保存，引用确定的任务版本';
	});
}
async function loadCollectorVersions() {
	await guard(async () => {
		upgrade.value = undefined;
		targetVersion.value = '';
		const st = props.task.definition.stages.find((s: any) => s.id === props.selectedStage);
		if (!st) return;
		const v = await coApi.version(st.collector_version_id);
		collectorVersions.value = (await coApi.versions(v.collector_id!)).items;
	});
}
async function previewUpgrade() {
	await guard(async () => {
		upgrade.value = await call('/tasks/' + props.task.id + '/upgrade-preview', 'post', {
			stage_id: props.selectedStage,
			collector_version_id: targetVersion.value,
		});
	});
}
async function confirm(text: string) {
	try {
		await ElMessageBox.confirm(text, '确认操作', { customClass: 'co-confirm', type: 'warning' });
		return true;
	} catch {
		return false;
	}
}
async function applyUpgrade() {
	if (!upgrade.value || props.dirty || !(await confirm('确认应用预演的采集器版本到任务草稿？活动运行保持原版本。'))) return;
	await guard(async () => {
		if (props.dirty) throw new Error('草稿已变化，请重新预演');
		await taskApi.save(props.task.id, upgrade.value.expected_revision, upgrade.value.proposed_definition);
		emit('upgraded');
	});
}
async function archive() {
	if (!(await confirm('确认归档此任务并关闭计划？历史数据及来源保留。'))) return;
	await guard(async () => {
		await taskMutation('archiveTask', props.task.id, '/tasks/' + props.task.id + '/archive', { expected_revision: props.task.revision });
		emit('upgraded');
	});
}
async function recover() {
	await guard(async () => {
		for (const kind of ['saveSchedule', 'archiveTask']) {
			const r = await recoverTaskMutation(kind, props.task.id);
			if (r) notice.value = '原操作：' + JSON.stringify(r);
		}
		await load();
	});
}
watch(() => [props.selectedStage, props.task.revision], loadCollectorVersions);
watch(targetVersion, () => (upgrade.value = undefined));
onMounted(async () => {
	await load();
	await loadCollectorVersions();
});
</script>
