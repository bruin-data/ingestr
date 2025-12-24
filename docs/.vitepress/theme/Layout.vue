<script setup>
import DefaultTheme from 'vitepress/theme'
import CopyPageButton from './CopyPageButton.vue'
import { useData } from 'vitepress'

const { Layout } = DefaultTheme
const { frontmatter } = useData()
</script>

<template>
  <Layout>
    <template #doc-before>
      <div class="copy-page-wrapper" v-if="frontmatter.layout !== 'home'">
        <CopyPageButton />
      </div>
    </template>
  </Layout>
</template>

<style>
.copy-page-wrapper {
  float: right;
  margin-top: 4px;
  position: relative;
  z-index: 10;
}
</style>
