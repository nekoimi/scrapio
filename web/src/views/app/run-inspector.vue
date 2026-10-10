<template>
	<section class="app-card">
		<div class="pane-heading">
			<h2>页面树与检查</h2>
			<button :disabled="busy" @click="load(false)">刷新页面证据</button>
		</div>
		<p class="muted">按运行尝试 / 步骤 / 列表轮次分组，详情关联真实父文档。这里只显示已保存文档；失败但无文档的操作在事件诊断中查看。</p>
		<p v-if="error" class="app-error">{{ error }}</p>
		<p v-if="!busy && !pages.length && !error">暂无保存文档。</p>
		<div class="run-inspector-grid">
			<div>
				<section v-for="g in groups" :key="g.key" class="trace-item">
					<h3>{{ g.title }}</h3>
					<div v-for="p in g.pages" :key="p.page_id" :class="{ 'run-child': !!p.parent_page_id }">
						<button :aria-pressed="selected === p.page_id" :disabled="inspectBusy" @click="inspect(p.page_id)">
							{{ meta(p).stage === 'detail' ? '详情' : '列表' }} · #{{ p.sequence }} ·
							{{ p.evidence_status === 'stored' ? '文档已保存' : '文档不可用' }}
						</button>
						<p class="source-url">{{ meta(p).source_url }}</p>
						<router-link v-if="p.parent_page_id" :to="`/app/pages/${p.parent_page_id}`">父列表页面</router-link>
					</div>
				</section>
				<button v-if="cursor" :disabled="busy" @click="load(true)">加载更多页面</button>
			</div>
			<section aria-label="页面检查面板">
				<p v-if="inspectBusy" role="status">正在读取所选页面…</p>
				<p v-if="inspectError" class="app-error">{{ inspectError }} <button @click="inspect(selected)">重新读取</button></p>
				<template v-if="detail">
					<h3>{{ document.stage }} · {{ document.step_id }}</h3>
					<p v-if="document.error_code" class="status-error">提取失败：{{ document.error_code }}</p>
					<p class="source-url">{{ document.source_url }}</p>
					<p>
						有效 {{ document.extraction?.valid_count || 0 }} · 无效 {{ document.extraction?.invalid_count || 0 }} · 匹配
						{{ document.extraction?.match_count || 0 }}{{ document.extraction?.match_count_lower_bound ? '（至少）' : '' }}
					</p>
					<p v-if="document.extraction?.truncated" class="status-warn">结果受提取上限截断，并非完整页面。</p>
					<p v-for="w in document.extraction?.warnings || []" :key="w" class="status-warn">{{ w }}</p>
					<label><input v-model="onlyIssues" type="checkbox" />仅显示无效记录</label>
					<article v-for="r in records" :key="r.index" class="trace-item">
						<strong>来源索引 {{ r.index + 1 }} · {{ r.valid ? '有效' : '无效' }}</strong>
						<details v-for="f in r.fields" :key="f.field_key" :open="!f.valid">
							<summary>{{ f.name }} · 匹配 {{ f.match_count }} · {{ f.errors.join('、') || '通过' }}</summary>
							<pre>
原值 {{ f.raw_json }}
输出 {{ f.value_json }}</pre>
							<pre v-if="f.pointer || f.locator">{{ f.pointer || JSON.stringify(f.locator) }}</pre>
						</details>
					</article>
					<p v-if="!records.length">当前条件没有记录。</p>
					<router-link :to="`/app/pages/${detail.page_id}`">打开页面证据和原始文档</router-link>
					<router-link :to="`/app/runs/${runId}?repair_document=${detail.page_id}#repair`">带此页面修复方案</router-link>
					<p class="muted">此面板只读已保存提取证据，原值和最终值保持序列化 JSON；不重新访问网站、不执行页面脚本。</p>
				</template>
				<p v-else-if="!inspectBusy">选择左侧页面，检查字段、定位和转换结果。</p>
			</section>
		</div>
	</section>
</template>
<script setup lang="ts">
import { ref, computed, watch, onBeforeUnmount } from 'vue';
import { appApi, type DataRow } from './api';
import { readError, parseJSON } from './data-read';
const props = defineProps<{ runId: string }>();
const pages = ref<DataRow[]>([]),
	cursor = ref(''),
	busy = ref(false),
	error = ref(''),
	selected = ref(''),
	detail = ref<DataRow>(),
	inspectBusy = ref(false),
	inspectError = ref(''),
	onlyIssues = ref(true);
let epoch = 0,
	inspection = 0;
const meta = (p: DataRow) => parseJSON<Record<string, any>>(p.metadata_json, {});
const document = computed(() => parseJSON<Record<string, any>>(detail.value?.result_json || '{}', {}));
const records = computed(() => (document.value.extraction?.records || []).filter((r: any) => !onlyIssues.value || !r.valid));
const groups = computed(() => {
	const map = new Map<string, { key: string; title: string; pages: DataRow[] }>();
	for (const p of pages.value) {
		const m = meta(p),
			key = `${p.attempt}/${m.step_id}/${m.list_page}`;
		if (!map.has(key)) map.set(key, { key, title: `尝试 ${p.attempt} · ${m.step_id} · 第 ${m.list_page} 轮`, pages: [] });
		map.get(key)!.pages.push(p);
	}
	return [...map.values()];
});
async function load(more = false) {
	if (busy.value) return;
	const token = epoch;
	busy.value = true;
	error.value = '';
	try {
		const p = await appApi.pages(props.runId, more ? cursor.value : '');
		if (token !== epoch) return;
		pages.value = more ? [...pages.value, ...p.items] : p.items;
		cursor.value = p.next_cursor;
	} catch (e) {
		if (token === epoch) error.value = readError(e);
	} finally {
		if (token === epoch) busy.value = false;
	}
}
async function inspect(id: string) {
	const token = ++inspection,
		current = epoch;
	selected.value = id;
	detail.value = undefined;
	inspectBusy.value = true;
	inspectError.value = '';
	try {
		const p = await appApi.page(id);
		if (token === inspection && current === epoch) detail.value = p;
	} catch (e) {
		if (token === inspection && current === epoch) inspectError.value = readError(e);
	} finally {
		if (token === inspection && current === epoch) inspectBusy.value = false;
	}
}
watch(
	() => props.runId,
	() => {
		epoch++;
		inspection++;
		pages.value = [];
		cursor.value = '';
		selected.value = '';
		detail.value = undefined;
		busy.value = false;
		inspectBusy.value = false;
		inspectError.value = '';
		void load();
	},
	{ immediate: true }
);
onBeforeUnmount(() => {
	epoch++;
	inspection++;
});
</script>
<style scoped>
.run-inspector-grid {
	display: grid;
	grid-template-columns: minmax(240px, 1fr) minmax(300px, 1.5fr);
	gap: 24px;
}
.run-child {
	margin-left: 20px;
	border-left: 2px solid var(--border, #dfe4ed);
	padding-left: 12px;
}
pre {
	white-space: pre-wrap;
	overflow-wrap: anywhere;
}
button[aria-pressed='true'] {
	outline: 2px solid #6083ce;
}
@media (max-width: 850px) {
	.run-inspector-grid {
		grid-template-columns: 1fr;
	}
}
</style>
