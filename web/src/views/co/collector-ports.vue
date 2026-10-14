<template>
	<el-card
		><template #header>命名输出端口 · 与 records 共用页面获取和分页</template>
		<p>每个端口独立记录定位、字段与输出身份。记录预算为全部端口合计；records 控制分页与空态，附加端口允许零输出。任务中逐端口选择保存或仅传递。</p>
		<el-card v-for="(p, i) in rows" :key="i">
			<div class="co-fields">
				<el-input v-model="p.output.port" placeholder="端口名，例如 links" /><el-select v-model="p.records.strategy"
					><el-option v-for="s in strategies" :key="s" :value="s" /></el-select
				><el-input v-model="p.records.expression" placeholder="此端口的记录容器" /><el-button type="danger" plain @click="removePort(i)"
					>删除端口</el-button
				>
			</div>
			<div v-for="(f, j) in p.output.fields" :key="j" class="co-field-row">
				<div class="co-fields">
					<el-input v-model="f.key" placeholder="字段键" @change="renameField(i, j)" /><el-select v-model="f.type"
						><el-option v-for="t in types" :key="t" :value="t" /></el-select
					><el-checkbox v-model="f.required">必填</el-checkbox><el-checkbox v-model="f.nullable">null</el-checkbox
					><el-checkbox v-model="f.multiple">数组</el-checkbox><el-checkbox v-model="f.empty_allowed">允许空值</el-checkbox
					><el-button link type="danger" @click="removeField(i, j)">删除字段</el-button>
				</div>
				<div class="co-fields">
					<el-select v-model="p.output.mapping[j].source" @change="source(i, j)"
						><el-option value="element" label="元素" /><el-option value="input" label="输入透传" /><el-option value="constant" label="常量"
					/></el-select>
					<el-select v-if="p.output.mapping[j].source === 'input'" v-model="p.output.mapping[j].input_key"
						><el-option v-for="f in inputFields" :key="f.key" :value="f.key"
					/></el-select>
					<el-input
						v-else-if="p.output.mapping[j].source === 'constant'"
						:model-value="invalidConstants.get(p.output.mapping[j]) ?? JSON.stringify(p.output.mapping[j].value)"
						@change="constant(i, j, String($event))"
						placeholder="JSON常量"
					/>
					<template v-else
						><el-select v-model="p.output.mapping[j].locator!.strategy"><el-option v-for="s in strategies" :key="s" :value="s" /></el-select
						><el-input v-model="p.output.mapping[j].locator!.expression" placeholder="相对字段定位" /><el-select v-model="p.output.mapping[j].extract"
							><el-option v-for="s in ['text', 'link', 'image', 'attribute']" :key="s" :value="s" /></el-select
						><el-input v-if="p.output.mapping[j].extract === 'attribute'" v-model="p.output.mapping[j].attribute" placeholder="属性名"
					/></template>
				</div>
			</div>
			<el-button :disabled="p.output.fields.length >= 30" @click="addField(i)">添加字段</el-button>
			<el-select v-model="p.output.identity_fields" multiple placeholder="稳定输出身份（1..5个必填标量）"
				><el-option v-for="f in p.output.fields" :key="f.key" :value="f.key"
			/></el-select>
		</el-card>
		<el-button :disabled="rows.length >= 4" @click="addPort">添加命名输出端口</el-button>
		<el-alert v-if="problem" :title="problem" type="error" :closable="false" />
	</el-card>
</template>
<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import type { Definition, Field, Mapping } from './api';
const props = defineProps<{ modelValue: NonNullable<Definition['additional_outputs']>; inputFields: Field[]; json: boolean }>();
const emit = defineEmits<{ 'update:modelValue': [NonNullable<Definition['additional_outputs']>]; invalid: [boolean] }>();
const rows = computed({ get: () => props.modelValue, set: (v) => emit('update:modelValue', v) }),
	invalidConstants = ref(new Map<Mapping, string>()),
	problem = computed(() => (invalidConstants.value.size ? '常量必须为有效JSON；请修正后保存' : '')),
	types = ['string', 'url', 'integer', 'number', 'boolean', 'date', 'datetime', 'json'];
const strategies = computed(() => (props.json ? ['jsonpath'] : ['css', 'xpath']));
function addPort() {
	const json = props.json;
	let n = rows.value.length + 1;
	while (rows.value.some((p: NonNullable<Definition['additional_outputs']>[number]) => p.output.port === 'port_' + n)) n++;
	rows.value = [
		...rows.value,
		{
			records: { strategy: json ? 'jsonpath' : 'css', expression: json ? '$.links[*]' : 'a' },
			output: {
				port: 'port_' + n,
				fields: [{ key: 'id', type: 'string', required: true, nullable: false, multiple: false }],
				identity_fields: ['id'],
				mapping: [
					{
						key: 'id',
						source: 'element',
						locator: { strategy: json ? 'jsonpath' : 'xpath', expression: json ? '$.id' : '.' },
						extract: 'text',
						clean: 'trim',
					},
				],
			},
		},
	];
}
function source(i: number, j: number) {
	const m = rows.value[i].output.mapping[j];
	if (m.source === 'element' && !m.locator) m.locator = { strategy: props.json ? 'jsonpath' : 'xpath', expression: props.json ? '$.id' : '.' };
	invalidConstants.value.delete(m);
}
function addField(i: number) {
	const p = rows.value[i];
	let n = p.output.fields.length;
	while (p.output.fields.some((f: Field) => f.key === 'field_' + n)) n++;
	const key = 'field_' + n;
	p.output.fields.push({ key, type: 'string', required: true, nullable: false, multiple: false });
	p.output.mapping.push({
		key,
		source: 'element',
		locator: { strategy: props.json ? 'jsonpath' : 'xpath', expression: props.json ? '$.id' : '.' },
		extract: 'text',
		clean: 'trim',
	});
}
function renameField(i: number, j: number) {
	const p = rows.value[i],
		old = p.output.mapping[j].key,
		key = p.output.fields[j].key;
	p.output.mapping[j].key = key;
	p.output.identity_fields = p.output.identity_fields.map((k: string) => (k === old ? key : k));
}
function removeField(i: number, j: number) {
	const p = rows.value[i],
		key = p.output.fields[j].key;
	invalidConstants.value.delete(p.output.mapping[j]);
	p.output.fields.splice(j, 1);
	p.output.mapping.splice(j, 1);
	p.output.identity_fields = p.output.identity_fields.filter((k: string) => k !== key);
}
function removePort(i: number) {
	for (const m of rows.value[i].output.mapping) invalidConstants.value.delete(m);
	rows.value = rows.value.filter((_: unknown, j: number) => j !== i);
}
function constant(i: number, j: number, text: string) {
	const m = rows.value[i].output.mapping[j];
	try {
		m.value = JSON.parse(text);
		invalidConstants.value.delete(m);
	} catch {
		invalidConstants.value.set(m, text);
	}
}
watch(
	() => invalidConstants.value.size,
	(n) => emit('invalid', n > 0),
	{ immediate: true }
);
watch(
	() => props.modelValue,
	() => {
		const current = new Set<Mapping>(rows.value.flatMap((p: NonNullable<Definition['additional_outputs']>[number]) => p.output.mapping));
		for (const m of invalidConstants.value.keys()) if (!current.has(m)) invalidConstants.value.delete(m);
	}
);
</script>
