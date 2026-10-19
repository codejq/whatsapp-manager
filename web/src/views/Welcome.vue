<script setup>
import { ref, computed } from 'vue'
import { useRouter } from 'vue-router'
import { put } from '../api'
import { app, refreshState } from '../state'
import Icon from '../components/Icon.vue'
import WhatsAppLink from '../components/WhatsAppLink.vue'
import SmtpForm from '../components/SmtpForm.vue'
import logo from '../logo.png'

const router = useRouter()
const step = ref(0)
const steps = ['welcome.step_lang', 'welcome.step_wa', 'welcome.step_email', 'welcome.step_rule']

async function setLang(l) {
  await put('/api/settings', { ...app.general, language: l })
  await refreshState()
}

async function finish(to) {
  await put('/api/settings', { ...app.general, first_run_done: true })
  await refreshState()
  router.replace(to)
}

const connected = computed(() => app.wa.wa === 'connected')
const templates = [
  { id: 'links_email', icon: 'link' },
  { id: 'contact_wa', icon: 'user' },
  { id: 'keyword_group', icon: 'users' },
]
</script>

<template>
  <div class="flex min-h-full items-start justify-center bg-gradient-to-b from-brand-50 to-slate-50 p-6 dark:from-slate-900 dark:to-slate-950">
    <div class="w-full max-w-3xl">
      <div class="mb-6 flex items-center gap-3">
        <img :src="logo" alt="" class="h-12 w-12 rounded-2xl" />
        <div>
          <h1 class="text-2xl font-bold">{{ $t('welcome.title') }}</h1>
          <p class="text-sm text-slate-500">{{ $t('welcome.text') }}</p>
        </div>
      </div>

      <ol class="mb-4 flex gap-2">
        <li v-for="(s, i) in steps" :key="s" class="flex flex-1 items-center gap-2 text-xs font-semibold" :class="i <= step ? 'text-brand-700 dark:text-brand-300' : 'text-slate-400'">
          <span class="h-1.5 flex-1 rounded-full" :class="i <= step ? 'bg-brand-500' : 'bg-slate-200 dark:bg-slate-800'" />
          <span class="hidden sm:inline">{{ $t(s) }}</span>
        </li>
      </ol>

      <div class="card p-6">
        <!-- Language -->
        <div v-if="step === 0" class="space-y-5 text-center">
          <h2 class="text-lg font-bold">{{ $t('welcome.choose_lang') }} · Choose your language</h2>
          <div class="flex justify-center gap-4">
            <button class="choice w-40 !py-6 text-center text-xl font-bold" :class="app.general.language === 'ar' && 'choice-on'" @click="setLang('ar')">العربية</button>
            <button class="choice w-40 !py-6 text-center text-xl font-bold" :class="app.general.language === 'en' && 'choice-on'" @click="setLang('en')">English</button>
          </div>
          <button class="btn-primary" @click="step = 1">{{ $t('welcome.start') }}<Icon name="chevron" :size="16" class="rtl:rotate-180" /></button>
        </div>

        <!-- WhatsApp -->
        <div v-else-if="step === 1">
          <WhatsAppLink>
            <template #connected-actions><span /></template>
          </WhatsAppLink>
          <div class="mt-6 flex justify-between border-t border-slate-200 pt-4 dark:border-slate-800">
            <button class="btn-ghost" @click="step = 0">{{ $t('common.back') }}</button>
            <div class="flex gap-2">
              <button v-if="!connected" class="btn-ghost" @click="step = 2">{{ $t('common.skip') }}</button>
              <button class="btn-primary" :disabled="!connected" @click="step = 2">{{ $t('common.next') }}</button>
            </div>
          </div>
        </div>

        <!-- Email -->
        <div v-else-if="step === 2">
          <h2 class="section-title">{{ $t('welcome.email_title') }}</h2>
          <p class="mb-4 text-sm text-slate-500">{{ $t('welcome.email_text') }}</p>
          <SmtpForm @ok="step = 3" />
          <div class="mt-6 flex justify-between border-t border-slate-200 pt-4 dark:border-slate-800">
            <button class="btn-ghost" @click="step = 1">{{ $t('common.back') }}</button>
            <button class="btn-primary" @click="step = 3">{{ app.smtp.state === 'ok' ? $t('common.next') : $t('common.skip') }}</button>
          </div>
        </div>

        <!-- First rule -->
        <div v-else-if="step === 3" class="space-y-4">
          <div class="flex items-center gap-3 rounded-xl bg-brand-50 p-4 dark:bg-brand-900/20">
            <Icon name="check" :size="24" class="text-brand-600" />
            <div>
              <div class="font-bold">{{ $t('welcome.done_title') }}</div>
              <div class="text-sm text-slate-600 dark:text-slate-300">{{ $t('welcome.done_text') }}</div>
            </div>
          </div>
          <h2 class="section-title">{{ $t('welcome.rule_title') }}</h2>
          <p class="text-sm text-slate-500">{{ $t('welcome.rule_text') }}</p>
          <div class="grid gap-3 sm:grid-cols-3">
            <button v-for="tp in templates" :key="tp.id" class="choice !p-4" @click="finish('/rules/new?template=' + tp.id)">
              <Icon :name="tp.icon" :size="22" class="mb-2 text-brand-600" />
              <span class="block font-semibold">{{ $t('rules.tpl_' + tp.id) }}</span>
              <span class="block text-xs text-slate-500">{{ $t('rules.tpl_' + tp.id + '_text') }}</span>
            </button>
          </div>
          <div class="flex justify-between border-t border-slate-200 pt-4 dark:border-slate-800">
            <button class="btn-ghost" @click="step = 2">{{ $t('common.back') }}</button>
            <button class="btn-secondary" @click="finish('/')">{{ $t('welcome.go_home') }}</button>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
