<template>
	<section class="co-page">
		<div class="co-heading">
			<div>
				<h1>独立采集器</h1>
				<p>用结构化输入获取当前页数据，发布后交给编排任务使用。</p>
			</div>
			<el-button type="primary" @click="creating = true">新建采集器</el-button>
		</div>
		<el-alert v-if="error" :title="error" type="error" :closable="false" show-icon />
		<el-empty v-if="!rows.length && !loading" description="从一个详情地址或接口开始，制作你的第一个采集器" />
		<el-table :data="rows" v-loading="loading"
			><el-table-column prop="name" label="采集器"
				><template #default="{ row }"
					><router-link :to="'/app/co/collectors/' + row.id">{{ row.name }}</router-link></template
				></el-table-column
			><el-table-column label="获取方式"
				><template #default="{ row }">{{ row.definition.fetch?.kind || '未配置' }}</template></el-table-column
			><el-table-column label="输入 → 输出"
				><template #default="{ row }"
					>{{ row.definition.input_fields?.map((f: Field) => f.key).join(', ') || '待配置' }} →
					{{ row.definition.output?.fields?.map((f: Field) => f.key).join(', ') || '待配置' }}</template
				></el-table-column
			><el-table-column label="草稿"
				><template #default="{ row }">revision {{ row.revision }} {{ row.archived ? '· 已归档' : '' }}</template></el-table-column
			><el-table-column label="发布 / 最近测试"
				><template #default="{ row }"
					>v{{ row.version_number || 0 }} · {{ row.latest_test?.result_status || row.latest_test?.status || '未测试' }}
					{{ row.latest_test?.mode || '' }}</template
				></el-table-column
			><el-table-column label="操作" width="120"
				><template #default="{ row }"
					><el-button link type="primary" @click="router.push('/app/co/collectors/' + row.id)">打开工作台</el-button></template
				></el-table-column
			></el-table
		>
		<el-button v-if="cursor" :loading="loading" @click="load(true)">加载更多</el-button>
		<el-dialog v-model="creating" title="新建独立采集器" width="min(480px, 92vw)"
			><el-form label-position="top" @submit.prevent="create"
				><el-form-item label="名称"><el-input v-model="name" maxlength="64" placeholder="例如：电影详情或电影短评" /></el-form-item
				><el-alert v-if="createError" :title="createError" type="error" :closable="false" /></el-form
			><template #footer
				><el-button @click="creating = false">取消</el-button
				><el-button type="primary" :disabled="!name.trim()" :loading="saving" @click="create">创建</el-button></template
			></el-dialog
		>
	</section>
</template>
<script setup lang="ts">
import { onMounted, ref, watch } from 'vue';
import { useRouter } from 'vue-router';
import { call, coApi, describe, key, type Collector, type Field } from './api';
const router = useRouter(),
	rows = ref<Collector[]>([]),
	cursor = ref(''),
	loading = ref(false),
	error = ref(''),
	creating = ref(false),
	name = ref(''),
	saving = ref(false),
	createError = ref('');
let createKey = key();
watch(name, () => {
	createKey = key();
});
async function load(more = false) {
	loading.value = true;
	error.value = '';
	try {
		const p = await coApi.list(more ? cursor.value : '');
		rows.value = more ? [...rows.value, ...p.items] : p.items;
		cursor.value = p.next_cursor;
	} catch (e) {
		error.value = describe(e);
	} finally {
		loading.value = false;
	}
}
async function create() {
	saving.value = true;
	createError.value = '';
	try {
		const c = await call<Collector>('/collectors', 'post', { name: name.value }, createKey);
		creating.value = false;
		await router.push('/app/co/collectors/' + c.id);
	} catch (e) {
		createError.value = describe(e);
	} finally {
		saving.value = false;
	}
}
onMounted(() => load());
</script>
<style src="./style.css"></style>
