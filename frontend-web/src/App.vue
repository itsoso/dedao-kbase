<template>
  <main class="dedao-web-shell">
    <header class="shell-header">
      <div class="shell-brand">
        <span class="eyebrow">Dedao Web GUI</span>
        <h1>{{ currentTitle }}</h1>
      </div>
      <div class="shell-status">
        <span>Private</span>
        <span>Bearer API</span>
      </div>
    </header>

    <nav class="shell-nav" aria-label="Dedao desktop navigation">
      <router-link
        v-for="item in navigationItems"
        :key="item.path"
        :to="item.path"
        :class="{ active: route.path === item.path || route.path.startsWith(`${item.path}/`) }"
      >
        <span>{{ item.label }}</span>
        <small>{{ item.meta }}</small>
      </router-link>
    </nav>

    <router-view />
  </main>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { navigationItems } from './router'

const route = useRoute()

const currentTitle = computed(() => {
  return String(route.meta.title || '书籍知识库')
})
</script>
