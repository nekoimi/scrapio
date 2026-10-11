<template>
	<div class="record-node-workspace">
		<div class="record-node-heading">
			<div>
				<h3>{{ nodeLabels[activeNode] }}</h3>
				<p class="muted">{{ detailPage ? '当前网页：详情页' : '当前网页：列表 / 单页' }} · 切换配置不会访问网站</p>
			</div>
			<button v-if="activeNode !== 'overview'" @click="activeNode = 'overview'">返回配置总览</button>
		</div>
		<nav class="record-node-nav" aria-label="当前采集步骤配置">
			<button v-for="node in nodes" :key="node.key" :aria-pressed="activeNode === node.key" @click="activeNode = node.key">
				<strong>{{ nodeLabels[node.key] }}</strong
				><small>{{ node.summary }}</small>
			</button>
		</nav>
		<div v-if="activeNode === 'overview'" class="record-node-overview">
			<p>每条记录先提取列表字段；启用详情后打开详情、提取字段并返回；当前列表完成后再翻页。</p>
			<p class="muted">单页采集可以直接添加字段。详情和翻页都是可选配置；“已配置”只说明规则存在，页面检查结果在数据预览中核对。</p>
			<button class="primary-action" :disabled="disabled || !available" @click="select('field')">
				{{ detailPage ? '从详情页选字段' : '从网页选字段' }}
			</button>
		</div>
		<fieldset v-else :disabled="disabled" class="record-node-config">
			<template v-if="activeNode === 'record'">
				<label
					>记录方式<select v-model="plan.mode" :disabled="detailPage" @change="changeMode">
						<option value="single">整页一条记录</option>
						<option value="repeated">重复容器，每项一条</option>
					</select></label
				>
				<p>{{ plan.mode === 'single' ? '整份网页输出一条记录，可直接添加字段。' : '每个匹配容器输出一条记录，字段在容器内提取。' }}</p>
				<button :disabled="!available || detailPage" @click="select('record')">{{ plan.locator ? '重新点选记录范围' : '从网页选记录范围' }}</button>
				<details class="editor-advanced">
					<summary>高级：定位与记录上限</summary>
					<label>列表累计记录上限<input v-model.number="plan.max_records" type="number" min="1" max="200" /></label>
					<div v-if="plan.mode === 'repeated' && plan.locator" class="command-form-grid">
						<label
							>定位方式<select v-model="plan.locator.strategy">
								<option value="css">CSS</option>
								<option value="xpath">XPath</option>
							</select></label
						><label>容器规则<input v-model="plan.locator.expression" maxlength="2048" :disabled="detailPage" /></label>
					</div>
				</details>
			</template>
			<template v-if="activeNode === 'fields' || activeNode === 'detail'">
				<template v-if="activeNode === 'detail'">
					<p v-if="!plan.detail" class="muted">可选：进入每条记录的详情页，补充列表没有的字段。</p>
					<div class="editor-actions">
						<button :disabled="!available || detailPage" @click="select('detail')">{{ plan.detail ? '重新点选详情入口' : '从网页选详情入口' }}</button
						><button v-if="!plan.detail" :disabled="detailPage" @click="enableDetail">手动添加路径</button
						><button v-else class="danger-action" :disabled="detailPage" @click="removeDetail">移除详情路径</button>
					</div>
					<template v-if="plan.detail">
						<p>
							打开详情 → 提取 {{ plan.detail_fields?.length || 0 }} 个字段 →
							{{ plan.detail.return_strategy === 'back' ? '后退到列表' : '重新打开列表地址' }}
						</p>
						<button :disabled="!canPreview || detailPage" @click="emit('inspect-path', 'detail')">查看详情候选</button>
						<button v-if="!detailPage" class="primary-action" :disabled="!canOpenDetail" @click="emit('open-detail')">{{ unsaved ? '保存入口并打开示例详情' : '打开示例详情并选字段' }}</button>
						<p v-if="!detailPage" class="muted">当前仍在列表页。先打开一条示例详情，再从详情网页中选字段；此操作只调试页面，不写正式数据。</p>
						<button v-else :disabled="!canReturn" @click="emit('return-list')">返回列表并重新检查</button>
						<details class="editor-advanced">
							<summary>高级：入口、返回方式与检查预算</summary>
							<div class="command-form-grid">
								<label
									>定位方式<select v-model="plan.detail.locator.strategy" :disabled="detailPage">
										<option value="css">CSS</option>
										<option value="xpath">XPath</option>
									</select></label
								><label>入口规则<input v-model="plan.detail.locator.expression" maxlength="2048" :disabled="detailPage" /></label>
								<label
									>返回方式<select v-model="plan.detail.return_strategy" :disabled="detailPage">
										<option value="back">浏览器后退</option>
										<option value="navigate-list">重新打开列表地址</option>
									</select></label
								><label>最多详情候选<input v-model.number="plan.detail.max_details" type="number" min="1" max="5" /></label>
							</div>
						</details>
					</template>
				</template>
				<template v-if="activeNode === 'fields' || plan.detail">
					<div class="pane-heading">
						<h4>{{ fieldRole === 'detail' ? '详情页字段' : '列表 / 单页字段' }}</h4>
						<div class="editor-actions">
							<button v-if="fieldRole === 'detail' && !detailPage" :disabled="!canOpenDetail" @click="emit('open-detail')">打开详情后选字段</button>
							<button v-else :disabled="!available || (fieldRole === 'detail') !== detailPage" @click="select('field')">{{ fieldRole === 'detail' ? '从当前详情页选字段' : '继续选列表字段' }}</button
							><button @click="addField">手动添加字段</button>
						</div>
					</div>
					<p v-if="activeNode === 'fields' && detailPage" class="muted">当前网页是详情页。列表字段可编辑；点选详情字段请进入“详情路径”。</p>
					<div class="record-fields-table">
						<table>
							<thead>
								<tr>
									<th>字段名称</th>
									<th>提取内容</th>
									<th>最近样例</th>
									<th>检查状态</th>
									<th>操作</th>
								</tr>
							</thead>
							<tbody>
								<tr v-for="field in fields" :key="field.field_key || field.name" :class="{ 'field-is-editing': editingField === field }">
									<td>
										<button class="field-title" @click="editField(field)">{{ field.name || '未命名字段' }}</button>
									</td>
									<td>{{ extractLabels[field.extract] }}</td>
									<td class="field-sample">{{ fieldSample(field) }}</td>
									<td>{{ fieldStatus(field) }}</td>
									<td><button :aria-expanded="editingField === field" @click="editField(field)">编辑</button></td>
								</tr>
							</tbody>
						</table>
					</div>
					<p v-if="!fields.length" class="record-empty-state">尚未添加字段。点选网页中的标题、链接或图片，也可以手动添加。</p>
					<div v-if="editingField && fields.includes(editingField)" class="record-field-editor">
						<div class="pane-heading">
							<strong>编辑字段 · {{ editingField.name || '未命名' }}</strong
							><button @click="editingField = undefined">收起编辑</button>
						</div>
						<div class="command-form-grid">
							<label>字段名称<input v-model="editingField.name" maxlength="64" /></label
							><label
								>提取内容<select v-model="editingField.extract">
									<option value="text">文本</option>
									<option value="link">链接</option>
									<option value="image">图片地址</option>
									<option value="attribute">属性</option>
								</select></label
							><label v-if="editingField.extract === 'attribute'">属性名<input v-model="editingField.attribute" placeholder="例如 data-id" /></label>
						</div>
						<button :disabled="!available || (fieldRole === 'detail') !== detailPage" @click="select('field', editingField)">重新点选此字段</button>
						<details class="editor-advanced">
							<summary>高级定位规则</summary>
							<div v-if="editingField.locator" class="command-form-grid">
								<label
									>定位方式<select v-model="editingField.locator.strategy">
										<option value="css">CSS</option>
										<option value="xpath">XPath</option>
									</select></label
								><label>定位规则<input v-model="editingField.locator.expression" maxlength="2048" /></label>
							</div>
							<span v-else>使用记录容器自身</span
							><button @click="editingField.locator = editingField.locator ? undefined : { strategy: 'css', expression: '' }">
								{{ editingField.locator ? '改用容器自身' : '指定子元素' }}
							</button>
						</details>
						<FieldOptions :field="editingField" :role="fieldRole" />
						<div class="editor-actions">
							<button :disabled="fields.indexOf(editingField) === 0" @click="moveField(-1)">上移</button
							><button :disabled="fields.indexOf(editingField) === fields.length - 1" @click="moveField(1)">下移</button
							><button class="danger-action" @click="removeField">删除字段</button>
						</div>
					</div>
				</template>
			</template>
			<template v-if="activeNode === 'next'">
				<p v-if="!plan.next_page" class="muted">可选：未启用时，只采集当前页面。</p>
				<div class="editor-actions">
					<button :disabled="!available || detailPage" @click="select('next')">{{ plan.next_page ? '重新点选翻页入口' : '从网页选翻页入口' }}</button
					><button v-if="!plan.next_page" :disabled="detailPage" @click="enableNext">手动添加翻页</button
					><button v-else class="danger-action" :disabled="detailPage" @click="plan.next_page = undefined">移除翻页</button>
				</div>
				<template v-if="plan.next_page"
					><label
						>翻页方式<select v-model="plan.next_page.kind" :disabled="detailPage">
							<option value="link">下一页链接</option>
							<option value="click">下一页按钮</option>
							<option value="load_more">加载更多</option>
						</select></label
					>
					<label>最多列表轮次<input v-model.number="plan.next_page.max_pages" type="number" min="1" max="100" :disabled="detailPage" /></label>
					<p class="muted">无入口、重复页或空页时自动结束；正式运行还受运行预算限制。加载更多受累计记录上限限制。</p>
					<button :disabled="!canPreview || detailPage" @click="emit('inspect-path', 'next')">查看翻页入口</button>
					<details class="editor-advanced">
						<summary>高级：定位与等待</summary>
						<div class="command-form-grid">
							<label
								>定位方式<select v-model="plan.next_page.locator.strategy" :disabled="detailPage">
									<option value="css">CSS</option>
									<option value="xpath">XPath</option>
								</select></label
							><label>入口规则<input v-model="plan.next_page.locator.expression" maxlength="2048" :disabled="detailPage" /></label
							><label
								>动作后等待（毫秒）<input v-model.number="plan.next_page.wait_ms" type="number" min="0" max="10000" :disabled="detailPage"
							/></label>
						</div>
					</details>
				</template>
			</template>
		</fieldset>
	</div>
</template>

<script setup lang="ts">
import { confirmAction } from '../../utils/dialog';
import { computed, nextTick, ref, watch } from 'vue';
import type { FieldRule, RecordPlan, RecordPreview, SelectionRule } from './api';
import FieldOptions from './field-options.vue';
const props = defineProps<{
	plan: RecordPlan;
	available: boolean;
	disabled: boolean;
	detailPage: boolean;
	canPreview: boolean;
	canReturn: boolean;
	canOpenDetail?: boolean;
	unsaved?: boolean;
	preview?: RecordPreview;
	previewStale: boolean;
}>();
const emit = defineEmits<{
	(event: 'select', target: SelectionRule['target'], field?: FieldRule): void;
	(event: 'inspect-path', target: 'detail' | 'next'): void;
	(event: 'return-list'): void;
	(event: 'open-detail'): void;
}>();
type Node = 'overview' | 'record' | 'fields' | 'detail' | 'next';
const activeNode = ref<Node>('overview'),
	editingField = ref<FieldRule>();
const nodeLabels: Record<Node, string> = { overview: '配置总览', record: '记录范围', fields: '列表字段', detail: '详情路径', next: '翻页方式' };
const extractLabels = { text: '文本', link: '链接', image: '图片地址', attribute: '属性' };
const nodes = computed(() => [
	{
		key: 'record' as const,
		summary: props.plan.mode === 'single' ? '整页一条' : props.plan.locator?.expression ? '重复容器 · 已配置' : '重复容器 · 待点选',
	},
	{ key: 'fields' as const, summary: `${props.plan.fields.length} 个字段` },
	{ key: 'detail' as const, summary: props.plan.detail ? `已配置 · ${props.plan.detail_fields?.length || 0} 个字段` : '未启用 · 可选' },
	{ key: 'next' as const, summary: props.plan.next_page ? '已配置 · 请检查入口' : '未启用 · 可选' },
]);
const fieldRole = computed(() => (activeNode.value === 'detail' ? ('detail' as const) : ('list' as const)));
const fields = computed(() => (fieldRole.value === 'detail' ? props.plan.detail_fields || [] : props.plan.fields));
function select(target: SelectionRule['target'], field?: FieldRule) {
	if (target === 'field' && activeNode.value !== 'overview' && (fieldRole.value === 'detail') !== props.detailPage) return;
	if (!props.disabled && props.available) emit('select', target, field);
}
function changeMode() {
	props.plan.locator = props.plan.mode === 'repeated' ? props.plan.locator || { strategy: 'css', expression: '' } : undefined;
}
function enableDetail() {
	props.plan.detail = { locator: { strategy: 'css', expression: '' }, return_strategy: 'back', max_details: 3 };
	props.plan.detail_fields ||= [];
}
function enableNext() {
	props.plan.next_page = { locator: { strategy: 'css', expression: '' }, kind: 'link', max_pages: 10, wait_ms: 500 };
}
async function removeDetail() {
	if (props.detailPage || props.disabled) return;
	const plan = props.plan;
	if (!(await confirmAction(`移除详情路径将同时删除 ${props.plan.detail_fields?.length || 0} 个详情字段。确认移除？`))) return;
	if (props.detailPage || props.disabled || props.plan !== plan) return;
	props.plan.detail = undefined;
	props.plan.detail_fields = [];
	editingField.value = undefined;
}
function addField() {
	if (props.disabled) return;
	const role = fieldRole.value;
	const target = role === 'detail' ? (props.plan.detail_fields ||= []) : props.plan.fields;
	let number = target.length + 1;
	while (target.some((field) => field.name === `field_${number}`)) number++;
	const field: FieldRule = { name: `field_${number}`, extract: 'text', locator: { strategy: 'css', expression: '' } };
	target.push(field);
	editingField.value = field;
}
function editField(field: FieldRule) {
	editingField.value = editingField.value === field ? undefined : field;
}
async function removeField() {
	const field = editingField.value;
	if (!field || props.disabled) return;
	if (!(await confirmAction(`删除字段“${field.name}”？保存后生效。`))) return;
	if (props.disabled || editingField.value !== field) return;
	const index = fields.value.indexOf(field);
	if (index >= 0) fields.value.splice(index, 1);
	editingField.value = undefined;
}
function moveField(offset: number) {
	if (props.disabled || !editingField.value) return;
	const index = fields.value.indexOf(editingField.value),
		next = index + offset;
	if (index < 0 || next < 0 || next >= fields.value.length) return;
	[fields.value[index], fields.value[next]] = [fields.value[next], fields.value[index]];
}
function previewField(field: FieldRule) {
	return props.preview?.stage === fieldRole.value ? props.preview.records[0]?.fields.find((item) => item.name === field.name) : undefined;
}
function fieldSample(field: FieldRule) {
	const row = props.preview?.stage === fieldRole.value ? props.preview.records[0] : undefined;
	return row?.values[field.name] || '—';
}
function fieldStatus(field: FieldRule) {
	const evidence = previewField(field);
	if (!evidence) return '待检查';
	if (props.previewStale) return '上次样例 · 已过期';
	return evidence.errors.length ? '检查异常' : evidence.truncated ? '检查受限' : '当前页已检查';
}
function focusSelection(target: SelectionRule['target'], field?: FieldRule) {
	activeNode.value = target === 'field' ? (props.detailPage ? 'detail' : 'fields') : target;
	editingField.value = field;
}
async function focusField(key: string, role: string) {
	activeNode.value = role === 'detail' ? 'detail' : 'fields';
	editingField.value = fields.value.find((field) => (field.field_key || field.name) === key);
	await nextTick();
	const el = document.getElementById(`v22-field-${role}-${key}`) as HTMLDetailsElement | null;
	if (el) {
		el.open = true;
		el.scrollIntoView({ block: 'nearest' });
	}
}
watch(
	() => props.plan,
	() => {
		const key = editingField.value?.field_key || editingField.value?.name;
		editingField.value = key ? fields.value.find((field) => (field.field_key || field.name) === key) : undefined;
	}
);
function resetNode() {
	activeNode.value = 'overview';
	editingField.value = undefined;
}
defineExpose({ focusSelection, focusField, resetNode });
</script>
