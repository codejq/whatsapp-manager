<script setup>
import { ref } from 'vue'
import Icon from './Icon.vue'

// Chips is a tag input: Enter or comma adds the typed value.
const props = defineProps({
  modelValue: { type: Array, default: () => [] },
  placeholder: { type: String, default: '' },
  validate: { type: Function, default: null },
  dir: { type: String, default: 'auto' },
})
const emit = defineEmits(['update:modelValue'])
const text = ref('')
const bad = ref(false)

function add() {
  const parts = text.value.split(/[,،\n]/).map((s) => s.trim()).filter(Boolean)
  if (!parts.length) return
  const next = [...props.modelValue]
  for (const p of parts) {
    if (props.validate && !props.validate(p)) { bad.value = true; return }
    if (!next.includes(p)) next.push(p)
  }
  bad.value = false
  text.value = ''
  emit('update:modelValue', next)
}

function remove(i) {
  const next = [...props.modelValue]
  next.splice(i, 1)
  emit('update:modelValue', next)
}

function onKey(e) {
  if (e.key === 'Enter' || e.key === ',' || e.key === '،') { e.preventDefault(); add() }
  else if (e.key === 'Backspace' && !text.value && props.modelValue.length) remove(props.modelValue.length - 1)
}
</script>

<template>
  <div class="input flex flex-wrap items-center gap-1.5 !py-1.5" :class="bad && '!border-red-400'">
    <span v-for="(c, i) in modelValue" :key="c" class="badge-green !py-1" :dir="dir">
      {{ c }}
      <button type="button" class="opacity-60 hover:opacity-100" :aria-label="$t('common.remove')" @click="remove(i)"><Icon name="x" :size="12" /></button>
    </span>
    <input
      v-model="text"
      class="min-w-[10rem] flex-1 bg-transparent py-0.5 outline-none"
      :placeholder="modelValue.length ? '' : placeholder"
      :dir="dir"
      @keydown="onKey"
      @blur="add"
      @input="bad = false"
    />
  </div>
</template>
