<template>
	<div class="app-page governance-page">
		<div class="eyebrow">SCRAPIO / DATA / FIELDS</div>
		<h1>{{ table?.name || '数据表' }} · 字段设置</h1>
		<p class="lead">先检查影响，再发布 Schema 新版本。历史记录按原版本保留，关联方案需要重新确认输出、试采和发布。</p>
		<div class="editor-actions">
			<router-link :to="`/app/data/${id}`">返回数据表</router-link><button :disabled="busy" @click="reset">重新读取</button>
		</div>
		<p v-if="error" class="app-error" role="alert">{{ error }}</p>
		<p v-if="notice" role="status">{{ notice }}</p>
		<p v-if="busy" role="status">处理中…</p>
		<template v-if="table && schema">
			<section class="app-card">
				<h2>当前 Schema {{ table.schema_version }}</h2>
				<p class="muted">字段标识保持稳定；修改名字不会创建新字段。唯一键及其类型在当前表内固定，变更唯一键请新建数据表。</p>
				<div class="record-preview-table">
					<table>
						<thead>
							<tr>
								<th>字段名称</th>
								<th>类型</th>
								<th>可为空</th>
								<th>多值</th>
								<th>唯一键</th>
								<th>操作</th>
							</tr>
						</thead>
						<tbody>
							<tr v-for="f in schema.fields" :key="f.field_key">
								<td>
									<input v-model="f.name" :disabled="busy || uncertain" aria-label="字段名称" maxlength="64" /><small class="muted">{{
										f.field_key
									}}</small>
								</td>
								<td>
									<select v-model="f.type" :disabled="busy || uncertain || schema.unique_key.includes(f.field_key)" aria-label="字段类型">
										<option v-for="type in types" :key="type">{{ type }}</option>
									</select>
								</td>
								<td><input v-model="f.nullable" :disabled="busy || uncertain" type="checkbox" aria-label="可为空" /></td>
								<td>
									<input
										v-model="f.multiple"
										:disabled="busy || uncertain || schema.unique_key.includes(f.field_key)"
										type="checkbox"
										aria-label="允许多值"
									/>
								</td>
								<td>{{ schema.unique_key.includes(f.field_key) ? '唯一键' : '—' }}</td>
								<td>
									<button
										:disabled="busy || uncertain || schema.unique_key.includes(f.field_key)"
										@click="schema.fields = schema.fields.filter((x) => x.field_key !== f.field_key)"
									>
										移除字段
									</button>
								</td>
							</tr>
						</tbody>
					</table>
				</div>
				<div class="editor-actions">
					<button :disabled="busy || uncertain || schema.fields.length >= 30" @click="add">添加字段</button
					><button :disabled="busy || uncertain || !dirty" @click="preview">检查影响</button>
				</div>
			</section>
			<section v-if="impact" class="app-card">
				<h2>变更影响预览</h2>
				<p>
					Schema {{ impact.expected_schema_version }} → {{ impact.expected_schema_version + 1 }} · {{ impact.changes.length }} 处字段变化 ·
					{{ impact.record_count }} 条历史记录。预览有效至 {{ localTime(impact.expires_at) }}。
				</p>
				<ul>
					<li v-for="c in impact.changes" :key="c.field_key">
						{{ c.before?.name || c.after?.name }}：{{ kind(c.kind) }}
						<span v-if="c.before && c.after"
							>{{ c.before.type }} → {{ c.after.type }}，可为空 {{ c.before.nullable }} → {{ c.after.nullable }}，多值
							{{ c.before.multiple }} → {{ c.after.multiple }}</span
						>
					</li>
				</ul>
				<p>历史记录按版本分布：{{ impact.record_versions.map((v) => `v${v.schema_version}：${v.count} 条`).join('；') || '尚无记录' }}</p>
				<p>
					离线检查了 {{ impact.sampled_records }} 条历史记录，其中 {{ impact.historical_violations.length }} 条不符合新字段规则。{{
						impact.analysis_truncated ? '这是有限抽样，不能代表全部历史数据。' : '已覆盖当前全部记录。'
					}}历史值不会被转换或补填。
				</p>
				<details v-if="impact.historical_violations.length">
					<summary>查看不兼容记录标识</summary>
					<ul>
						<li v-for="v in impact.historical_violations" :key="v.record_id">
							<router-link :to="`/app/records/${v.record_id}`">{{ v.record_id }}</router-link> · v{{ v.schema_version }} ·
							{{ v.issues.map((i) => i.code).join('、') }}
						</li>
					</ul>
				</details>
				<h3>关联方案与版本</h3>
				<p v-if="!impact.dependencies.length" class="muted">当前没有关联方案、版本、活动运行、调度或保存视图。</p>
				<ul>
					<li v-for="d in impact.dependencies" :key="`${d.kind}-${d.id}`">
						{{ dependencyKind(d.kind) }} · {{ d.name || d.id }} · {{ d.state }} · 修订 {{ d.revision }}
					</li>
				</ul>
				<p class="muted">
					已有版本和视图保持原样；旧 Schema
					的版本不能继续写入当前表，视图引用被移除的字段时会返回明确错误。暂停调度并等待活动运行结束后，回到关联方案重新绑定输出、试采和发布。
				</p>
				<p v-for="b in impact.blockers" :key="b" class="app-error">{{ blocker(b) }}</p>
				<label
					><input v-model="confirmed" type="checkbox" :disabled="busy || !!impact.blockers.length" />我确认保留旧记录，发布新
					Schema，并重新配置受影响方案。</label
				>
				<div class="editor-actions">
					<button :disabled="busy || !confirmed || !!impact.blockers.length" @click="apply">
						{{ uncertain ? '查询并重试同一次确认' : '确认发布 Schema' }}
					</button>
				</div>
			</section>
			<section class="app-card">
				<h2>不可变 Schema 历史</h2>
				<p>当前支持最多 100 个版本。记录详情、修订和查询快照保留对应版本信息。</p>
				<details v-for="h in history" :key="h.version">
					<summary>Schema {{ h.version }} · {{ localTime(h.created_at) }}</summary>
					<pre>{{ prettySchema(h.schema) }}</pre>
				</details>
			</section>
		</template>
	</div>
</template>
<script setup lang="ts">
import { computed, ref, watch, onMounted, onBeforeUnmount } from 'vue';
import { useRoute, onBeforeRouteLeave, onBeforeRouteUpdate } from 'vue-router';
import { appApi, type LogicalTable, type TableSchema, type SchemaImpact, type DataRow } from './api';
import { readError } from './data-read';
const route = useRoute(),
	id = computed(() => String(route.params.id));
const table = ref<LogicalTable>(),
	schema = ref<TableSchema>(),
	history = ref<DataRow[]>([]),
	impact = ref<SchemaImpact>();
const busy = ref(false),
	error = ref(''),
	notice = ref(''),
	confirmed = ref(false),
	uncertain = ref(false);
let epoch = 0;
const types = ['string', 'integer', 'number', 'boolean', 'date', 'datetime', 'json'];
const dirty = computed(() => !!table.value && JSON.stringify(schema.value) !== JSON.stringify(table.value.schema));
watch(
	schema,
	() => {
		if (!uncertain.value) {
			impact.value = undefined;
			confirmed.value = false;
			notice.value = '';
		}
	},
	{ deep: true, flush: 'sync' },
);
const localTime = (v: string) => new Date(v).toLocaleString();
const kind = (v: string) => ({ added: '新增', removed: '移除', changed: '修改' })[v] || v;
const dependencyKind = (v: string) =>
	({ draft: '草稿', version: '已发布版本', run: '活动运行', schedule: '调度', api_trigger: 'API 触发', view: '保存视图' })[v] || v;
function blocker(v: string) {
	return (
		{
			UNIQUE_KEY_CHANGE_REQUIRES_NEW_TABLE: '唯一键变更需要创建新数据表。',
			UNIQUE_KEY_TYPE_CHANGE_REQUIRES_NEW_TABLE: '唯一键类型变更需要创建新数据表。',
			SCHEMA_UNCHANGED: 'Schema 没有变化。',
			ACTIVE_RUNS_MUST_FINISH: '请先等待活动运行结束。',
			PAUSE_SCHEDULE_BEFORE_SCHEMA_CHANGE: '请先暂停关联调度。',
			DEPENDENCY_PREVIEW_LIMIT_EXCEEDED: '关联项超过 200 个，本次检查不能确认发布。',
			SCHEMA_VERSION_LIMIT_REQUIRES_NEW_TABLE: 'Schema 已达到 100 个版本，请创建新数据表。',
		}[v] || v
	);
}
function prettySchema(raw: string) {
	try {
		return JSON.stringify(JSON.parse(raw), null, 2);
	} catch {
		return raw;
	}
}
function add() {
	schema.value?.fields.push({
		field_key: crypto.randomUUID(),
		name: `field_${schema.value.fields.length + 1}`,
		type: 'string',
		nullable: true,
		multiple: false,
	});
}
async function load() {
	const token = ++epoch,
		current = id.value;
	busy.value = true;
	error.value = '';
	table.value = undefined;
	schema.value = undefined;
	history.value = [];
	impact.value = undefined;
	uncertain.value = false;
	confirmed.value = false;
	try {
		const [t, h] = await Promise.all([appApi.table(current), appApi.schemaHistory(current)]);
		if (token === epoch) {
			table.value = t;
			schema.value = JSON.parse(JSON.stringify(t.schema));
			history.value = h.items;
		}
	} catch (e) {
		if (token === epoch) error.value = readError(e);
	} finally {
		if (token === epoch) busy.value = false;
	}
}
function reset() {
	if (!dirty.value || window.confirm('重新读取会丢弃本地字段修改；未确认的操作请先查询原预览。')) void load();
}
async function preview() {
	if (!table.value || !schema.value) return;
	const token = epoch;
	busy.value = true;
	error.value = '';
	impact.value = undefined;
	confirmed.value = false;
	try {
		const r = await appApi.schemaCheck(id.value, {
			expected_schema_version: table.value.schema_version,
			schema: JSON.parse(JSON.stringify(schema.value)),
		});
		if (token === epoch) impact.value = r;
	} catch (e) {
		if (token === epoch) error.value = readError(e);
	} finally {
		if (token === epoch) busy.value = false;
	}
}
async function apply() {
	if (!impact.value) return;
	const token = epoch;
	const currentID = id.value;
	busy.value = true;
	error.value = '';
	try {
		const result = await appApi.confirmSchema(id.value, impact.value.check_id);
		if (token !== epoch) return;
		uncertain.value = false;
		await load();
		if (id.value === currentID) notice.value = `已发布 Schema ${result.applied_version}，历史记录保持原样。请重新确认关联方案输出。`;
	} catch (e) {
		if (token === epoch) {
			error.value = readError(e);
			const known = (e as any)?.error?.code;
			uncertain.value = !known || known === 'INTERNAL';
			if (!uncertain.value) {
				confirmed.value = false;
				impact.value = undefined;
			}
		}
	} finally {
		if (token === epoch) busy.value = false;
	}
}
function leave() {
	return (!busy.value && !uncertain.value && !dirty.value) || window.confirm('字段修改或确认结果尚未处理完，确认离开？');
}
onBeforeRouteLeave(leave);
onBeforeRouteUpdate((to, from) => to.params.id === from.params.id || leave());
watch(id, () => void load());
onMounted(() => void load());
onBeforeUnmount(() => epoch++);
</script>
