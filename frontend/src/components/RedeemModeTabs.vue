<template>
  <div class="redeem-tabs card !p-1.5 mb-6">
    <nav class="grid grid-cols-2 sm:grid-cols-4 gap-1">
      <router-link
        v-for="tab in tabs"
        :key="tab.to"
        :to="tab.to"
        class="redeem-tab"
        :class="{ active: isActive(tab.to) }"
      >
        {{ tab.label }}
      </router-link>
    </nav>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'

const route = useRoute()
const { t } = useI18n({ useScope: 'global' })

const tabs = computed(() => [
  { to: '/recharge', label: t('redeemTabs.single') },
  { to: '/batch', label: t('redeemTabs.batch') },
  { to: '/history', label: t('redeemTabs.lookup') },
  { to: '/billing', label: t('redeemTabs.billing') },
])

function isActive(to: string) {
  if (to === '/recharge') return route.path === '/recharge'
  return route.path === to || route.path.startsWith(to + '/')
}
</script>

<!-- 样式在 styles/skins.css（.redeem-tabs / .redeem-tab），各皮肤自定 -->
