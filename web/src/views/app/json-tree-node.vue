<template>
	<div class="json-tree-node">
		<div class="json-node-row">
			<button v-if="object" @click="expanded = !expanded" :aria-expanded="expanded">{{ expanded ? '−' : '+' }}</button><span v-else>·</span
			><code>{{ label || '(root)' }}</code
			><small>{{ Array.isArray(value) ? `array · ${value.length}` : object ? 'object' : sample }}</small
			><button :disabled="disabled" @click="emit('select', path, Array.isArray(value))">{{ Array.isArray(value) ? '选择数组' : '选择字段' }}</button>
		</div>
		<template v-if="expanded && object"
			><p v-if="depth >= 20" class="muted">显示深度已达上限，请使用手工 JSON Pointer。</p>
			<template v-else
				><JsonTreeNode
					v-for="entry in entries.slice(0, limit)"
					:key="entry[0]"
					:label="entry[0]"
					:path="path + '/' + escapeKey(entry[0])"
					:value="entry[1]"
					:depth="depth + 1"
					:disabled="disabled"
					@select="(pointer, array) => emit('select', pointer, array)"
				/><button v-if="entries.length > limit" :disabled="limit >= 500" @click="limit += 50">
					显示更多（{{ Math.min(limit, entries.length) }}/{{ entries.length }}，最多 500）
				</button></template
			></template
		>
	</div>
</template>
<script setup lang="ts">
import { computed, ref } from 'vue';
defineOptions({ name: 'JsonTreeNode' });
const props = defineProps<{ value: unknown; path: string; depth: number; label?: string; disabled: boolean }>();
const emit = defineEmits<{ (event: 'select', pointer: string, array: boolean): void }>();
const expanded = ref(props.depth === 0),
	limit = ref(50);
const object = computed(() => props.value !== null && typeof props.value === 'object');
const entries = computed(() => (object.value ? Object.entries(props.value as object) : []));
const sample = computed(() => JSON.stringify(props.value)?.slice(0, 140));
function escapeKey(key: string) {
	return key.replaceAll('~', '~0').replaceAll('/', '~1');
}
</script>
