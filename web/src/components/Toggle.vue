<script setup>
defineProps({ modelValue: Boolean, disabled: Boolean, label: { type: String, default: '' } })
const emit = defineEmits(['update:modelValue'])
</script>

<template>
  <button
    type="button"
    role="switch"
    :aria-checked="modelValue"
    :aria-label="label"
    :disabled="disabled"
    class="relative inline-flex h-6 w-11 shrink-0 items-center rounded-full transition disabled:opacity-50"
    :class="modelValue ? 'bg-brand-600' : 'bg-slate-300 dark:bg-slate-600'"
    @click="emit('update:modelValue', !modelValue)"
  >
    <span
      class="inline-block h-5 w-5 rounded-full bg-white shadow transition"
      :class="modelValue ? 'translate-x-5 rtl:-translate-x-5' : 'translate-x-0.5 rtl:-translate-x-0.5'"
    />
  </button>
</template>
