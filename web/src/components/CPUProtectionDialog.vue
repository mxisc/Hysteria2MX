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
    <section class="modal-card cpu-editor" role="dialog" aria-modal="true" aria-labelledby="cpu-protection-title">
      <form @submit.prevent="save">
        <div class="section-head section-head-wrap">
          <h3 id="cpu-protection-title">{{ nodeName }} · CPU 保护</h3>
          <div class="hero-actions">
            <button class="secondary" type="button" :disabled="saving" @click="emit('close')">关闭</button>
            <button class="primary" type="submit" :disabled="loading || saving || !dirty">{{ saving ? '保存中…' : '保存并下发' }}</button>
          </div>
        </div>
        <div class="cpu-status" role="status">{{ loading ? '正在读取配置…' : dirty ? '有未保存的修改' : !configured ? '尚未配置' : applied ? 'Agent 已确认生效' : '已保存，等待 Agent 同步确认' }}</div>
        <p v-if="error" class="cpu-error" role="alert">{{ error }}</p>
        <div class="cpu-enable">
          <strong>自动保护</strong>
          <button class="cpu-switch" type="button" role="switch" :aria-checked="form.enabled" aria-label="自动 CPU 保护" :disabled="loading || saving" @click="form.enabled = !form.enabled"><span /></button>
        </div>
        <div class="form-grid two-up-grid modal-form-grid">
          <label class="form-field"><span>触发阈值（%）</span><input v-model.number="form.high_percent" type="number" min="1" max="100" step="1" required :disabled="loading || saving" /></label>
          <label class="form-field"><span>恢复阈值（%）</span><input v-model.number="form.low_percent" type="number" min="1" max="99" step="1" required :disabled="loading || saving" /></label>
          <label class="form-field"><span>首次保护配额（%）</span><input v-model.number="form.max_quota_percent" type="number" min="1" max="100" step="1" required :disabled="loading || saving" /></label>
          <label class="form-field"><span>最低配额（%）</span><input v-model.number="form.min_quota_percent" type="number" min="1" max="100" step="1" required :disabled="loading || saving" /></label>
        </div>
      </form>
    </section>
  </div>
</template>

<style scoped>
.cpu-editor { width: min(680px, 100%); background: #101a2b; scrollbar-width: none; }
.cpu-editor::-webkit-scrollbar { display: none; }
.section-head h3 { overflow-wrap: anywhere; }
.cpu-status { margin-top: 14px; color: var(--text-muted); font-size: 12px; }
.cpu-error { color: var(--danger); font-size: 12px; }
.cpu-enable { height: 44px; display: flex; align-items: center; justify-content: space-between; margin-top: 20px; padding: 0 14px; border: 1px solid var(--panel-border); border-radius: 11px; background: var(--panel-bg-soft); }
.cpu-enable strong { font-size: 13px; font-weight: 500; }
.cpu-switch { padding: 3px; width: 44px; height: 26px; flex-shrink: 0; border-radius: 20px; background: #43516a; transition: background .2s; }
.cpu-switch span { display: block; width: 20px; height: 20px; border-radius: 50%; background: white; transition: transform .2s; }
.cpu-switch[aria-checked="true"] { background: var(--accent); }
.cpu-switch[aria-checked="true"] span { transform: translateX(18px); }
.modal-form-grid { gap: 18px 22px; }
.form-field > span { font-size: 12px; font-weight: 500; color: var(--text-muted); }
.form-field input { height: 44px; min-height: 44px; box-sizing: border-box; padding: 0 14px; border: 1px solid var(--panel-border); border-radius: 11px; background: var(--panel-bg-soft); color: var(--text-main); font-size: 13px; transition: border-color .15s, box-shadow .15s; }
.form-field input:focus { outline: none; border-color: var(--accent); box-shadow: 0 0 0 3px var(--accent-soft); }
button:focus-visible { outline: 2px solid var(--accent); outline-offset: 3px; }
button:disabled, input:disabled { opacity: .5; cursor: not-allowed; }
@media (max-width: 540px) { .two-up-grid { grid-template-columns: 1fr; } }
@media (prefers-reduced-motion: reduce) { .cpu-switch, .cpu-switch span, .form-field input { transition: none; } }
</style>
