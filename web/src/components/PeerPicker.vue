<script setup>
import { ref, computed, watch } from 'vue'
import { get, post } from '../api'
import { app, waConnected } from '../state'
import { errText } from '../i18n'
import { peerLabel, phoneOf, isGroup } from '../util'
import Modal from './Modal.vue'
import Icon from './Icon.vue'

// PeerPicker selects groups/contacts from WhatsApp or a typed phone number.
const props = defineProps({
  modelValue: { type: [Array, Object], default: null },
  multiple: { type: Boolean, default: true },
  kinds: { type: Array, default: () => ['group', 'contact'] },
  title: { type: String, default: '' },
  buttonText: { type: String, default: '' },
})
const emit = defineEmits(['update:modelValue'])

const open = ref(false)
const tab = ref(props.kinds[0])
const chats = ref([])
const q = ref('')
const loading = ref(false)
const loadErr = ref('')
const phone = ref('')
const phoneErr = ref('')
const checking = ref(false)

const selected = computed(() => {
  if (props.multiple) return props.modelValue || []
  return props.modelValue ? [props.modelValue] : []
})

async function load(refresh = false) {
  loading.value = true
  loadErr.value = ''
  try {
    const r = await get('/api/wa/chats' + (refresh ? '?refresh=1' : ''))
    chats.value = r.chats || []
    if (r.refresh_error) loadErr.value = errText(r.refresh_error)
    if (!refresh && chats.value.length === 0 && waConnected()) return load(true)
  } catch (e) {
    loadErr.value = errText(e.key)
  } finally {
    loading.value = false
  }
}

watch(open, (v) => { if (v) { q.value = ''; load() } })

const list = computed(() => {
  const term = q.value.trim().toLowerCase()
  return chats.value
    .filter((c) => c.kind === tab.value)
    .filter((c) => !term || (c.name || '').toLowerCase().includes(term) || c.jid.includes(term.replace(/\D/g, '') || '\u0000'))
})

const isSel = (jid) => selected.value.some((p) => p.jid === jid)

function toggle(c) {
  const peer = { jid: c.jid, name: c.name || phoneOf(c.jid) }
  if (!props.multiple) {
    emit('update:modelValue', peer)
    open.value = false
    return
  }
  const next = isSel(c.jid) ? selected.value.filter((p) => p.jid !== c.jid) : [...selected.value, peer]
  emit('update:modelValue', next)
}

function removeSel(jid) {
  if (!props.multiple) return emit('update:modelValue', null)
  emit('update:modelValue', selected.value.filter((p) => p.jid !== jid))
}

async function addPhone() {
  phoneErr.value = ''
  if (!phone.value.trim()) return
  checking.value = true
  try {
    const r = await post('/api/wa/check', { phone: phone.value })
    if (!r.on_whatsapp) { phoneErr.value = 'picker.not_on_wa'; return }
    toggle({ jid: r.jid, name: '+' + r.phone })
    phone.value = ''
  } catch (e) {
    phoneErr.value = e.key
  } finally {
    checking.value = false
  }
}

const tabs = computed(() => props.kinds.map((k) => ({ k, label: k === 'group' ? 'picker.groups' : 'picker.contacts' })))
</script>

<template>
  <div>
    <div class="flex flex-wrap items-center gap-2">
      <span v-for="p in selected" :key="p.jid" class="badge-green !py-1">
        <Icon :name="isGroup(p.jid) ? 'users' : 'user'" :size="12" />
        <span>{{ peerLabel(p) }}</span>
        <button type="button" class="opacity-60 hover:opacity-100" :aria-label="$t('common.remove')" @click="removeSel(p.jid)"><Icon name="x" :size="12" /></button>
      </span>
      <button type="button" class="btn-secondary btn-sm" @click="open = true">
        <Icon :name="multiple ? 'plus' : 'search'" :size="14" />
        {{ buttonText || $t('picker.pick') }}
      </button>
    </div>

    <Modal v-if="open" :title="title || $t('picker.pick')" @close="open = false">
      <div v-if="!waConnected() && app.wa.wa !== 'unknown'" class="mb-3 rounded-xl bg-amber-50 p-3 text-sm text-amber-800 dark:bg-amber-900/30 dark:text-amber-200">
        {{ $t('picker.not_connected') }}
      </div>
      <div class="mb-3 flex items-center gap-2">
        <div v-if="tabs.length > 1" class="flex rounded-xl bg-slate-100 p-1 dark:bg-slate-800">
          <button v-for="tb in tabs" :key="tb.k" type="button" class="rounded-lg px-3 py-1 text-sm font-medium" :class="tab === tb.k ? 'bg-white shadow dark:bg-slate-700' : 'text-slate-500'" @click="tab = tb.k">{{ $t(tb.label) }}</button>
        </div>
        <button type="button" class="btn-ghost btn-sm ms-auto" :disabled="loading" :title="$t('picker.refresh')" @click="load(true)">
          <Icon name="refresh" :size="14" :class="loading && 'animate-spin'" />
        </button>
      </div>
      <input v-model="q" class="input mb-2" :placeholder="$t('picker.search')" />
      <p v-if="loadErr" class="mb-2 text-xs text-red-600">{{ loadErr }}</p>
      <div class="max-h-72 overflow-y-auto rounded-xl border border-slate-200 dark:border-slate-800">
        <p v-if="loading && !chats.length" class="p-4 text-center text-sm text-slate-500">{{ $t('common.loading') }}</p>
        <p v-else-if="!list.length" class="p-4 text-center text-sm text-slate-500">{{ $t('picker.empty') }}</p>
        <button
          v-for="c in list"
          :key="c.jid"
          type="button"
          class="flex w-full items-center gap-3 border-b border-slate-100 px-3 py-2 text-start last:border-0 hover:bg-slate-50 dark:border-slate-800 dark:hover:bg-slate-800"
          @click="toggle(c)"
        >
          <span class="flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-brand-100 text-brand-700 dark:bg-brand-900 dark:text-brand-200">
            <Icon :name="c.kind === 'group' ? 'users' : 'user'" :size="16" />
          </span>
          <span class="min-w-0 flex-1">
            <span class="block truncate text-sm font-medium">{{ c.name || phoneOf(c.jid) }}</span>
            <span v-if="c.kind === 'contact'" class="ltr block text-xs text-slate-500">{{ phoneOf(c.jid) }}</span>
          </span>
          <Icon v-if="isSel(c.jid)" name="check" :size="18" class="text-brand-600" />
        </button>
      </div>
      <p v-if="tab === 'group'" class="hint">{{ $t('picker.limit_note') }}</p>

      <div v-if="kinds.includes('contact')" class="mt-4">
        <label class="label">{{ $t('picker.add_phone') }}</label>
        <div class="flex gap-2">
          <input v-model="phone" class="input ltr" dir="ltr" inputmode="tel" :placeholder="$t('picker.phone_placeholder')" @keydown.enter.prevent="addPhone" />
          <button type="button" class="btn-secondary" :disabled="checking" @click="addPhone">{{ checking ? $t('picker.checking') : $t('common.add') }}</button>
        </div>
        <p v-if="phoneErr" class="mt-1 text-xs text-red-600">{{ phoneErr === 'picker.not_on_wa' ? $t('picker.not_on_wa') : errText(phoneErr) }}</p>
      </div>

      <template v-if="multiple" #footer>
        <span class="me-auto self-center text-sm text-slate-500">{{ $t('picker.selected', { n: selected.length }) }}</span>
        <button class="btn-primary" @click="open = false">{{ $t('common.done') }}</button>
      </template>
    </Modal>
  </div>
</template>
