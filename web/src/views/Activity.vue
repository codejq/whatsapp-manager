<script setup>
import { ref, onMounted, onBeforeUnmount, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { get, post } from '../api'
import { errText } from '../i18n'
import { fmtTime, notify } from '../util'
import Icon from '../components/Icon.vue'

const { t, te } = useI18n()
const route = useRoute()
const items = ref([])
const only = ref(route.query.only || '')
const q = ref('')
const loading = ref(false)
const more = ref(true)
const open = ref({})
let timer = null

async function load(append = false) {
  loading.value = true
  try {
    const before = append && items.value.length ? items.value[items.value.length - 1].id : 0
    const params = new URLSearchParams({ limit: '50', only: only.value, q: q.value, before: String(before) })
    const list = await get('/api/activity?' + params)
    items.value = append ? [...items.value, ...list] : list
    more.value = list.length === 50
  } catch (e) {
    notify(errText(e.key), 'error')
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  load()
  // Live refresh while looking at the newest entries.
  timer = setInterval(() => { if (items.value.length <= 50 && !q.value) load() }, 5000)
})
onBeforeUnmount(() => clearInterval(timer))
let qTimer = null
watch(q, () => { clearTimeout(qTimer); qTimer = setTimeout(() => load(), 300) })
watch(only, () => load())

const stClass = {
  pending: 'badge-yellow', sending: 'badge-blue', sent: 'badge-green', failed: 'badge-red',
  uncertain: 'badge-red', waiting_email: 'badge-blue', cancelled: 'badge-gray',
}

function noteText(key) {
  if (!key) return ''
  if (te(key)) return t(key)
  return key
}

async function retry(d) {
  try { await post(`/api/outbox/${d.id}/retry`); load() } catch (e) { notify(errText(e.key), 'error') }
}
async function cancel(d) {
  try { await post(`/api/outbox/${d.id}/cancel`); load() } catch (e) { notify(errText(e.key), 'error') }
}
</script>

<template>
  <div class="mx-auto max-w-5xl space-y-5 p-6">
    <div>
      <h1 class="text-2xl font-bold">{{ $t('activity.title') }}</h1>
      <p class="text-sm text-slate-500">{{ $t('activity.subtitle') }}</p>
    </div>

    <div class="flex flex-wrap items-center gap-3">
      <div class="flex rounded-xl bg-slate-100 p-1 dark:bg-slate-800">
        <button
          v-for="f in [['', 'activity.all'], ['matched', 'activity.matched'], ['failed', 'activity.failed']]" :key="f[0]"
          class="rounded-lg px-3 py-1.5 text-sm font-medium" :class="only === f[0] ? 'bg-white shadow dark:bg-slate-700' : 'text-slate-500'" @click="only = f[0]"
        >
          {{ $t(f[1]) }}
        </button>
      </div>
      <div class="relative ms-auto w-64">
        <Icon name="search" :size="16" class="absolute start-3 top-2.5 text-slate-400" />
        <input v-model="q" class="input !ps-9" :placeholder="$t('common.search')" />
      </div>
    </div>

    <div class="card divide-y divide-slate-100 dark:divide-slate-800">
      <p v-if="!items.length && !loading" class="p-8 text-center text-sm text-slate-500">{{ $t('activity.empty') }}</p>
      <div v-for="e in items" :key="e.id" class="px-5 py-3">
        <button class="flex w-full items-start gap-3 text-start" @click="open[e.id] = !open[e.id]">
          <span class="mt-1 flex h-8 w-8 shrink-0 items-center justify-center rounded-full" :class="e.deliveries.length ? 'bg-brand-100 text-brand-700 dark:bg-brand-900 dark:text-brand-200' : 'bg-slate-100 text-slate-400 dark:bg-slate-800'">
            <Icon :name="e.deliveries.length ? 'forward' : 'chat'" :size="16" />
          </span>
          <span class="min-w-0 flex-1">
            <span class="flex items-center gap-2 text-sm">
              <b class="truncate">{{ e.chat_name }}</b>
              <span v-if="e.sender_name && e.sender_name !== e.chat_name" class="truncate text-slate-500">· {{ e.sender_name }}</span>
              <span class="ms-auto shrink-0 text-xs text-slate-400">{{ fmtTime(e.received_at) }}</span>
            </span>
            <span class="block truncate text-sm text-slate-600 dark:text-slate-300" dir="auto">{{ e.preview || $t('types.' + (e.msg_type || 'other')) }}</span>
            <span class="mt-1 flex flex-wrap gap-1.5">
              <template v-if="e.deliveries.length">
                <span v-for="d in e.deliveries" :key="d.id" :class="stClass[d.status]">
                  <Icon :name="d.channel === 'email' ? 'mail' : 'chat'" :size="11" />{{ d.destination_label }} · {{ $t('activity.st.' + d.status) }}
                </span>
              </template>
              <span v-else class="badge-gray">{{ $t('activity.not_matched') }}<template v-if="e.note"> · {{ noteText(e.note) }}</template></span>
            </span>
          </span>
        </button>

        <div v-if="open[e.id] && e.deliveries.length" class="ms-11 mt-3 space-y-2">
          <div v-for="d in e.deliveries" :key="d.id" class="rounded-xl border border-slate-200 p-3 text-sm dark:border-slate-800">
            <div class="flex flex-wrap items-center gap-2">
              <span :class="stClass[d.status]">{{ $t('activity.st.' + d.status) }}</span>
              <span>{{ $t('activity.to') }}: <b dir="auto">{{ d.destination_label }}</b></span>
              <span class="text-slate-500">· {{ $t('activity.rule') }}: {{ d.rule_name }}</span>
              <span class="text-xs text-slate-400">{{ $t('activity.attempts', { n: d.attempts }) }}</span>
              <span class="ms-auto flex gap-1">
                <button v-if="['failed', 'uncertain', 'cancelled', 'waiting_email'].includes(d.status)" class="btn-secondary btn-sm" @click="retry(d)"><Icon name="refresh" :size="12" />{{ $t('activity.send_again') }}</button>
                <button v-if="['pending', 'failed', 'uncertain', 'waiting_email'].includes(d.status)" class="btn-ghost btn-sm" @click="cancel(d)">{{ $t('activity.cancel_send') }}</button>
              </span>
            </div>
            <p v-if="d.status === 'uncertain'" class="mt-2 text-xs text-amber-700 dark:text-amber-300">{{ $t('activity.uncertain_help') }}</p>
            <p v-else-if="d.last_error" class="ltr mt-2 break-all text-xs text-red-600" dir="auto">{{ noteText(d.last_error.split(': ')[0]) }}<template v-if="d.last_error.includes(': ')"> — {{ d.last_error.split(': ').slice(1).join(': ') }}</template></p>
          </div>
        </div>
      </div>
    </div>
    <div v-if="more && items.length" class="text-center">
      <button class="btn-secondary" :disabled="loading" @click="load(true)">{{ $t('common.more') }}</button>
    </div>
  </div>
</template>
