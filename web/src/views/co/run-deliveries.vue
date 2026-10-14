<template>
	<div>
		<p>交付独立于采集结果。成功仅表示aria2接受请求；下载完成及产物由外部服务管理。试采为预演，外部动作0。</p>
		<el-table :data="items">
			<el-table-column prop="binding_id" label="交付" width="130" />
			<el-table-column label="状态" width="140"
				><template #default="{ row }">{{ labels[row.status] || row.status }}</template></el-table-column
			>
			<el-table-column label="来源与原回执"
				><template #default="{ row }"
					><p>{{ row.plugin_code }} · GID {{ row.external_id || '未提交' }}</p>
					<p>{{ row.error?.message }}</p>
					<details>
						<summary>输入、回执与固定身份</summary>
						<pre class="co-json">{{
							JSON.stringify(
								{
									output_id: row.output_id,
									input: row.input,
									receipt: row.receipt,
									binding_hash: row.binding_hash,
									idempotency_key: row.idempotency_key,
									attempts: row.attempts,
								},
								null,
								2
							)
						}}</pre>
					</details></template
				></el-table-column
			>
			<el-table-column label="处理" width="210"
				><template #default="{ row }">
					<el-button v-if="row.status === 'awaiting_confirmation'" :disabled="busy" link @click="control(row, 'query')">查询原回执</el-button>
					<el-button v-if="row.status === 'awaiting_confirmation'" :disabled="busy" link @click="control(row, 'confirm_delivered')"
						>确认已交付</el-button
					>
					<el-button
						v-if="row.status === 'failed' && !row.external_id && row.namespace === 'formal'"
						:disabled="busy"
						link
						@click="control(row, 'retry')"
						>重试未提交请求</el-button
					>
					<el-button
						v-if="['queued', 'failed', 'awaiting_confirmation'].includes(row.status) && row.namespace === 'formal'"
						:disabled="busy"
						link
						type="danger"
						@click="control(row, 'cancel')"
						>结束交付</el-button
					>
				</template></el-table-column
			>
		</el-table>
		<el-alert v-if="problem" :title="problem" type="error" :closable="false" />
		<el-button :disabled="busy" @click="recover">查询上次交付控制操作</el-button>
	</div>
</template>
<script setup lang="ts">
import { ref } from 'vue';
import { ElMessageBox } from 'element-plus';
import { describe } from './api';
import { taskMutation, recoverTaskMutation } from './tasks-api';
const props = defineProps<{ runId: string; items: any[] }>();
const emit = defineEmits<{ changed: [] }>();
const busy = ref(false),
	problem = ref('');
const labels: Record<string, string> = {
	preview: '试采预演',
	queued: '等待交付',
	sending: '正在提交',
	checking: '查询原回执',
	succeeded: '已接受（未确认下载完成）',
	failed: '交付失败',
	awaiting_confirmation: '结果未知，需确认',
	cancelled: '已结束跟踪',
};
async function control(row: any, action: string) {
	const text =
		action === 'retry'
			? '已确认此前尚未提交。检查插件配置后，以同一固定身份重新提交？'
			: action === 'confirm_delivered'
				? '请先在aria2确认原GID已接受。此操作只记录人工确认，不调用外部动作，也不代表下载完成。'
				: action === 'cancel'
					? '结束本平台跟踪；已发出的请求可能仍在aria2运行，本操作不会取消外部下载。'
					: '只查询原GID，不会重复提交。';
	try {
		await ElMessageBox.confirm(text, '确认交付操作', {
			customClass: 'co-confirm',
			type: 'warning',
			confirmButtonText: '确认',
			cancelButtonText: '取消',
		});
	} catch {
		return;
	}
	busy.value = true;
	problem.value = '';
	try {
		await taskMutation('controlDelivery', props.runId, '/runs/' + props.runId + '/deliveries/control', {
			delivery_id: row.id,
			action,
			expected_revision: row.revision,
			confirmed: true,
		});
		emit('changed');
	} catch (e) {
		problem.value = describe(e);
	} finally {
		busy.value = false;
	}
}
async function recover() {
	busy.value = true;
	problem.value = '';
	try {
		await recoverTaskMutation('controlDelivery', props.runId);
		emit('changed');
	} catch (e) {
		problem.value = describe(e);
	} finally {
		busy.value = false;
	}
}
</script>
