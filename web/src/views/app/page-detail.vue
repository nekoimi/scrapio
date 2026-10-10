<template>
	<div class="app-page">
		<h1>来源页面证据</h1>
		<button :disabled="loading" @click="reload">重新读取</button>
		<p v-if="loading" role="status">读取中…</p>
		<p v-if="message" class="app-error">{{ message }}</p>
		<template v-if="page"
			><section class="app-card">
				<div class="editor-actions">
					<router-link :to="`/app/runs/${page.run_id}`">返回来源运行</router-link
					><router-link :to="`/app/runs/${page.run_id}?version=1`">发布 v{{ page.version_number }}</router-link
					><router-link v-if="page.parent_page_id" :to="`/app/pages/${page.parent_page_id}`">父列表页面</router-link>
				</div>
				<p class="source-url">{{ result.source_url }}</p>
				<p>
					{{ result.stage }} · {{ result.step_id }} · 列表页 {{ result.list_page }} · 父记录索引 {{ result.parent_record_index }} · attempt
					{{ page.attempt }}
				</p>
				<p class="muted">来源网址仅作为证据展示，此页不会重新访问目标网站。当前保存的是 HTML/JSON 文档，不包含截图。</p>
			</section>
			<h2>字段提取诊断</h2>
			<article v-for="r in result.extraction?.records || []" :key="r.index" class="trace-item">
				<strong>来源记录 {{ r.index + 1 }} · {{ r.valid ? '有效' : '无效' }}</strong>
				<div v-for="f in r.fields" :key="f.field_key">
					<p>{{ f.name }} · {{ f.errors.join('、') || '通过' }}</p>
					<pre>
原值 {{ f.raw_json }}
输出 {{ f.value_json }}</pre>
				</div>
			</article>
			<h2>原始文档</h2>
			<p v-if="asset?.status === 'unavailable'" class="status-warn">文档未保存或已清理，无法还原原始页面。</p>
			<p v-if="asset?.status === 'corrupt'" class="status-error">文档哈希或编码校验失败，原始内容不可用。</p>
			<template v-if="asset?.status === 'available'"
				><p class="muted">
					{{ asset.content_hash }} · 已读取 {{ asset.next_offset }} / {{ asset.total_bytes }} 字节。以文本展示，不执行文档中的脚本。
				</p>
				<pre class="source-document">{{ content }}</pre>
				<button v-if="asset.has_more" :disabled="loading" @click="more">读取下一段文档</button></template
			>
		</template>
	</div>
</template>
<script setup lang="ts">
import { ref, computed, watch, onBeforeUnmount } from 'vue';
import { useRoute } from 'vue-router';
import { appApi, type DataRow, type DocumentAsset } from './api';
import { readError, parseJSON } from './data-read';
const route = useRoute(),
	page = ref<DataRow>(),
	asset = ref<DocumentAsset>(),
	content = ref(''),
	message = ref(''),
	loading = ref(false),
	result = computed(() => parseJSON<Record<string, any>>(page.value?.result_json || '{}', {}));
let epoch = 0;
async function reload() {
	const token = ++epoch;
	loading.value = true;
	message.value = '';
	page.value = undefined;
	asset.value = undefined;
	content.value = '';
	try {
		const id = String(route.params.id),
			p = await appApi.page(id);
		if (token !== epoch) return;
		page.value = p;
		const a = await appApi.document(id);
		if (token !== epoch) return;
		asset.value = a;
		content.value = a.content;
	} catch (e) {
		if (token === epoch) message.value = readError(e);
	} finally {
		if (token === epoch) loading.value = false;
	}
}
async function more() {
	if (loading.value || !asset.value?.has_more) return;
	const token = epoch,
		previous = asset.value;
	loading.value = true;
	try {
		const a = await appApi.document(String(route.params.id), previous.next_offset);
		if (token !== epoch) return;
		if (a.status !== 'available' || a.content_hash !== previous.content_hash) {
			content.value = '';
			asset.value = a;
			message.value = '文档已变化或不可用，请重新读取。';
			return;
		}
		asset.value = a;
		content.value += a.content;
	} catch (e) {
		if (token === epoch) message.value = readError(e);
	} finally {
		if (token === epoch) loading.value = false;
	}
}
watch(() => route.params.id, reload, { immediate: true });
onBeforeUnmount(() => epoch++);
</script>
