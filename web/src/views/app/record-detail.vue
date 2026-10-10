<template>
	<div class="record-overlay" @click.self="$emit('close')">
		<aside
			class="record-drawer"
			role="dialog"
			aria-modal="true"
			aria-labelledby="record-title"
			tabindex="-1"
			ref="drawer"
			@keydown.esc="$emit('close')"
			@keydown.tab="trap"
		>
			<div class="pane-heading">
				<h2 id="record-title">记录详情</h2>
				<button @click="$emit('close')" aria-label="关闭记录详情">关闭</button>
			</div>
			<p v-if="loading" role="status">正在读取记录…</p>
			<p v-if="message" class="app-error" role="alert">{{ message }} <button @click="reload">重试</button></p>
			<template v-if="record">
				<p>
					<router-link :to="`/app/data/${record.record.table_id}`">{{ record.record.table_name }}</router-link> · 修订 {{ record.record.revision }} ·
					Schema {{ record.record.schema_version }}
				</p>
				<p class="muted">首次 {{ record.record.first_observed_at }}<br />最近观察 {{ record.record.last_observed_at }}</p>
				<dl class="record-fields">
					<template v-for="field in record.fields" :key="field.field_key"
						><dt>
							{{ field.name }} <small>{{ field.type }}</small>
						</dt>
						<dd>
							<pre>{{ displayJSON(field.value_json) }}</pre>
						</dd></template
					>
				</dl>
				<h3>来源观察</h3>
				<p class="muted">每次实际写入都有来源，未变数据也保留观察。来源值是本次输入，可能与合并后的记录不同。</p>
				<p v-if="!observations.length">暂无来源观察。</p>
				<article v-for="item in observations" :key="item.observation_id" class="trace-item">
					<strong>{{ item.outcome }} · {{ item.observed_at }}</strong>
					<p class="source-url">{{ item.source_url }}</p>
					<div class="editor-actions">
						<router-link :to="`/app/collectors/${item.collector_id}`">采集方案</router-link
						><router-link :to="`/app/runs/${item.run_id}?version=1`">发布 v{{ item.version_number }}</router-link
						><router-link :to="`/app/runs/${item.run_id}`">来源运行</router-link
						><router-link :to="`/app/pages/${item.page_id}`">{{ item.page_role }} 页面证据</router-link>
					</div>
					<details>
						<summary>本次来源值 · Schema {{ item.schema_version }}</summary>
						<pre>{{ item.values_json }}</pre>
					</details>
				</article>
				<button v-if="obsCursor" :disabled="loading" @click="moreObservations">更多来源</button>
				<h3>记录变化</h3>
				<p v-if="!revisions.length">暂无修订。</p>
				<article v-for="item in revisions" :key="item.revision" class="trace-item">
					<strong>修订 {{ item.revision }} · {{ item.created_at }}</strong>
					<p>变化字段 {{ item.changed_fields_json }}</p>
					<details>
						<summary>变化前 / 变化后</summary>
						<pre>
之前 {{ item.previous_values_json || '首次创建' }}
之后 {{ item.values_json }}</pre>
						<p class="muted">Schema {{ item.schema_version }} · 来源观察 {{ item.observation_id }}</p>
					</details>
				</article>
				<button v-if="revisionCursor" :disabled="loading" @click="moreRevisions">更多修订</button>
			</template>
		</aside>
	</div>
</template>
<script setup lang="ts">
import { ref, watch, onBeforeUnmount, nextTick } from 'vue';
import { appApi, type StoredRecord, type DataRow } from './api';
import { readError, displayJSON } from './data-read';
const props = defineProps<{ id: string }>();
defineEmits<{ (event: 'close'): void }>();
const record = ref<StoredRecord>(),
	observations = ref<DataRow[]>([]),
	revisions = ref<DataRow[]>([]),
	obsCursor = ref(''),
	revisionCursor = ref(''),
	loading = ref(false),
	message = ref(''),
	drawer = ref<HTMLElement>();
let epoch = 0;
const previousFocus = document.activeElement as HTMLElement | null;
function trap(event: KeyboardEvent) {
	const list = drawer.value?.querySelectorAll<HTMLElement>('button:not(:disabled),a[href],summary,[tabindex="0"]');
	if (!list?.length) return;
	const first = list[0],
		last = list[list.length - 1];
	if (event.shiftKey && (document.activeElement === first || document.activeElement === drawer.value)) {
		event.preventDefault();
		last.focus();
	} else if (!event.shiftKey && document.activeElement === last) {
		event.preventDefault();
		first.focus();
	}
}
async function reload() {
	const token = ++epoch;
	loading.value = true;
	message.value = '';
	record.value = undefined;
	observations.value = [];
	revisions.value = [];
	obsCursor.value = '';
	revisionCursor.value = '';
	try {
		const [r, o, v] = await Promise.all([appApi.record(props.id), appApi.observations(props.id), appApi.revisions(props.id)]);
		if (token !== epoch) return;
		record.value = r;
		observations.value = o.items;
		obsCursor.value = o.next_cursor;
		revisions.value = v.items;
		revisionCursor.value = v.next_cursor;
	} catch (e) {
		if (token === epoch) message.value = readError(e);
	} finally {
		if (token === epoch) loading.value = false;
	}
}
async function moreObservations() {
	if (loading.value) return;
	const token = epoch;
	loading.value = true;
	try {
		const r = await appApi.observations(props.id, obsCursor.value);
		if (token === epoch) {
			observations.value.push(...r.items);
			obsCursor.value = r.next_cursor;
		}
	} catch (e) {
		if (token === epoch) message.value = readError(e);
	} finally {
		if (token === epoch) loading.value = false;
	}
}
async function moreRevisions() {
	if (loading.value) return;
	const token = epoch;
	loading.value = true;
	try {
		const r = await appApi.revisions(props.id, revisionCursor.value);
		if (token === epoch) {
			revisions.value.push(...r.items);
			revisionCursor.value = r.next_cursor;
		}
	} catch (e) {
		if (token === epoch) message.value = readError(e);
	} finally {
		if (token === epoch) loading.value = false;
	}
}
watch(
	() => props.id,
	async () => {
		void reload();
		await nextTick();
		drawer.value?.focus();
	},
	{ immediate: true }
);
onBeforeUnmount(() => {
	epoch++;
	previousFocus?.focus();
});
</script>
