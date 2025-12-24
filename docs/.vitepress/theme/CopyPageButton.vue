<script setup>
import { ref, computed, watch, onMounted } from 'vue'
import { useData } from 'vitepress'

const { page } = useData()

const copied = ref(false)
const copying = ref(false)
const cachedMarkdown = ref('')

// Get the current page's markdown URL
const markdownUrl = computed(() => {
  const relativePath = page.value.relativePath
  return `https://raw.githubusercontent.com/bruin-data/ingestr/main/docs/${relativePath}`
})

// Pre-fetch markdown when page loads or changes
async function prefetchMarkdown() {
  cachedMarkdown.value = ''
  try {
    const response = await fetch(markdownUrl.value)
    if (response.ok) {
      cachedMarkdown.value = await response.text()
    }
  } catch (e) {
    // Will use fallback when copying
  }
}

onMounted(prefetchMarkdown)
watch(() => page.value.relativePath, prefetchMarkdown)

async function getPageMarkdown() {
  if (cachedMarkdown.value) {
    return cachedMarkdown.value
  }

  try {
    const response = await fetch(markdownUrl.value)
    if (response.ok) {
      return await response.text()
    }
  } catch (e) {
    console.error('Failed to fetch markdown:', e)
  }

  // Fallback: convert current page HTML to markdown-ish text
  const content = document.querySelector('.vp-doc')
  if (content) {
    return convertToMarkdown(content)
  }
  return ''
}

function convertToMarkdown(element) {
  let markdown = ''
  const title = document.querySelector('h1')
  if (title) {
    markdown += `# ${title.textContent}\n\n`
  }

  const content = element.cloneNode(true)

  // Process headings
  content.querySelectorAll('h1, h2, h3, h4, h5, h6').forEach(h => {
    const level = parseInt(h.tagName[1])
    const prefix = '#'.repeat(level)
    h.outerHTML = `\n${prefix} ${h.textContent}\n\n`
  })

  // Process code blocks
  content.querySelectorAll('pre code').forEach(code => {
    const lang = code.className.match(/language-(\w+)/)?.[1] || ''
    code.parentElement.outerHTML = `\n\`\`\`${lang}\n${code.textContent}\`\`\`\n\n`
  })

  // Process inline code
  content.querySelectorAll('code').forEach(code => {
    if (!code.closest('pre')) {
      code.outerHTML = `\`${code.textContent}\``
    }
  })

  // Process links
  content.querySelectorAll('a').forEach(a => {
    const href = a.getAttribute('href')
    const text = a.textContent
    a.outerHTML = `[${text}](${href})`
  })

  // Process bold
  content.querySelectorAll('strong, b').forEach(b => {
    b.outerHTML = `**${b.textContent}**`
  })

  // Process italic
  content.querySelectorAll('em, i').forEach(i => {
    i.outerHTML = `*${i.textContent}*`
  })

  // Process lists
  content.querySelectorAll('ul, ol').forEach(list => {
    const items = list.querySelectorAll(':scope > li')
    let listMarkdown = '\n'
    items.forEach((li, index) => {
      const prefix = list.tagName === 'OL' ? `${index + 1}.` : '-'
      listMarkdown += `${prefix} ${li.textContent.trim()}\n`
    })
    list.outerHTML = listMarkdown + '\n'
  })

  // Get text content and clean up
  markdown = content.textContent
    .replace(/\n{3,}/g, '\n\n')
    .trim()

  return markdown
}

async function copyAsMarkdown() {
  copying.value = true
  try {
    const markdown = await getPageMarkdown()
    await navigator.clipboard.writeText(markdown)
    copied.value = true
    setTimeout(() => {
      copied.value = false
    }, 2000)
  } finally {
    copying.value = false
  }
}
</script>

<template>
  <button class="copy-page-button" @click="copyAsMarkdown">
    <svg class="copy-icon" xmlns="http://www.w3.org/2000/svg" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
      <rect x="9" y="9" width="13" height="13" rx="2" ry="2"></rect>
      <path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"></path>
    </svg>
    <span>{{ copying ? 'Copying...' : copied ? 'Copied!' : 'Copy page' }}</span>
  </button>
</template>

<style scoped>
.copy-page-button {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 6px 8px;
  background: transparent;
  border: 1px solid var(--vp-c-divider);
  border-radius: 4px;
  color: var(--vp-c-text-3);
  font-size: 12px;
  font-weight: 500;
  cursor: pointer;
  transition: all 0.2s ease;
  font-family: var(--vp-font-family-base);
  white-space: nowrap;
}

.copy-page-button span {
  line-height: 1;
}

.copy-page-button:hover {
  background: var(--vp-c-bg-soft);
  border-color: var(--vp-c-divider);
  color: var(--vp-c-text-2);
}

.copy-icon {
  flex-shrink: 0;
}
</style>
