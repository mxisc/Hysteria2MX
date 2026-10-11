<script setup lang="ts">
import { computed, onMounted, onBeforeUnmount, ref } from 'vue'
import { fetchCPUProtection, saveCPUProtection } from '../services/hysteriaService'
import type { CPUProtectionConfig } from '../types'

const props = defineProps<{ nodeId: number; nodeName: string }>()
const emit = defineEmits<{ close: [] }>()
const form = ref<CPUProtectionConfig>({ enabled: false, high_percent: 90, low_percent: 70, max_quota_percent: 80, min_quota_percent: 20 })
const loading = ref(true)
const saving = ref(false)
const error = ref('')
const configured = ref(false)
const applied = ref(false)
const baseline = ref('')
const dirty = computed(() => JSON.stringify(form.value) !== baseline.value)
let timer: ReturnType<typeof setInterval> | undefined
let disposed = false
let checking = false

async function check(initial = false) {
  if (checking) return
  checking = true
  try {
    const state = await fetchCPUProtection(props.nodeId)
    if (disposed) return
    if (initial) { form.value = { ...state.config }; baseline.value = JSON.stringify(state.config) }
    configured.value = state.configured
    applied.value = state.applied
    error.value = ''
  } catch {
    if (!disposed) error.value = '暂时无法读取配置或确认下发状态，请稍后重试。'
  } finally {
    checking = false
    if (!disposed) loading.value = false
  }
}

async function save() {
  if (form.value.low_percent >= form.value.high_percent || form.value.min_quota_percent > form.value.max_quota_percent) {
    error.value = '恢复阈值须低于触发阈值，最低配额须不高于保护配额。'
    return
  }
  saving.value = true
  error.value = ''
  try {
    const state = await saveCPUProtection(props.nodeId, form.value)
    if (disposed) return
    form.value = { ...state.config }
    baseline.value = JSON.stringify(state.config)
    configured.value = state.configured
    applied.value = state.applied
  } catch {
    if (!disposed) error.value = '保存失败，请检查参数与节点Agent状态后重试。'
  } finally { saving.value = false }
}

onMounted(async () => { await check(true); if (!disposed) timer = setInterval(() => { if (!saving.value) void check() }, 3000) })
onBeforeUnmount(() => { disposed = true; if (timer) clearInterval(timer) })
</script>

<template>
  <div class="modal-overlay">
    <section class="modal-card" role="dialog" aria-modal="true" aria-labelledby="cpu-protection-title">
      <div class="section-head section-head-wrap">
        <div><p class="eyebrow">节点资源保护</p><h2 id="cpu-protection-title">{{ nodeName }} · CPU保护</h2></div>
        <button class="secondary" :disabled="saving" @click="emit('close')">关闭</button>
      </div>
      <p>按全部核心的整机利用率判断。持续超限时降低转发处理量，负载回落后缓慢恢复。</p>
      <p role="status">{{ loading ? '正在读取…' : dirty ? '有未保存的修改' : !configured ? '尚未配置' : applied ? 'Agent已确认生效' : '已保存，等待Agent上线同步并确认' }}</p>
      <p v-if="error" role="alert">{{ error }}</p>
      <form class="form-grid" @submit.prevent="save">
        <label class="form-field"><span>保护开关</span><select v-model="form.enabled" :disabled="loading || saving"><option :value="false">关闭</option><option :value="true">开启</option></select></label>
        <label class="form-field"><span>触发阈值（%）</span><input v-model.number="form.high_percent" type="number" min="1" max="100" step="1" required :disabled="loading || saving" /></label>
        <label class="form-field"><span>恢复阈值（%）</span><input v-model.number="form.low_percent" type="number" min="1" max="99" step="1" required :disabled="loading || saving" /></label>
        <label class="form-field"><span>首次保护配额（整机算力 %）</span><input v-model.number="form.max_quota_percent" type="number" min="1" max="100" step="1" required :disabled="loading || saving" /></label>
        <label class="form-field"><span>最低配额（整机算力 %）</span><input v-model.number="form.min_quota_percent" type="number" min="1" max="100" step="1" required :disabled="loading || saving" /></label>
        <p>每5秒检测，连续15秒超限后降额；连续一分钟低于恢复阈值后逐步恢复。保护不会重启转发服务。内核网络处理及其他程序仍可能使整机超过阈值。</p>
        <button class="primary" type="submit" :disabled="loading || saving || !dirty">{{ saving ? '保存中…' : '保存并下发' }}</button>
      </form>
    </section>
  </div>
</template>

<style scoped>
.form-grid > p, .form-grid > button { grid-column: 1 / -1; }
[role="alert"] { color: #dc3545; }
</style>
