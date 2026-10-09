<template>
	<section class="output-panel">
		<div class="pane-heading">
			<h3>保存步骤 · 数据表与输出确认</h3>
			<button :disabled="busy" @click="loadTables(false)">刷新数据表</button>
		</div>
		<p class="muted">先预演映射、唯一键和更新结果，再确认建表/绑定；此处只保存配置，不写正式记录。每个方案当前绑定一个输出步骤与页面角色。</p>
		<p v-if="draft.definition.output" class="status-good">
			已绑定表 {{ draft.definition.output.table_id }} · Schema {{ draft.definition.output.schema_version }} · {{ draft.definition.output.step_id }} /
			{{ draft.definition.output.stage }}；更改规则后需重新预演。
		</p>
		<p v-if="message" class="app-error" role="status">{{ message }}</p>
		<p v-if="pendingKey" class="status-warn">
			上次预演响应未确认。<button :disabled="busy" @click="recoverCheck">查询原请求</button
			><button :disabled="busy" @click="clearPending">结束本地等待</button>
		</p>
		<p v-if="pendingConfirm" class="status-warn">上次确认响应未收到。<button :disabled="busy" @click="recoverConfirm">读取服务器确认状态</button></p>
		<div class="command-form-grid">
			<label
				>目标表<select v-model="tableId" :disabled="busy || !!pendingKey || !!pendingConfirm" @change="chooseTable">
					<option value="">新建逻辑表（确认时创建）</option>
					<option v-for="table in tables" :key="table.table_id" :value="table.table_id">
						{{ table.name }} · {{ table.table_id }} / Schema {{ table.schema_version }}
					</option>
				</select></label
			>
			<button v-if="nextCursor" :disabled="busy" @click="loadTables(true)">加载更多数据表</button>
			<label v-if="!tableId">新表名称<input v-model="tableName" maxlength="160" :disabled="busy || !!pendingKey || !!pendingConfirm" /></label>
			<label
				>同键记录更新<select v-model="updatePolicy" :disabled="busy || !!pendingKey || !!pendingConfirm">
					<option value="update">更新变化字段</option>
					<option value="keep_existing">保留已存在记录</option>
				</select></label
			>
			<label
				>空值处理<select v-model="emptyPolicy" :disabled="busy || !!pendingKey || !!pendingConfirm">
					<option value="preserve_existing">空值不覆盖已有值</option>
					<option value="overwrite">空值覆盖已有值</option>
				</select></label
			>
		</div>
		<p class="muted">
			固定输入 {{ capture?.capture_id || '未选择' }} · {{ stepId || '未选择步骤' }} /
			{{ stage }}。同键同值重复项预计未变；同批同键不同值会冲突，不按顺序覆盖。
		</p>
		<div class="editor-actions">
			<button v-if="!tableId" :disabled="busy || blocked || !sources.length || !!pendingKey || !!pendingConfirm" @click="generate">
				从已保存字段生成新表草案
			</button>
		</div>
		<div class="record-preview-table">
			<table>
				<thead>
					<tr>
						<th>保存字段</th>
						<th>字段键</th>
						<th>类型</th>
						<th>多值</th>
						<th>允许空值</th>
						<th>唯一键</th>
						<th>来源字段映射</th>
						<th>操作</th>
					</tr>
				</thead>
				<tbody>
					<tr v-for="(field, index) in schema.fields" :key="index">
						<td><input v-model="field.name" :disabled="lockedSchema" maxlength="64" aria-label="目标字段名称" /></td>
						<td><input v-model="field.field_key" :disabled="lockedSchema" maxlength="128" aria-label="目标字段键" /></td>
						<td>
							<select v-model="field.type" :disabled="lockedSchema">
								<option v-for="type in ['string', 'integer', 'number', 'boolean', 'date', 'datetime', 'json']" :key="type" :value="type">
									{{ type }}
								</option>
							</select>
						</td>
						<td><input v-model="field.multiple" :disabled="lockedSchema" type="checkbox" aria-label="多值" /></td>
						<td><input v-model="field.nullable" :disabled="lockedSchema" type="checkbox" aria-label="可空" /></td>
						<td>
							<input
								:checked="schema.unique_key.includes(field.field_key)"
								:disabled="lockedSchema || field.multiple || field.type === 'json'"
								type="checkbox"
								aria-label="唯一键"
								@change="toggleKey(field.field_key, ($event.target as HTMLInputElement).checked)"
							/>
						</td>
						<td>
							<select
								:value="sourceFor(field.field_key)"
								:disabled="busy || !!pendingKey || !!pendingConfirm"
								@change="mapField(field.field_key, ($event.target as HTMLSelectElement).value)"
							>
								<option value="">不映射（可空字段）</option>
								<option v-for="source in sources" :key="source.field_key" :value="source.field_key">
									{{ source.name }} · {{ source.type || '动态类型' }}{{ source.multiple ? ' / 多值' : '' }}
								</option>
							</select>
						</td>
						<td><button v-if="!tableId" :disabled="lockedSchema" @click="removeField(index)">删除</button></td>
					</tr>
				</tbody>
			</table>
		</div>
		<p v-if="!schema.fields.length" class="muted">尚未确认字段。可从提取规则生成表草案，也可选择已有表进行显式映射。</p>
		<p v-if="tableId" class="muted">已有表 Schema 只读；所有非空字段和唯一键必须映射。未保存的来源字段会在预演中明确提示。</p>
		<p>唯一键：{{ schema.unique_key.join(' + ') || '尚未选择，不能确认' }} · 复合键按所选顺序组合；0 和 false 是有效键值，空值不能作为键。</p>
		<div class="editor-actions">
			<button
				:disabled="
					busy || blocked || !capture || capture.status !== 'succeeded' || !stepId || !!pendingKey || !!pendingConfirm || !schema.fields.length
				"
				@click="runCheck"
			>
				{{ busy ? '处理中…' : '预演保存结果' }}</button
			><button :disabled="busy || blocked || !check || stale || !check.ready || !!pendingKey || !!pendingConfirm" @click="confirm">
				确认保存输出配置{{ tableId ? '' : '并创建表' }}
			</button>
		</div>
		<section v-if="check">
			<p v-if="stale" class="status-warn">配置、规则、输入或时效已变化，以下预演已过期，请重新预演。</p>
			<p :class="check.ready ? 'status-good' : 'status-warn'">
				{{ check.ready ? '可确认配置' : '存在阻断项' }} · 新增 {{ check.result.counts.created }} / 更新 {{ check.result.counts.updated }} / 未变
				{{ check.result.counts.unchanged }} / 无效 {{ check.result.counts.invalid }} / 冲突 {{ check.result.counts.conflict }}
			</p>
			<p class="muted">
				Schema {{ check.schema_version }} · 规则 revision {{ check.collector_revision }} · 有效期
				{{ new Date(check.expires_at).toLocaleTimeString() }}；这是当前数据库状态预判，正式运行会重新判断，不保证届时仍是新增/更新。
			</p>
			<ul>
				<li v-for="(issue, index) in check.result.compatibility.issues" :key="`e${index}`" class="app-error">
					<button :disabled="stale || !issue.source_field_key" @click="focus(issue.source_field_key)">
						{{ issueLabel(issue.code) }} · {{ issue.source_field_key || issue.target_field_key }}
					</button>
				</li>
				<li v-for="(issue, index) in check.result.compatibility.warnings" :key="`w${index}`" class="status-warn">
					{{ issueLabel(issue.code) }} · {{ issue.source_field_key || issue.target_field_key }}
				</li>
			</ul>
			<div class="record-preview-table">
				<table>
					<thead>
						<tr>
							<th>候选</th>
							<th>预计保存</th>
							<th>唯一键值</th>
							<th>目标值</th>
							<th>变化/诊断</th>
						</tr>
					</thead>
					<tbody>
						<tr v-for="row in check.result.rows" :key="row.index">
							<td>{{ row.index + 1 }}</td>
							<td>
								{{ decisionLabel(row.decision) }}
								<p>{{ issueLabel(row.reason || '') }}</p>
								<small v-if="row.record_id">记录 {{ row.record_id }} / revision {{ row.record_revision }}</small>
							</td>
							<td>
								<pre>{{ row.key_json }}</pre>
							</td>
							<td>
								<pre>{{ row.values_json }}</pre>
							</td>
							<td>
								{{ row.changed_fields.join('、') }}
								<p v-for="(issue, index) in row.issues" :key="index">
									<button :disabled="stale || !issue.source_field_key" @click="focus(issue.source_field_key)">
										{{ issueLabel(issue.code) }} · {{ issue.source_field_key || issue.target_field_key }}
									</button>
								</p>
							</td>
						</tr>
					</tbody>
				</table>
			</div>
			<p v-for="warning in check.result.warnings" :key="warning" class="muted">{{ issueLabel(warning) }}</p>
		</section>
	</section>
</template>
<script setup lang="ts">
import { computed, onMounted, onBeforeUnmount, ref } from 'vue';
import {
	appApi,
	type Collector,
	type InputCapture,
	type LogicalTable,
	type TableSchema,
	type TableField,
	type OutputMapping,
	type OutputCheck,
	type OutputCheckInput,
} from './api';
const props = defineProps<{ draft: Collector; capture?: InputCapture; stepId: string; stage: 'list' | 'detail'; blocked: boolean }>();
const emit = defineEmits<{
	(event: 'draft-saved', draft: Collector): void;
	(event: 'focus-field', stepId: string, key: string, role: string): void;
}>();
const tables = ref<LogicalTable[]>([]),
	nextCursor = ref(''),
	tableId = ref(''),
	tableName = ref(''),
	schema = ref<TableSchema>({ fields: [], unique_key: [] }),
	mapping = ref<OutputMapping[]>([]),
	updatePolicy = ref<'update' | 'keep_existing'>('update'),
	emptyPolicy = ref<'preserve_existing' | 'overwrite'>('preserve_existing'),
	check = ref<OutputCheck>(),
	busy = ref(false),
	message = ref(''),
	pendingKey = ref(''),
	pendingConfirm = ref(''),
	now = ref(Date.now());
let disposed = false;
let timer: ReturnType<typeof setInterval> | undefined;
const baseline = ref('');
const checkedSignature = ref('');
const state = () => JSON.stringify([tableId.value, tableName.value, schema.value, mapping.value, updatePolicy.value, emptyPolicy.value]);
const dirty = computed(() => baseline.value !== state());
const stale = computed(
	() =>
		!check.value ||
		props.blocked ||
		state() !== checkedSignature.value ||
		check.value.collector_revision !== props.draft.revision ||
		check.value.capture_id !== props.capture?.capture_id ||
		check.value.content_hash !== props.capture?.content_hash ||
		check.value.config.step_id !== props.stepId ||
		check.value.config.stage !== props.stage ||
		Date.parse(check.value.expires_at) <= now.value
);
const lockedSchema = computed(() => !!tableId.value || busy.value || !!pendingKey.value || !!pendingConfirm.value);
const sources = computed<TableField[]>(() => {
	const step = props.draft.definition.steps?.find((s: any) => s.step_id === props.stepId);
	const rules = step?.type === 'json_records' ? step.config?.fields : props.stage === 'detail' ? step?.config?.detail_fields : step?.config?.fields;
	return Array.isArray(rules)
		? rules.map((f: any) => ({
				field_key: f.field_key || f.name,
				name: f.name,
				type: f.type || (step?.type === 'record_set' ? 'string' : ''),
				nullable: !f.required,
				multiple: !!f.multiple,
			}))
		: [];
});
const storageKey = () => `scrapio:v22:output-pending:${props.draft.id}`;
const confirmKey = () => `scrapio:v22:output-confirm:${props.draft.id}`;
function errorMessage(cause: any) {
	return cause?.error?.message || cause?.message || '请求失败，本地配置已保留';
}
function issueLabel(code: string) {
	return (
		(
			{
				SOURCE_FIELD_MISSING: '来源字段不存在',
				TARGET_FIELD_MISSING: '目标字段不存在',
				MULTIPLE_TYPE_MISMATCH: '多值类型不兼容',
				FIELD_TYPE_MISMATCH: '字段类型不兼容',
				REQUIRED_TARGET_UNMAPPED: '必填目标未映射',
				UNIQUE_KEY_UNMAPPED: '唯一键未映射',
				SOURCE_TYPE_DYNAMIC: '来源类型动态，按候选逐项校验',
				SOURCE_NOT_SAVED: '该来源字段未保存',
				EXTRACTION_INVALID: '提取候选无效',
				REQUIRED_TARGET_EMPTY: '必填目标为空',
				VALUE_TYPE_MISMATCH: '值类型不兼容',
				UNIQUE_KEY_EMPTY: '唯一键为空',
				DUPLICATE_KEY_DIFFERENT_VALUES: '同批同键不同值，需修正规则',
				DUPLICATE_KEY_IDENTICAL: '同批相同键和值重复',
				KEEP_EXISTING: '按策略保留已有记录',
				DATABASE_STATE_MAY_CHANGE: '预判基于当前数据库，正式执行须重新判断',
				NO_CANDIDATES: '没有候选，不能确认',
				PREVIEW_INCOMPLETE: '预览覆盖被截取，不能确认',
				RECORD_BUDGET_EXCEEDED: '目标记录超出预算',
			} as Record<string, string>
		)[code] || code
	);
}
function decisionLabel(code: string) {
	return { created: '新增', updated: '更新', unchanged: '未变', invalid: '无效', conflict: '冲突' }[code] || code;
}
async function loadTables(more = false) {
	if (busy.value) return;
	busy.value = true;
	try {
		const page = await appApi.tables(more ? nextCursor.value : '');
		if (!disposed) {
			const selected = tables.value.find((table) => table.table_id === tableId.value);
			const items = more ? [...tables.value, ...page.items] : page.items;
			if (selected && !items.some((table) => table.table_id === selected.table_id)) items.unshift(selected);
			tables.value = items.filter((table, index) => items.findIndex((item) => item.table_id === table.table_id) === index);
			nextCursor.value = page.next_cursor;
		}
	} catch (cause) {
		message.value = errorMessage(cause);
	} finally {
		busy.value = false;
	}
}
function sourceFor(key: string) {
	return mapping.value.find((m) => m.target_field_key === key)?.source_field_key || '';
}
function mapField(key: string, source: string) {
	mapping.value = mapping.value.filter((m) => m.target_field_key !== key);
	if (source) mapping.value.push({ source_field_key: source, target_field_key: key });
}
function toggleKey(key: string, on: boolean) {
	schema.value.unique_key = schema.value.unique_key.filter((k) => k !== key);
	if (on) schema.value.unique_key.push(key);
}
function removeField(index: number) {
	const key = schema.value.fields[index].field_key;
	schema.value.fields.splice(index, 1);
	schema.value.unique_key = schema.value.unique_key.filter((k) => k !== key);
	mapping.value = mapping.value.filter((m) => m.target_field_key !== key);
}
function generate() {
	if (dirty.value && schema.value.fields.length && !window.confirm('用保存字段重建新表草案会覆盖当前表配置，确认？')) return;
	schema.value = { fields: sources.value.map((f) => ({ ...f, type: f.type || 'json' })), unique_key: [] };
	mapping.value = schema.value.fields.map((f) => ({ source_field_key: f.field_key, target_field_key: f.field_key }));
	tableName.value ||= `${props.draft.name}数据`;
	message.value = '已生成草案，请选择唯一键并检查类型、可空字段；还未创建数据表。';
}
async function chooseTable() {
	if (!tableId.value) {
		schema.value = { fields: [], unique_key: [] };
		mapping.value = [];
		return;
	}
	busy.value = true;
	try {
		const value = await appApi.table(tableId.value);
		if (!disposed) {
			schema.value = JSON.parse(JSON.stringify(value.schema));
			mapping.value = value.schema.fields.flatMap((f) => {
				const source = sources.value.find((s) => s.field_key === f.field_key);
				return source ? [{ source_field_key: source.field_key, target_field_key: f.field_key }] : [];
			});
		}
	} catch (cause) {
		message.value = errorMessage(cause);
	} finally {
		busy.value = false;
	}
}
function acceptCheck(value: OutputCheck) {
	check.value = value;
	checkedSignature.value = state();
	message.value = value.ready ? '预演完成，请检查结果后确认保存。' : '预演存在阻断项，请修正后重新检查。';
}
async function runCheck() {
	if (busy.value || props.blocked || pendingKey.value || pendingConfirm.value || !props.capture) return;
	busy.value = true;
	try {
		const key = crypto.randomUUID();
		pendingKey.value = key;
		sessionStorage.setItem(storageKey(), key);
		const target = tables.value.find((t) => t.table_id === tableId.value) || (tableId.value ? await appApi.table(tableId.value) : undefined);
		const input: OutputCheckInput = {
			expected_revision: props.draft.revision,
			capture_id: props.capture.capture_id,
			step_id: props.stepId,
			stage: props.stage,
			mapping: mapping.value,
			update_policy: updatePolicy.value,
			empty_policy: emptyPolicy.value,
			...(target
				? { table_id: target.table_id, schema_version: target.schema_version }
				: { proposed_table: { name: tableName.value, schema: schema.value } }),
		};
		const signature = state(),
			revision = props.draft.revision,
			inputID = props.capture.capture_id;
		const value = await appApi.createOutputCheck(props.draft.id, input, key);
		if (!disposed) {
			pendingKey.value = '';
			sessionStorage.removeItem(storageKey());
			if (signature === state() && revision === props.draft.revision && inputID === props.capture?.capture_id) {
				acceptCheck(value);
			} else {
				check.value = value;
				checkedSignature.value = signature;
				message.value = '预演已保存，但上下文变化，结果已过期。';
			}
		}
	} catch (cause: any) {
		if (cause?.error && cause.error.code !== 'INTERNAL') {
			pendingKey.value = '';
			sessionStorage.removeItem(storageKey());
		}
		message.value = errorMessage(cause);
	} finally {
		busy.value = false;
	}
}
async function recoverCheck() {
	if (busy.value || !pendingKey.value) return;
	busy.value = true;
	try {
		const value = await appApi.outputCheckByKey(props.draft.id, pendingKey.value);
		if (!disposed) {
			check.value = value;
			checkedSignature.value = '';
			pendingKey.value = '';
			sessionStorage.removeItem(storageKey());
			message.value = '已恢复原预演；为确认当前配置，请重新预演。';
		}
	} catch (cause) {
		message.value = errorMessage(cause);
	} finally {
		busy.value = false;
	}
}
function clearPending() {
	if (window.confirm('结束本地等待不删除服务器预演，确认？')) {
		pendingKey.value = '';
		sessionStorage.removeItem(storageKey());
	}
}
function applyConfirmed(value: Collector) {
	baseline.value = state();
	pendingConfirm.value = '';
	sessionStorage.removeItem(confirmKey());
	emit('draft-saved', value);
	message.value = '输出配置已确认；正式数据尚未写入。';
}
async function confirm() {
	if (busy.value || props.blocked || stale.value || !check.value?.ready || pendingConfirm.value) return;
	if (!window.confirm('确认保存字段映射、唯一键及更新策略？新表只在这次确认时创建，不写正式记录。')) return;
	busy.value = true;
	const id = check.value.check_id;
	try {
		pendingConfirm.value = id;
		sessionStorage.setItem(confirmKey(), id);
		const value = await appApi.confirmOutput(props.draft.id, check.value.collector_revision, id);
		if (!disposed) {
			applyConfirmed(value);
			tableId.value = value.definition.output.table_id;
			baseline.value = state();
			const target = await appApi.table(tableId.value);
			if (!disposed) {
				tables.value = [target, ...tables.value.filter((t) => t.table_id !== target.table_id)];
				baseline.value = state();
			}
		}
	} catch (cause: any) {
		if (cause?.error && cause.error.code !== 'INTERNAL') {
			pendingConfirm.value = '';
			sessionStorage.removeItem(confirmKey());
		}
		message.value = errorMessage(cause);
	} finally {
		busy.value = false;
	}
}
async function recoverConfirm() {
	if (busy.value || !pendingConfirm.value) return;
	busy.value = true;
	try {
		const value = await appApi.collector(props.draft.id);
		if (!disposed) {
			if (value.definition.output?.check_id === pendingConfirm.value) {
				tableId.value = value.definition.output.table_id;
				mapping.value = JSON.parse(JSON.stringify(value.definition.output.mapping));
				updatePolicy.value = value.definition.output.update_policy;
				emptyPolicy.value = value.definition.output.empty_policy;
				const target = await appApi.table(tableId.value);
				schema.value = JSON.parse(JSON.stringify(target.schema));
				applyConfirmed(value);
			} else {
				message.value = '服务端尚未绑定该预演；已读取当前草稿，请重新预演确认。';
				pendingConfirm.value = '';
				sessionStorage.removeItem(confirmKey());
				emit('draft-saved', value);
			}
		}
	} catch (cause) {
		message.value = errorMessage(cause);
	} finally {
		busy.value = false;
	}
}
function focus(key?: string) {
	if (key && !stale.value) emit('focus-field', check.value!.config.step_id, key, check.value!.config.stage);
}
async function initialize() {
	const config = props.draft.definition.output;
	if (config) {
		tableId.value = config.table_id;
		mapping.value = JSON.parse(JSON.stringify(config.mapping));
		updatePolicy.value = config.update_policy;
		emptyPolicy.value = config.empty_policy;
		try {
			const target = await appApi.table(config.table_id);
			if (!disposed) {
				schema.value = JSON.parse(JSON.stringify(target.schema));
				tables.value = [target];
			}
		} catch (cause) {
			message.value = errorMessage(cause);
		}
	}
	baseline.value = state();
}
onMounted(async () => {
	pendingKey.value = sessionStorage.getItem(storageKey()) || '';
	pendingConfirm.value = sessionStorage.getItem(confirmKey()) || '';
	busy.value = true;
	await initialize();
	busy.value = false;
	if (disposed) return;
	await loadTables(false);
	if (disposed) return;
	timer = setInterval(() => (now.value = Date.now()), 1000);
});
onBeforeUnmount(() => {
	disposed = true;
	if (timer) clearInterval(timer);
});
defineExpose({ hasUnsavedChanges: () => dirty.value || busy.value || !!pendingKey.value || !!pendingConfirm.value });
</script>
