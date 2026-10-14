<template>
	<el-button :disabled="busy" @click="open">从指定阶段局部补采</el-button>
	<el-dialog v-model="dialog" title="创建局部补采运行" width="min(860px, 96vw)">
		<el-alert
			title="仅执行所选阶段及其下游，共享下游只接收本次保留路径的来源。未执行上游会记入范围；局部成功不代表整个任务回归通过。强制访问，不应用刷新间隔跳过。"
			type="warning"
			:closable="false"
		/>
		<el-alert v-if="problem" :title="problem" type="error" :closable="false" />
		<el-form label-position="top"
			><el-form-item label="补采阶段"
				><el-select v-model="stageID"><el-option v-for="s in stages" :key="s.id" :value="s.stage_id" :label="s.stage_id" /></el-select></el-form-item
			><el-form-item label="执行版本"
				><el-select v-model="versionID"
					><el-option :value="run.task_version_id" label="原运行冻结版本（含原试采证据）" /><el-option
						v-for="v in versions.filter((v) => v.id !== run.task_version_id)"
						:key="v.id"
						:value="v.id"
						:label="'显式修复任务版本 ' + v.number" /></el-select></el-form-item
			><el-radio-group v-model="mode"
				><el-radio-button label="items">历史输入快照</el-radio-button><el-radio-button label="outputs">历史上游输出</el-radio-button
				><el-radio-button label="table">表输入快照</el-radio-button></el-radio-group
			></el-form
		>
		<el-form-item v-if="mode === 'outputs'" label="历史输出使用的上游连接"
			><el-select v-model="edgeID"
				><el-option v-for="e in replayEdges" :key="e.id" :value="e.id" :label="e.from + ' / ' + e.port + ' · ' + e.id" /></el-select
		></el-form-item>
		<p v-if="mode === 'outputs'">只使用当前所选连接映射历史输出；其他上游不会补跑，缺失分支会记入本次范围。</p>
		<template v-if="mode !== 'table'"
			><p>选择{{ mode === 'items' ? '此阶段原工作项' : '此阶段上游输出' }}；最多100个。此页只显示当前已加载记录，可在运行详情继续加载。</p>
			<el-checkbox-group v-model="selectedIDs"
				><div v-for="r in available" :key="r.id">
					<el-checkbox :label="r.id"
						>{{ r.id.slice(0, 8) }} · {{ r.status || r.decision }} · {{ JSON.stringify(r.input_values || r.values).slice(0, 180) }}</el-checkbox
					>
				</div></el-checkbox-group
			></template
		>
		<SourcePicker
			v-else-if="collectorVersion"
			v-model="source"
			kind="table"
			:version-i-d="collectorVersion.id"
			:fields="collectorVersion.definition.input_fields"
		/>
		<p>采用新版本时会校验冻结输入及表绑定；证据缺失不读取最新记录代替。原试采保持隔离正式写入0。</p>
		<template #footer
			><el-button :disabled="busy" @click="recover">查询原补采操作</el-button><el-button @click="dialog = false">关闭</el-button
			><el-button type="primary" :disabled="busy || !stageID" @click="replay">确认访问并创建补采运行</el-button></template
		>
	</el-dialog>
</template>
<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import { useRouter } from 'vue-router';
import { ElMessageBox } from 'element-plus';
import { coApi, describe, type Version } from './api';
import { taskApi, taskMutation, recoverTaskMutation, type Run, type TaskVersion, type TaskSource } from './tasks-api';
import SourcePicker from './source-picker.vue';
const props = defineProps<{ run: Run; stages: any[]; items: any[]; outputs: any[] }>(),
	router = useRouter();
const dialog = ref(false),
	busy = ref(false),
	problem = ref(''),
	stageID = ref(''),
	edgeID = ref(''),
	versionID = ref(''),
	versions = ref<TaskVersion[]>([]),
	mode = ref('items'),
	selectedIDs = ref<string[]>([]),
	source = ref<TaskSource>({ kind: 'table', inputs: [] }),
	collectorVersion = ref<Version>();
const replayEdges = computed(() => {
	const d =
		versionID.value === props.run.task_version_id
			? props.run.manifest.definition
			: versions.value.find((v) => v.id === versionID.value)?.manifest.definition;
	return d?.edges.filter((e: any) => e.to === stageID.value) || [];
});
watch(replayEdges, () => {
	edgeID.value = replayEdges.value.length === 1 ? replayEdges.value[0].id : '';
	selectedIDs.value = [];
});
watch(edgeID, () => (selectedIDs.value = []));
const available = computed(() => {
	if (mode.value === 'items')
		return props.items.filter(
			(i: any) => props.stages.find((s: any) => s.id === i.stage_run_id)?.stage_id === stageID.value && i.status !== 'awaiting_confirmation'
		);
	const edge = replayEdges.value.find((e: any) => e.id === edgeID.value);
	return props.outputs.filter((o: any) => o.source?.stage_id === edge?.from && (o.port || 'records') === edge?.port);
});
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
async function open() {
	dialog.value = true;
	versionID.value = props.run.task_version_id;
	stageID.value = props.stages[0]?.stage_id || '';
	await guard(async () => {
		if (props.run.task_id) versions.value = (await taskApi.versions(props.run.task_id)).items;
		await loadCollector();
	});
}
async function loadCollector() {
	source.value = { kind: 'table', inputs: [] };
	selectedIDs.value = [];
	const d =
		versionID.value === props.run.task_version_id
			? props.run.manifest.definition
			: versions.value.find((v) => v.id === versionID.value)?.manifest.definition;
	const st = d?.stages.find((s: any) => s.id === stageID.value);
	collectorVersion.value = st ? await coApi.version(st.collector_version_id) : undefined;
}
watch(
	() => [stageID.value, versionID.value, mode.value],
	() => guard(loadCollector)
);
async function replay() {
	if (mode.value === 'outputs' && !edgeID.value) {
		problem.value = '请先选择一条上游连接';
		return;
	}
	if (mode.value !== 'table' && (!selectedIDs.value.length || selectedIDs.value.length > 100)) {
		problem.value = '请选择1..100个历史证据';
		return;
	}
	try {
		await ElMessageBox.confirm('确认从所选阶段再次访问、保存并执行其下游？未执行上游不算回归通过。', '确认局部补采', {
			customClass: 'co-confirm',
			type: 'warning',
		});
	} catch {
		return;
	}
	await guard(async () => {
		const result = await taskMutation('replayStage', props.run.id, '/runs/' + props.run.id + '/replay', {
			stage_id: stageID.value,
			task_version_id: versionID.value,
			confirmed: true,
			...(mode.value === 'table'
				? { source: source.value }
				: mode.value === 'items'
					? { item_ids: selectedIDs.value }
					: { output_ids: selectedIDs.value, edge_id: edgeID.value }),
		});
		await router.push('/app/co/runs/' + result.id);
	});
}
async function recover() {
	await guard(async () => {
		const result = await recoverTaskMutation('replayStage', props.run.id);
		if (result) await router.push('/app/co/runs/' + result.id);
	});
}
</script>
