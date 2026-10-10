<template>
	<section v-if="run.summary.committed && first" class="app-card first-data">
		<div class="eyebrow">正式数据已保存</div>
		<h3>这次采集拿到了什么</h3>
		<p>
			固定发布 v{{ run.version_number }} · 文档 {{ run.summary.pages || 0 }} · 输出候选 {{ run.summary.candidates || 0 }} · 停止原因
			{{ run.summary.stop_reason }}
		</p>
		<p>新增 {{ run.summary.counts?.created || 0 }} / 更新 {{ run.summary.counts?.updated || 0 }} / 未变 {{ run.summary.counts?.unchanged || 0 }}</p>
		<h4>一条实际保存的记录</h4>
		<pre>{{ first.values_json }}</pre>
		<p class="muted">这是本次提交时的结果；记录详情展示当前值及后续修订。</p>
		<div class="editor-actions">
			<router-link v-if="tableID" class="primary-link" :to="`/app/data/${tableID}`">查看全部数据</router-link
			><router-link :to="`/app/records/${first.record_id}`">查看记录与来源</router-link
			><router-link :to="`/app/runs/${run.run_id}`">运行详情</router-link>
		</div>
		<p v-if="message" class="app-error">{{ message }} <button @click="lookup">重新读取数据表</button></p>
	</section>
</template>
<script setup lang="ts">
import { computed, ref, watch, onBeforeUnmount } from 'vue';
import { appApi, type FormalRun } from './api';
import { readError } from './data-read';
const props = defineProps<{ run: FormalRun }>();
const first = computed(() => props.run.summary.writes?.[0]),
	tableID = ref(''),
	message = ref('');
let epoch = 0;
async function lookup() {
	const token = ++epoch;
	tableID.value = '';
	message.value = '';
	if (!first.value) return;
	try {
		const r = await appApi.record(first.value.record_id);
		if (token === epoch) tableID.value = r.record.table_id;
	} catch (e) {
		if (token === epoch) message.value = readError(e);
	}
}
watch(() => first.value?.record_id, lookup, { immediate: true });
onBeforeUnmount(() => epoch++);
</script>
