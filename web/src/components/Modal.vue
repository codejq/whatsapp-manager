<script setup>
import { onMounted, onBeforeUnmount } from 'vue'
import Icon from './Icon.vue'

defineProps({ title: { type: String, default: '' }, wide: Boolean })
const emit = defineEmits(['close'])

const onKey = (e) => { if (e.key === 'Escape') emit('close') }
onMounted(() => window.addEventListener('keydown', onKey))
onBeforeUnmount(() => window.removeEventListener('keydown', onKey))
</script>

<template>
  <Teleport to="body">
    <div class="fixed inset-0 z-40 flex items-center justify-center bg-slate-900/50 p-4" @mousedown.self="emit('close')">
      <div class="card flex max-h-[90vh] w-full flex-col" :class="wide ? 'max-w-3xl' : 'max-w-lg'" role="dialog" aria-modal="true">
        <div class="flex items-center justify-between border-b border-slate-200 px-5 py-3 dark:border-slate-800">
          <h2 class="text-base font-bold">{{ title }}</h2>
          <button class="btn-ghost btn-sm" :aria-label="$t('common.close')" @click="emit('close')"><Icon name="x" :size="18" /></button>
        </div>
        <div class="overflow-y-auto p-5"><slot /></div>
        <div v-if="$slots.footer" class="flex justify-end gap-2 border-t border-slate-200 px-5 py-3 dark:border-slate-800"><slot name="footer" /></div>
      </div>
    </div>
  </Teleport>
</template>
