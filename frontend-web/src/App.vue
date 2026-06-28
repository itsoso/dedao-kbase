<template>
  <main class="dedao-web-shell">
    <nav class="shell-nav compact-shell-nav" aria-label="Dedao desktop navigation">
      <router-link
        v-for="item in navigationItems"
        :key="item.path"
        :to="item.path"
        :class="{ active: route.path === item.path || route.path.startsWith(`${item.path}/`) }"
      >
        <span>{{ item.label }}</span>
      </router-link>
    </nav>

    <router-view />
  </main>
</template>

<script setup lang="ts">
import { useRoute } from 'vue-router'
import { navigationItems } from './router'

const route = useRoute()
</script>
