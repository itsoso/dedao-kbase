<template>
  <section class="module-landing">
    <div class="module-hero">
      <span class="eyebrow">Desktop GUI Parity</span>
      <h2>{{ title }}</h2>
      <p>{{ scope }}</p>
    </div>

    <dl class="module-status-grid">
      <div>
        <dt>Status</dt>
        <dd>{{ status }}</dd>
      </div>
      <div>
        <dt>Desktop Source</dt>
        <dd>{{ source }}</dd>
      </div>
    </dl>

    <section class="module-methods">
      <span class="eyebrow">Wails Methods To Migrate</span>
      <code v-for="method in desktopMethods" :key="method">{{ method }}</code>
    </section>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'

const route = useRoute()

const title = computed(() => String(route.meta.title || 'Module'))
const scope = computed(() => String(route.meta.scope || 'pending'))
const status = computed(() => String(route.meta.status || 'planned'))
const source = computed(() => String(route.meta.source || ''))
const desktopMethods = computed(() => {
  return Array.isArray(route.meta.desktopMethods) ? route.meta.desktopMethods.map(String) : []
})
</script>
