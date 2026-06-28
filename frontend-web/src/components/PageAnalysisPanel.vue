<template>
  <section class="page-analysis-panel">
    <div class="analysis-head">
      <div>
        <span class="eyebrow">TokenPlan</span>
        <h3>页面分析</h3>
      </div>
      <select v-model="selectedModel" aria-label="analysis model">
        <option v-for="model in modelOptions" :key="model.value" :value="model.value">
          {{ model.label }}
        </option>
      </select>
    </div>

    <div class="prompt-row">
      <button
        v-for="prompt in prompts"
        :key="prompt.label"
        type="button"
        :class="{ active: selectedMode === prompt.mode }"
        @click="applyPrompt(prompt)"
      >
        {{ prompt.label }}
      </button>
    </div>

    <textarea v-model="question" placeholder="输入你想分析的问题"></textarea>

    <div class="analysis-actions">
      <span>{{ contextStats.sections }} sections · {{ contextStats.chars }} chars</span>
      <button type="button" class="primary-action compact" :disabled="cannotSubmit" @click="submitAnalysis">
        {{ loading ? '分析中' : '分析当前页' }}
      </button>
    </div>

    <div v-if="errorMessage" class="error-strip compact">{{ errorMessage }}</div>

    <article v-if="response" class="analysis-answer">
      <div class="answer-meta">
        <span>{{ response.model }} · {{ response.mode }}</span>
        <span>{{ response.context_stats.chars }} chars</span>
      </div>
      <div class="answer-markdown" v-html="renderedAnswer"></div>
    </article>
  </section>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import {
  KBaseClient,
  type PageAnalysisResponse,
  type PageAnalysisSection,
} from '../api'
import { renderMarkdown } from '../utils/markdownRender'

interface QuickPrompt {
  label: string
  mode: string
  question: string
}

const defaultPrompts: QuickPrompt[] = [
  { label: '重点', mode: 'study', question: '分析当前页面的重点、难点和建议学习路径。' },
  { label: '总结', mode: 'summary', question: '总结当前页面的核心内容和关键结论。' },
  { label: '问题', mode: 'questions', question: '基于当前页面生成适合复习的关键问题和参考答案。' },
]

const modelOptions = [
  { value: 'qwen3.7-max', label: 'Qwen-3.7-Max' },
  { value: 'qwen-plus', label: 'Qwen-Plus' },
  { value: 'qwen-max', label: 'Qwen-Max' },
  { value: 'MiniMax-M2.5', label: 'MiniMax-M2.5' },
]

const props = withDefaults(
  defineProps<{
    baseUrl: string
    token: string
    source: string
    pageTitle: string
    pageUrl?: string
    contextSections: PageAnalysisSection[]
    defaultQuestion?: string
    quickPrompts?: QuickPrompt[]
  }>(),
  {
    pageUrl: '',
    defaultQuestion: '分析当前页面的重点、难点和建议学习路径。',
  },
)

const selectedModel = ref('qwen3.7-max')
const selectedMode = ref('study')
const question = ref(props.defaultQuestion)
const loading = ref(false)
const errorMessage = ref('')
const response = ref<PageAnalysisResponse | null>(null)

const prompts = computed(() => (props.quickPrompts?.length ? props.quickPrompts : defaultPrompts))
const contextStats = computed(() => {
  const usable = props.contextSections.filter((section) => section.content.trim())
  return {
    sections: usable.length,
    chars: usable.reduce((total, section) => total + Array.from(section.content).length, 0),
  }
})
const cannotSubmit = computed(() =>
  loading.value || !props.token.trim() || !question.value.trim() || contextStats.value.sections === 0,
)
const renderedAnswer = computed(() => renderMarkdown(response.value?.answer || ''))

watch(
  () => props.defaultQuestion,
  (next) => {
    if (!response.value && next && question.value === '') {
      question.value = next
    }
  },
)

const applyPrompt = (prompt: QuickPrompt) => {
  selectedMode.value = prompt.mode
  question.value = prompt.question
}

const submitAnalysis = async () => {
  if (cannotSubmit.value) {
    return
  }
  loading.value = true
  errorMessage.value = ''
  try {
    const client = new KBaseClient(props.baseUrl, props.token)
    response.value = await client.analyzePage({
      source: props.source,
      title: props.pageTitle,
      url: props.pageUrl,
      mode: selectedMode.value,
      question: question.value,
      model: selectedModel.value,
      max_context_chars: 12000,
      context_sections: props.contextSections,
    })
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : String(error)
  } finally {
    loading.value = false
  }
}
</script>

<style scoped>
.page-analysis-panel {
  display: grid;
  gap: 10px;
  margin-top: 14px;
  border-top: 1px solid #dbe2ec;
  padding-top: 12px;
}

.analysis-head,
.analysis-actions,
.answer-meta {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.analysis-head h3 {
  margin: 0;
  color: #121926;
  font-size: 15px;
  line-height: 20px;
}

.analysis-head select {
  width: 132px;
}

.prompt-row {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 6px;
}

.prompt-row button {
  min-width: 0;
  padding: 6px 8px;
  font-size: 12px;
  font-weight: 700;
}

.analysis-actions span,
.answer-meta {
  color: #6c7b8f;
  font-size: 11px;
  font-weight: 700;
}

.analysis-answer {
  display: grid;
  gap: 8px;
  border: 1px solid #dbe2ec;
  border-radius: 8px;
  padding: 10px;
  background: #fbfcfe;
}

.answer-markdown {
  overflow-wrap: anywhere;
  color: #263244;
  font-size: 13px;
}

.answer-markdown :deep(p),
.answer-markdown :deep(ul),
.answer-markdown :deep(ol) {
  margin: 6px 0;
}

.answer-markdown :deep(h1),
.answer-markdown :deep(h2),
.answer-markdown :deep(h3) {
  margin: 10px 0 6px;
  color: #121926;
  font-size: 15px;
  line-height: 20px;
}

.answer-markdown :deep(code) {
  border: 1px solid #dbe2ec;
  border-radius: 4px;
  padding: 1px 4px;
  background: #eef2f6;
}

.error-strip.compact {
  padding: 8px 10px;
  font-size: 12px;
}
</style>
