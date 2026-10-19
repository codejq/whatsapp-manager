<script setup>
import { ref, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { get, post, del } from '../api'
import { refreshState } from '../state'
import { errText } from '../i18n'
import { ruleSummary, notify } from '../util'
import Icon from '../components/Icon.vue'
import Toggle from '../components/Toggle.vue'
import Modal from '../components/Modal.vue'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const rules = ref([])
const loading = ref(true)
const showTemplates = ref(route.query.templates === '1')

async function load() {
  try { rules.value = await get('/api/rules') } catch (e) { notify(errText(e.key), 'error') }
  loading.value = false
}
onMounted(load)

async function toggle(r, on) {
  try {
    await post(`/api/rules/${r.id}/enabled`, { enabled: on })
    r.enabled = on
    refreshState()
  } catch (e) {
    notify(errText(e.key), 'error')
  }
}

async function remove(r) {
  if (!confirm(t('common.confirm_delete'))) return
  try {
    await del(`/api/rules/${r.id}`)
    notify(t('rules.deleted'))
    load()
    refreshState()
  } catch (e) {
    notify(errText(e.key), 'error')
  }
}

function duplicate(r) {
  router.push({ path: '/rules/new', query: { from: r.id } })
}

const templates = [
  { id: 'links_email', icon: 'link' },
  { id: 'contact_wa', icon: 'user' },
  { id: 'keyword_group', icon: 'users' },
  { id: 'blank', icon: 'plus' },
]
function pick(id) {
  showTemplates.value = false
  router.push({ path: '/rules/new', query: id === 'blank' ? {} : { template: id } })
}
</script>

<template>
  <div class="mx-auto max-w-5xl space-y-6 p-6">
    <div class="flex flex-wrap items-center gap-3">
      <div>
        <h1 class="text-2xl font-bold">{{ $t('rules.title') }}</h1>
        <p class="text-sm text-slate-500">{{ $t('rules.subtitle') }}</p>
      </div>
      <button class="btn-primary ms-auto" @click="showTemplates = true"><Icon name="plus" :size="16" />{{ $t('rules.new') }}</button>
    </div>

    <p v-if="loading" class="text-sm text-slate-500">{{ $t('common.loading') }}</p>

    <div v-else-if="!rules.length" class="card flex flex-col items-center gap-3 p-10 text-center">
      <Icon name="rules" :size="40" class="text-brand-500" />
      <h2 class="text-lg font-bold">{{ $t('rules.empty') }}</h2>
      <p class="max-w-md text-sm text-slate-500">{{ $t('rules.empty_text') }}</p>
      <button class="btn-primary" @click="showTemplates = true"><Icon name="plus" :size="16" />{{ $t('rules.new') }}</button>
    </div>

    <div v-for="r in rules" :key="r.id" class="card p-5" :class="!r.enabled && 'opacity-70'">
      <div class="flex items-start gap-4">
        <Toggle :model-value="r.enabled" :label="r.name" @update:model-value="toggle(r, $event)" />
        <div class="min-w-0 flex-1">
          <div class="flex items-center gap-2">
            <h3 class="truncate font-bold">{{ r.name }}</h3>
            <span :class="r.enabled ? 'badge-green' : 'badge-gray'">{{ r.enabled ? $t('rules.enabled') : $t('rules.disabled') }}</span>
          </div>
          <p class="mt-1 text-sm leading-relaxed text-slate-600 dark:text-slate-300">{{ ruleSummary(r) }}</p>
        </div>
        <div class="flex shrink-0 gap-1">
          <router-link :to="'/rules/' + r.id" class="btn-ghost btn-sm" :title="$t('common.edit')"><Icon name="edit" :size="16" /></router-link>
          <button class="btn-ghost btn-sm" :title="$t('common.duplicate')" @click="duplicate(r)"><Icon name="copy" :size="16" /></button>
          <button class="btn-ghost btn-sm text-red-600" :title="$t('common.delete')" @click="remove(r)"><Icon name="trash" :size="16" /></button>
        </div>
      </div>
    </div>

    <Modal v-if="showTemplates" :title="$t('rules.templates')" wide @close="showTemplates = false">
      <div class="grid gap-3 sm:grid-cols-2">
        <button v-for="tp in templates" :key="tp.id" class="choice flex gap-3 !p-4" @click="pick(tp.id)">
          <span class="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-brand-100 text-brand-700 dark:bg-brand-900 dark:text-brand-200"><Icon :name="tp.icon" :size="20" /></span>
          <span>
            <span class="block font-semibold">{{ $t('rules.tpl_' + tp.id) }}</span>
            <span class="block text-sm text-slate-500">{{ $t('rules.tpl_' + tp.id + '_text') }}</span>
          </span>
        </button>
      </div>
    </Modal>
  </div>
</template>
