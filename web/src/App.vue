<script setup>
import { computed, watch, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { app, refreshState } from './state'
import { get, put, post, hasToken } from './api'
import { toasts } from './util'
import Icon from './components/Icon.vue'
import SidebarAd from './components/SidebarAd.vue'
import logo from './logo.png'

const route = useRoute()
const router = useRouter()
const update = ref(null)

const nav = [
  { to: '/', icon: 'home', label: 'nav.dashboard' },
  { to: '/rules', icon: 'rules', label: 'nav.rules' },
  { to: '/activity', icon: 'activity', label: 'nav.activity' },
  { to: '/settings', icon: 'settings', label: 'nav.settings' },
]

const isActive = (to) => (to === '/' ? route.path === '/' : route.path.startsWith(to))

// First run: send to the welcome wizard once state is known.
watch(() => app.loaded, (ok) => {
  if (ok && !app.general.first_run_done && route.path !== '/welcome') router.replace('/welcome')
  if (ok) checkUpdate()
}, { immediate: true })

async function checkUpdate() {
  try {
    const r = await get('/api/update')
    if (r.available) update.value = r
  } catch { /* offline: ignore */ }
}

const statusInfo = computed(() => {
  if (app.wa.phase === 'error') return { cls: 'bg-red-500', text: 'status.error' }
  switch (app.wa.wa) {
    case 'connected': return { cls: 'bg-brand-500', text: 'status.connected' }
    case 'disconnected': return { cls: 'bg-amber-500 animate-pulse', text: 'status.disconnected' }
    case 'logged_out': return { cls: 'bg-red-500', text: 'status.logged_out' }
    default: return { cls: 'bg-slate-400 animate-pulse', text: 'status.unknown' }
  }
})

async function resume() {
  await put('/api/settings', { ...app.general, paused: false })
  refreshState()
}

async function dismissNotices() {
  await post('/api/notices/dismiss')
  refreshState()
}

async function openUpdate() {
  if (update.value?.url) await post('/api/open-url', { url: update.value.url })
}

const showChrome = computed(() => route.path !== '/welcome')
</script>

<template>
  <div class="flex h-full">
    <!-- Sidebar -->
    <aside v-if="showChrome" class="flex w-60 shrink-0 flex-col overflow-y-auto border-e border-slate-200 bg-white dark:border-slate-800 dark:bg-slate-900">
      <div class="flex items-center gap-3 px-5 py-5">
        <img :src="logo" alt="" class="h-9 w-9 rounded-xl" />
        <div class="min-w-0">
          <div class="truncate font-bold leading-tight">WhatsApp Manager</div>
          <div class="text-xs text-slate-500 ltr">v{{ app.version }}</div>
        </div>
      </div>
      <nav class="flex-1 space-y-1 px-3">
        <router-link
          v-for="n in nav"
          :key="n.to"
          :to="n.to"
          class="flex items-center gap-3 rounded-xl px-3 py-2.5 text-sm font-medium transition"
          :class="isActive(n.to) ? 'bg-brand-50 text-brand-700 dark:bg-brand-900/40 dark:text-brand-200' : 'text-slate-600 hover:bg-slate-100 dark:text-slate-300 dark:hover:bg-slate-800'"
        >
          <Icon :name="n.icon" :size="18" />{{ $t(n.label) }}
        </router-link>
      </nav>
      <SidebarAd class="mt-4" />
      <router-link to="/settings/whatsapp" class="m-3 mt-0 flex items-center gap-2 rounded-xl bg-slate-100 px-3 py-2.5 text-xs font-medium dark:bg-slate-800">
        <span class="h-2.5 w-2.5 rounded-full" :class="statusInfo.cls" />
        <span>{{ $t(statusInfo.text) }}</span>
        <span v-if="app.general.paused" class="badge-yellow ms-auto">{{ $t('status.paused') }}</span>
      </router-link>
    </aside>

    <!-- Main -->
    <main class="min-w-0 flex-1 overflow-y-auto">
      <div v-if="showChrome" class="space-y-2 px-6 pt-4 empty:hidden">
        <div v-if="!hasToken() || app.error === 'errors.unauthorized'" class="flex items-center gap-3 rounded-xl bg-red-50 px-4 py-3 text-sm text-red-800 dark:bg-red-900/30 dark:text-red-200">
          <Icon name="alert" :size="18" />{{ $t('errors.unauthorized') }}
        </div>
        <div v-else-if="app.error === 'errors.app_unreachable'" class="flex items-center gap-3 rounded-xl bg-red-50 px-4 py-3 text-sm text-red-800 dark:bg-red-900/30 dark:text-red-200">
          <Icon name="alert" :size="18" />{{ $t('banner.app_unreachable') }}
        </div>
        <div v-if="app.general.paused" class="flex items-center gap-3 rounded-xl bg-amber-50 px-4 py-3 text-sm text-amber-900 dark:bg-amber-900/30 dark:text-amber-100">
          <Icon name="pause" :size="18" />{{ $t('banner.paused') }}
          <button class="btn-secondary btn-sm ms-auto" @click="resume"><Icon name="play" :size="14" />{{ $t('banner.resume') }}</button>
        </div>
        <div v-if="app.loaded && app.wa.phase === 'running' && app.wa.wa === 'logged_out' && route.path !== '/settings/whatsapp'" class="flex items-center gap-3 rounded-xl bg-sky-50 px-4 py-3 text-sm text-sky-900 dark:bg-sky-900/30 dark:text-sky-100">
          <Icon name="qr" :size="18" />{{ $t('banner.not_linked') }}
          <router-link to="/settings/whatsapp" class="btn-primary btn-sm ms-auto">{{ $t('banner.link_now') }}</router-link>
        </div>
        <div v-if="app.wa.wa === 'disconnected'" class="flex items-center gap-3 rounded-xl bg-amber-50 px-4 py-3 text-sm text-amber-900 dark:bg-amber-900/30 dark:text-amber-100">
          <Icon name="refresh" :size="18" class="animate-spin" />{{ $t('banner.disconnected') }}
        </div>
        <div v-for="n in app.notices" :key="n" class="flex items-center gap-3 rounded-xl bg-slate-100 px-4 py-3 text-sm dark:bg-slate-800">
          <Icon name="info" :size="18" />{{ $t(n) }}
          <button class="btn-ghost btn-sm ms-auto" @click="dismissNotices"><Icon name="x" :size="14" /></button>
        </div>
        <div v-if="update" class="flex items-center gap-3 rounded-xl bg-brand-50 px-4 py-3 text-sm text-brand-900 dark:bg-brand-900/30 dark:text-brand-100">
          <Icon name="download" :size="18" />{{ $t('banner.update', { v: update.latest }) }}
          <button class="btn-primary btn-sm ms-auto" @click="openUpdate">{{ $t('banner.download') }}</button>
          <button class="btn-ghost btn-sm" @click="update = null"><Icon name="x" :size="14" /></button>
        </div>
      </div>
      <router-view />
    </main>

    <!-- Toasts -->
    <div class="pointer-events-none fixed bottom-4 start-1/2 z-50 flex -translate-x-1/2 flex-col items-center gap-2 rtl:translate-x-1/2">
      <div
        v-for="t in toasts"
        :key="t.id"
        class="pointer-events-auto rounded-xl px-4 py-2.5 text-sm font-medium text-white shadow-lg"
        :class="t.kind === 'error' ? 'bg-red-600' : 'bg-slate-900 dark:bg-slate-700'"
      >
        {{ t.text }}
      </div>
    </div>
  </div>
</template>
