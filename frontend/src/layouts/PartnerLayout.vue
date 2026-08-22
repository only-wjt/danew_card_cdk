<template>
  <div class="app-shell layout-sidebar">
    <aside class="sidenav">
      <div class="side-brand" @click="router.push('/partner')">
        <BrandMark :size="22" label="代理" />
        <span class="brand-text">代理控制台</span>
      </div>
      <nav class="side-pills">
        <router-link
          v-for="it in navItems"
          :key="it.path"
          :to="it.path"
          class="side-link"
          :class="{ active: isActive(it.path) }"
        >
          <el-icon><component :is="it.icon" /></el-icon>
          <span class="side-label">{{ it.label }}</span>
        </router-link>
      </nav>
      <div class="side-foot">
        <ThemeToggle />
        <div class="side-user">
          <div class="side-avatar">{{ avatarLetter }}</div>
          <div class="side-user-meta">
            <div class="side-user-name">{{ auth.name || auth.username }}</div>
            <div class="side-user-sub">代理账号</div>
          </div>
        </div>
        <button class="btn-secondary w-full !min-h-0 !py-2 text-sm" @click="doLogout">退出登录</button>
      </div>
    </aside>

    <div class="main-col">
      <header class="subtop">
        <div class="subtop-inner">
          <div>
            <div class="subtop-title">{{ currentTitle }}</div>
            <div v-if="currentSubtitle" class="subtop-sub">{{ currentSubtitle }}</div>
          </div>
        </div>
      </header>
      <main class="page">
        <router-view />
      </main>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import BrandMark from '../components/BrandMark.vue'
import ThemeToggle from '../components/ThemeToggle.vue'
import { useAgentAuthStore } from '../stores/agentAuth'
import { agentFetch } from '../lib/agentApi'

const router = useRouter()
const route = useRoute()
const auth = useAgentAuthStore()

const navItems = [
  { path: '/partner', label: '概览', icon: 'Odometer', subtitle: '套餐、记录与 API 速览' },
  { path: '/partner/batch', label: '批量充值', icon: 'Upload', subtitle: '批量提交与批次对账' },
  { path: '/partner/records', label: '兑换记录', icon: 'Document', subtitle: '按邮箱、卡密或 Session 检索' },
  { path: '/partner/api-keys', label: 'API 密钥', icon: 'Key', subtitle: '管理对接用的访问密钥' },
  { path: '/partner/api-docs', label: 'API 文档', icon: 'Reading', subtitle: '接口参考与规范导出' },
  { path: '/partner/settings', label: '对接设置', icon: 'Setting', subtitle: 'Webhook 与客户单号前缀' },
]

const currentTitle = computed(() => navItems.find((n) => isActive(n.path))?.label || '代理控制台')
const currentSubtitle = computed(() => navItems.find((n) => isActive(n.path))?.subtitle || '')
const avatarLetter = computed(() => (auth.username || 'A').slice(0, 1).toUpperCase())

function isActive(p: string) {
  return p === '/partner' ? route.path === '/partner' : route.path.startsWith(p)
}

async function doLogout() {
  await agentFetch('/api/v1/auth/agent/logout', { method: 'POST' })
  auth.logout()
  router.push('/partner/login')
}
</script>

<style scoped>
.app-shell {
  min-height: 100vh;
  display: flex;
  background: var(--bg);
  background-image: var(--bg-tint);
  background-attachment: fixed;
  color: var(--ink);
}
.layout-sidebar { flex-direction: row; align-items: stretch; }
.main-col {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  min-height: 100vh;
}
.sidenav {
  width: 240px;
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 18px 14px;
  background: var(--surface);
  border-right: 1px solid var(--brd);
  position: sticky;
  top: 0;
  height: 100vh;
}
.side-brand {
  display: flex;
  align-items: center;
  gap: 10px;
  cursor: pointer;
  padding: 4px 6px 12px;
  border-bottom: 1px solid var(--brd);
  margin-bottom: 4px;
}
.brand-text {
  font-size: 16px;
  font-weight: 700;
  color: var(--ink);
  font-family: var(--font-display, var(--font-serif));
}
.side-pills {
  display: flex;
  flex-direction: column;
  gap: 4px;
  flex: 1;
}
.side-link {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 12px;
  border-radius: var(--radius-md, 12px);
  color: var(--ink-2);
  text-decoration: none;
  font-size: 14px;
  transition: all .15s;
}
.side-link:hover {
  background: var(--primary-soft);
  color: var(--ink);
}
.side-link.active {
  background: var(--primary-soft);
  color: var(--primary);
  font-weight: 600;
}
.side-foot {
  display: flex;
  flex-direction: column;
  gap: 12px;
  padding-top: 12px;
  border-top: 1px solid var(--brd);
}
.side-user {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 4px;
}
.side-avatar {
  width: 36px;
  height: 36px;
  border-radius: 10px;
  display: flex;
  align-items: center;
  justify-content: center;
  font-weight: 700;
  font-size: 15px;
  color: var(--primary);
  background: var(--primary-soft);
  flex-shrink: 0;
}
.side-user-name {
  font-size: 14px;
  font-weight: 600;
  color: var(--ink);
  line-height: 1.2;
}
.side-user-sub {
  font-size: 11px;
  color: var(--ink-3);
  margin-top: 2px;
}
.subtop {
  position: sticky;
  top: 0;
  z-index: 50;
  background: color-mix(in srgb, var(--surface) 90%, transparent);
  backdrop-filter: saturate(1.2) blur(10px);
  border-bottom: 1px solid var(--brd);
}
.subtop-inner {
  max-width: var(--content-max, 1200px);
  margin: 0 auto;
  padding: 18px 24px;
}
.subtop-title {
  font-size: 22px;
  font-weight: 700;
  color: var(--ink);
  font-family: var(--font-display, var(--font-serif));
}
.subtop-sub {
  font-size: 13px;
  color: var(--ink-3);
  margin-top: 4px;
}
.page {
  max-width: var(--content-max, 1200px);
  width: 100%;
  margin: 0 auto;
  padding: 24px;
  flex: 1;
}
</style>
