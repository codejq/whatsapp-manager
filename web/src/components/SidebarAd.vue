<script setup>
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { SPONSOR, PUBLISHER, openExternal } from '../branding'
import logo from '../assets/siu-logo.png'

const { locale } = useI18n()
const url = computed(() => SPONSOR.url[locale.value] || SPONSOR.url.ar)
</script>

<template>
  <div class="mx-3 mb-2 space-y-2">
    <!-- Sponsor -->
    <button
      type="button"
      class="block w-full overflow-hidden rounded-2xl border border-amber-200 bg-gradient-to-b from-amber-50 to-white p-3 text-start shadow-sm transition hover:border-amber-400 hover:shadow dark:border-amber-900/60 dark:from-amber-950/40 dark:to-slate-900"
      :title="url"
      @click="openExternal(url)"
    >
      <div class="mb-2 flex items-center justify-between">
        <span class="text-[10px] font-semibold uppercase tracking-wide text-amber-700 dark:text-amber-300">{{ $t('ad.label') }}</span>
      </div>
      <img :src="logo" alt="" class="mx-auto mb-2 h-16 w-16 rounded-xl bg-white object-contain" />
      <p class="text-sm font-bold leading-snug text-slate-900 dark:text-white">{{ $t('ad.title') }}</p>
      <p class="mt-1 text-xs leading-relaxed text-slate-600 dark:text-slate-300">{{ $t('ad.text') }}</p>
      <span class="mt-2 flex w-full items-center justify-center rounded-lg bg-amber-500 px-3 py-1.5 text-xs font-bold text-white">{{ $t('ad.cta') }}</span>
    </button>

    <!-- Publisher -->
    <button type="button" class="block w-full text-center text-[11px] text-slate-400 hover:text-brand-600" @click="openExternal(PUBLISHER.website)">
      {{ $t('powered.by') }} <span class="font-semibold">{{ PUBLISHER.name }}</span>
    </button>
  </div>
</template>
