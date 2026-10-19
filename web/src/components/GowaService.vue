<script setup>
import { ref, computed } from 'vue'
import { post, put } from '../api'
import { app, refreshState } from '../state'
import { errText } from '../i18n'
import { notify, fmtTime } from '../util'
import { openExternal } from '../branding'
import Icon from './Icon.vue'
import Toggle from './Toggle.vue'

// GowaService shows the WhatsApp service (GoWA) version and offers updates.
const checking = ref(false)
const g = computed(() => app.gowa || {})
const busy = computed(() => ['downloading', 'installing'].includes(g.value.state))

async function check() {
  checking.value = true
  try {
    const r = await post('/api/gowa/check')
    if (r.error) notify(errText(r.error), 'error')
    await refreshState()
  } catch (e) {
    notify(errText(e.key), 'error')
  } finally {
    checking.value = false
  }
}

async function install() {
  try {
    await post('/api/gowa/install')
    await refreshState()
  } catch (e) {
    notify(errText(e.key), 'error')
  }
}

async function reset() {
  try {
    await post('/api/gowa/reset')
    await refreshState()
  } catch (e) {
    notify(errText(e.key), 'error')
  }
}

async function setAuto(on) {
  await put('/api/settings', { ...app.general, gowa_update_check: on })
  refreshState()
}
</script>

<template>
  <div class="rounded-xl border border-slate-200 p-4 dark:border-slate-800">
    <div class="mb-3 flex items-center gap-2 font-semibold"><Icon name="zap" :size="16" class="text-brand-600" />{{ $t('gowa.title') }}</div>

    <p v-if="!g.allowed" class="text-sm text-slate-500">{{ $t('gowa.store') }}</p>
    <template v-else>
      <dl class="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1.5 text-sm">
        <dt class="text-slate-500">{{ $t('gowa.installed') }}</dt>
        <dd class="ltr font-medium" dir="ltr">
          {{ g.current || '…' }}
          <span v-if="g.bundled" class="text-xs font-normal text-slate-400">· {{ $t('gowa.bundled') }}</span>
        </dd>
        <dt class="text-slate-500">{{ $t('gowa.latest') }}</dt>
        <dd class="ltr font-medium" dir="ltr">
          {{ g.latest || '—' }}
          <button v-if="g.page_url" class="ms-2 text-xs font-normal text-brand-700 hover:underline dark:text-brand-300" @click="openExternal(g.page_url)">{{ $t('gowa.notes') }}</button>
        </dd>
      </dl>

      <!-- Progress -->
      <div v-if="busy" class="mt-4">
        <div class="mb-1 text-sm">{{ g.state === 'downloading' ? $t('gowa.downloading', { p: g.progress }) : $t('gowa.installing') }}</div>
        <div class="h-2 overflow-hidden rounded-full bg-slate-200 dark:bg-slate-700">
          <div class="h-full rounded-full bg-brand-500 transition-all" :class="g.state === 'installing' && 'animate-pulse'" :style="{ width: (g.state === 'installing' ? 100 : g.progress) + '%' }" />
        </div>
      </div>

      <!-- Messages -->
      <p v-else-if="g.state === 'done'" class="mt-3 flex items-center gap-2 text-sm text-brand-700 dark:text-brand-300"><Icon name="check" :size="16" />{{ $t('gowa.done', { v: g.current }) }}</p>
      <div v-else-if="g.state === 'failed'" class="mt-3 rounded-lg bg-red-50 p-3 text-sm text-red-800 dark:bg-red-900/30 dark:text-red-200">
        {{ errText(g.error) }}
        <div v-if="g.detail" class="ltr mt-1 break-all text-xs opacity-80" dir="ltr">{{ g.detail }}</div>
      </div>
      <p v-if="!busy && g.available && !g.compatible" class="mt-3 rounded-lg bg-amber-50 p-3 text-sm text-amber-900 dark:bg-amber-900/30 dark:text-amber-100">{{ $t('gowa.major', { v: g.latest }) }}</p>
      <p v-else-if="!busy && g.latest && !g.available && g.state !== 'failed'" class="mt-3 flex items-center gap-2 text-sm text-slate-500"><Icon name="check" :size="16" />{{ $t('gowa.up_to_date') }}</p>

      <div class="mt-4 flex flex-wrap items-center gap-2">
        <button v-if="g.available && g.compatible" class="btn-primary btn-sm" :disabled="busy" @click="install"><Icon name="download" :size="14" />{{ $t('gowa.install', { v: g.latest }) }}</button>
        <button class="btn-secondary btn-sm" :disabled="checking || busy" @click="check"><Icon name="refresh" :size="14" :class="checking && 'animate-spin'" />{{ $t('gowa.check') }}</button>
        <button v-if="!g.bundled && g.current" class="btn-ghost btn-sm" :disabled="busy" @click="reset">{{ $t('gowa.reset') }}</button>
        <span v-if="g.checked_at && !g.checked_at.startsWith('0001')" class="ms-auto text-xs text-slate-400">{{ $t('gowa.checked', { t: fmtTime(Date.parse(g.checked_at)) }) }}</span>
      </div>
      <p class="hint">{{ $t('gowa.note') }}</p>

      <div class="mt-4 flex items-center gap-3 border-t border-slate-200 pt-3 dark:border-slate-800">
        <Toggle :model-value="app.general.gowa_update_check" :label="$t('gowa.auto')" @update:model-value="setAuto" />
        <span class="text-sm">{{ $t('gowa.auto') }}</span>
      </div>
    </template>
  </div>
</template>
