<script setup>
import { ref, onMounted, onBeforeUnmount, computed } from 'vue'
import { get } from '../api'
import { app } from '../state'
import { fmtTime, phoneOf } from '../util'
import Icon from '../components/Icon.vue'

const recent = ref([])
let timer = null

async function load() {
  try { recent.value = await get('/api/activity?limit=6') } catch { /* shown by banner */ }
}
onMounted(() => { load(); timer = setInterval(load, 5000) })
onBeforeUnmount(() => clearInterval(timer))

const tiles = computed(() => [
  { key: 'received', icon: 'chat', value: app.stats.received || 0, cls: 'text-slate-700 dark:text-slate-200' },
  { key: 'matched', icon: 'filter', value: app.stats.matched || 0, cls: 'text-sky-600' },
  { key: 'sent', icon: 'forward', value: app.stats.sent || 0, cls: 'text-brand-600' },
  { key: 'failed', icon: 'alert', value: app.stats.failed || 0, cls: (app.stats.failed ? 'text-red-600' : 'text-slate-400') },
])

const emailBadge = computed(() => ({
  ok: ['badge-green', 'dashboard.email_ok'],
  untested: ['badge-yellow', 'dashboard.email_untested'],
  auth_failed: ['badge-red', 'dashboard.email_auth_failed'],
  not_configured: ['badge-gray', 'dashboard.email_not_configured'],
}[app.smtp.state] || ['badge-gray', 'dashboard.email_not_configured']))

function entryStatus(e) {
  if (!e.deliveries.length) return ['badge-gray', 'activity.not_matched']
  const st = e.deliveries.map((d) => d.status)
  if (st.some((s) => s === 'failed' || s === 'uncertain')) return ['badge-red', 'activity.st.' + st.find((s) => s === 'failed' || s === 'uncertain')]
  if (st.every((s) => s === 'sent')) return ['badge-green', 'activity.st.sent']
  return ['badge-yellow', 'activity.st.pending']
}
</script>

<template>
  <div class="mx-auto max-w-6xl space-y-6 p-6">
    <h1 class="text-2xl font-bold">{{ $t('dashboard.title') }}</h1>

    <!-- Connection + email + rules -->
    <div class="grid gap-4 md:grid-cols-3">
      <router-link to="/settings/whatsapp" class="card flex items-center gap-4 p-5 transition hover:border-brand-300">
        <span class="flex h-12 w-12 items-center justify-center rounded-2xl" :class="app.wa.wa === 'connected' ? 'bg-brand-100 text-brand-700 dark:bg-brand-900 dark:text-brand-200' : 'bg-amber-100 text-amber-700 dark:bg-amber-900/40 dark:text-amber-200'">
          <Icon name="chat" :size="24" />
        </span>
        <div class="min-w-0">
          <div class="text-sm text-slate-500">{{ $t('wa.title') }}</div>
          <div class="font-bold">{{ $t('status.' + (app.wa.phase === 'error' ? 'error' : app.wa.wa)) }}</div>
          <div v-if="app.wa.jid" class="ltr truncate text-xs text-slate-500">{{ phoneOf(app.wa.jid) }}</div>
        </div>
      </router-link>
      <router-link to="/settings/email" class="card flex items-center gap-4 p-5 transition hover:border-brand-300">
        <span class="flex h-12 w-12 items-center justify-center rounded-2xl bg-sky-100 text-sky-700 dark:bg-sky-900/40 dark:text-sky-200"><Icon name="mail" :size="24" /></span>
        <div>
          <div class="text-sm text-slate-500">{{ $t('dashboard.email') }}</div>
          <span :class="emailBadge[0]">{{ $t(emailBadge[1]) }}</span>
        </div>
      </router-link>
      <router-link to="/rules" class="card flex items-center gap-4 p-5 transition hover:border-brand-300">
        <span class="flex h-12 w-12 items-center justify-center rounded-2xl bg-violet-100 text-violet-700 dark:bg-violet-900/40 dark:text-violet-200"><Icon name="rules" :size="24" /></span>
        <div>
          <div class="text-sm text-slate-500">{{ $t('dashboard.rules') }}</div>
          <div class="font-bold">{{ $t('dashboard.rules_of', { enabled: app.rules.enabled, total: app.rules.total }) }}</div>
        </div>
      </router-link>
    </div>

    <!-- Today -->
    <div>
      <h2 class="mb-3 text-sm font-semibold uppercase tracking-wide text-slate-500">{{ $t('dashboard.today') }}</h2>
      <div class="grid grid-cols-2 gap-4 md:grid-cols-4">
        <div v-for="tl in tiles" :key="tl.key" class="card p-5">
          <div class="flex items-center justify-between text-sm text-slate-500">{{ $t('dashboard.' + tl.key) }}<Icon :name="tl.icon" :size="18" :class="tl.cls" /></div>
          <div class="mt-2 text-3xl font-bold" :class="tl.cls">{{ tl.value }}</div>
        </div>
      </div>
      <div v-if="app.stats.pending || app.stats.uncertain || app.stats.waiting_email" class="mt-3 flex flex-wrap gap-2">
        <router-link v-if="app.stats.pending" to="/activity" class="badge-yellow !py-1.5">{{ $t('dashboard.queue') }}: {{ app.stats.pending }}</router-link>
        <router-link v-if="app.stats.uncertain" to="/activity?only=failed" class="badge-red !py-1.5">{{ $t('dashboard.uncertain') }}: {{ app.stats.uncertain }}</router-link>
        <router-link v-if="app.stats.waiting_email" to="/settings/email" class="badge-blue !py-1.5">{{ $t('dashboard.waiting_email') }}: {{ app.stats.waiting_email }}</router-link>
      </div>
    </div>

    <!-- No rules -->
    <div v-if="app.loaded && app.rules.total === 0" class="card flex flex-col items-center gap-3 p-10 text-center">
      <Icon name="rules" :size="40" class="text-brand-500" />
      <p class="font-semibold">{{ $t('dashboard.no_rules') }}</p>
      <router-link to="/rules?templates=1" class="btn-primary"><Icon name="plus" :size="16" />{{ $t('dashboard.create_first') }}</router-link>
    </div>

    <!-- Recent -->
    <div class="card">
      <div class="flex items-center justify-between border-b border-slate-200 px-5 py-3 dark:border-slate-800">
        <h2 class="font-bold">{{ $t('dashboard.recent') }}</h2>
        <router-link to="/activity" class="text-sm font-semibold text-brand-700 dark:text-brand-300">{{ $t('dashboard.view_all') }}</router-link>
      </div>
      <p v-if="!recent.length" class="p-6 text-center text-sm text-slate-500">{{ $t('activity.empty') }}</p>
      <div v-for="e in recent" :key="e.id" class="flex items-center gap-3 border-b border-slate-100 px-5 py-3 last:border-0 dark:border-slate-800">
        <div class="min-w-0 flex-1">
          <div class="flex items-center gap-2 text-sm">
            <b class="truncate">{{ e.chat_name }}</b>
            <span v-if="e.sender_name && e.sender_name !== e.chat_name" class="truncate text-slate-500">· {{ e.sender_name }}</span>
          </div>
          <div class="truncate text-sm text-slate-500" dir="auto">{{ e.preview || $t('types.' + (e.msg_type || 'other')) }}</div>
        </div>
        <span :class="entryStatus(e)[0]">{{ $t(entryStatus(e)[1]) }}</span>
        <span class="w-16 shrink-0 text-end text-xs text-slate-400">{{ fmtTime(e.received_at) }}</span>
      </div>
    </div>
  </div>
</template>
