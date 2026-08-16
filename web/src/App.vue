<script setup lang="ts">
import AuroraBackground from './components/AuroraBackground.vue'
</script>

<template>
  <AuroraBackground />
  <div class="relative z-10 h-full">
    <router-view />
  </div>
</template>
