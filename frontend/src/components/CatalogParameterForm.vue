<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import CatalogParameterField from './CatalogParameterField.vue'
import type { CatalogModel } from '../models/modelCatalog'

const props = defineProps<{ model: CatalogModel; parameters: Record<string, unknown> }>()
const emit = defineEmits<{ update: [key: string, value: unknown]; resize: [] }>()
const root = ref<HTMLElement | null>(null)
const order: Record<string, number> = { ratio: 0, resolution: 1, duration: 2, count: 3, audio: 4 }
const fields = computed(() => (props.model.parameters?.parameters ?? [])
  .filter((parameter) => parameter.role in order)
  .sort((a, b) => order[a.role]! - order[b.role]!))
let observer: ResizeObserver | undefined
onMounted(() => {
  observer = new ResizeObserver(() => emit('resize'))
  if (root.value) observer.observe(root.value)
})
onBeforeUnmount(() => observer?.disconnect())
</script>

<template>
  <div ref="root" class="catalog-parameter-form">
    <p v-if="!fields.length" class="catalog-parameters-note" role="status">该模型暂无可调整的基础参数。</p>
    <CatalogParameterField v-for="parameter in fields" :key="parameter.key" :parameter="parameter" :parameters="parameters" @update="(key, value) => emit('update', key, value)" />
  </div>
</template>

<style scoped>
.catalog-parameter-form { min-width: 0; }
.catalog-parameters-note { margin: 0; color: #a8a8a8; font-size: 12px; line-height: 1.6; }
</style>
