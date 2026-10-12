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
    <section class="modal-card cpu-dialog" role="dialog" aria-modal="true" aria-labelledby="cpu-protection-title">
      <header class="cpu-heading">
        <div class="cpu-title"><span class="cpu-icon" aria-hidden="true">▦</span><div><h2 id="cpu-protection-title">CPU 保护</h2><p class="cpu-node">{{ nodeName }}</p></div></div>
        <button class="cpu-close" type="button" aria-label="关闭 CPU 保护" :disabled="saving" @click="emit('close')">×</button>
      </header>
      <form @submit.prevent="save">
        <div class="cpu-content">
          <section class="cpu-enable" :class="{ active: form.enabled }">
            <div><h3>自动保护 <span class="cpu-tag">{{ form.enabled ? '开启' : '关闭' }}</span></h3></div>
            <button class="cpu-switch" type="button" role="switch" :aria-checked="form.enabled" aria-label="自动 CPU 保护" :disabled="loading || saving" @click="form.enabled = !form.enabled"><span /></button>
          </section>
          <div class="cpu-status" role="status"><i :class="{ confirmed: !loading && !dirty && configured && applied }" />{{ loading ? '正在读取配置…' : dirty ? '有未保存的修改' : !configured ? '尚未配置' : applied ? 'Agent 已确认生效' : '已保存，等待 Agent 同步确认' }}</div>
          <p v-if="error" class="cpu-error" role="alert">{{ error }}</p>
          <section class="cpu-group">
            <div class="cpu-group-head"><span class="cpu-step">01</span><div><h3>负载阈值</h3></div></div>
            <div class="cpu-fields">
              <label class="cpu-field"><span>触发阈值</span><div class="cpu-input"><input v-model.number="form.high_percent" type="number" min="1" max="100" step="1" required :disabled="loading || saving" /><span>%</span></div></label>
              <label class="cpu-field"><span>恢复阈值</span><div class="cpu-input"><input v-model.number="form.low_percent" type="number" min="1" max="99" step="1" required :disabled="loading || saving" /><span>%</span></div></label>
            </div>
          </section>
          <section class="cpu-group">
            <div class="cpu-group-head"><span class="cpu-step">02</span><div><h3>保护配额</h3></div></div>
            <div class="cpu-fields">
              <label class="cpu-field"><span>首次保护配额</span><div class="cpu-input"><input v-model.number="form.max_quota_percent" type="number" min="1" max="100" step="1" required :disabled="loading || saving" /><span>%</span></div></label>
              <label class="cpu-field"><span>最低配额</span><div class="cpu-input"><input v-model.number="form.min_quota_percent" type="number" min="1" max="100" step="1" required :disabled="loading || saving" /><span>%</span></div></label>
            </div>
          </section>
        </div>
        <footer class="cpu-footer"><div><button class="secondary" type="button" :disabled="saving" @click="emit('close')">取消</button><button class="primary" type="submit" :disabled="loading || saving || !dirty">{{ saving ? '保存中…' : '保存并下发' }}</button></div></footer>
      </form>
    </section>
  </div>
</template>

<style scoped>
.cpu-dialog { width: min(680px, 100%); padding: 0; background: var(--panel-bg); scrollbar-width: thin; scrollbar-color: var(--panel-border-strong) transparent; }
.cpu-heading { display: flex; justify-content: space-between; align-items: flex-start; padding: 26px 28px 22px; border-bottom: 1px solid var(--panel-border); }
.cpu-title { display: flex; gap: 14px; align-items: center; min-width: 0; }
.cpu-title > div { min-width: 0; }
.cpu-icon { display: grid; place-items: center; width: 48px; height: 48px; border-radius: 14px; color: var(--accent); background: var(--accent-soft); border: 1px solid var(--accent-border); font-size: 30px; flex-shrink: 0; }
.cpu-heading h2 { margin: 0; font-size: 23px; letter-spacing: -.5px; }
.cpu-node { margin: 4px 0 0; color: var(--text-muted); font-size: 12px; overflow-wrap: anywhere; }
.cpu-close { width: 32px; height: 32px; border-radius: 9px; background: var(--panel-bg-strong); color: var(--text-muted); font-size: 23px; flex-shrink: 0; }
.cpu-close:hover { color: var(--text-strong); background: var(--accent-soft); }
.cpu-content { padding: 24px 28px; }
.cpu-enable { display: flex; align-items: center; justify-content: space-between; gap: 20px; padding: 18px; border: 1px solid var(--panel-border-strong); border-radius: 14px; background: var(--panel-bg-strong); transition: border-color .2s, background .2s; }
.cpu-enable.active { border-color: var(--accent-border); background: var(--accent-soft); }
h3 { margin: 0; font-size: 14px; font-weight: 600; color: var(--text-strong); }
.cpu-tag { margin-left: 7px; font-size: 10px; font-weight: 500; color: var(--text-muted); }
.active .cpu-tag { color: var(--accent); }
.cpu-switch { padding: 3px; width: 44px; height: 26px; flex-shrink: 0; border-radius: 20px; background: #43516a; transition: background .2s; }
.cpu-switch span { display: block; width: 20px; height: 20px; border-radius: 50%; background: white; transition: transform .2s; box-shadow: 0 2px 5px #0003; }
.cpu-switch[aria-checked="true"] { background: var(--accent); }
.cpu-switch[aria-checked="true"] span { transform: translateX(18px); }
.cpu-status { display: flex; align-items: center; gap: 7px; margin: 12px 2px 22px; font-size: 11px; color: var(--text-muted); }
.cpu-status i { width: 6px; height: 6px; border-radius: 50%; background: var(--warning); }
.cpu-status i.confirmed { background: var(--success); }
.cpu-error { padding: 12px 14px; border-radius: 10px; background: #f8717112; color: var(--danger); font-size: 12px; }
.cpu-group + .cpu-group { margin-top: 25px; padding-top: 23px; border-top: 1px solid var(--panel-border); }
.cpu-group-head { display: flex; gap: 10px; align-items: flex-start; margin-bottom: 16px; }
.cpu-step { color: var(--accent); font-size: 11px; font-weight: 600; line-height: 22px; }
.cpu-fields { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 18px; }
.cpu-field { display: grid; gap: 8px; min-width: 0; font-size: 12px; color: var(--text-main); }
.cpu-input { display: flex; align-items: center; border: 1px solid var(--panel-border-strong); border-radius: 10px; background: var(--panel-bg-soft); overflow: hidden; }
.cpu-input:focus-within { border-color: var(--accent); box-shadow: 0 0 0 3px var(--accent-soft); }
.cpu-input input { width: 100%; min-width: 0; padding: 11px 13px; border: 0; outline: none; background: transparent; color: var(--text-strong); font-size: 19px; font-weight: 600; }
.cpu-input > span { padding-right: 14px; color: var(--text-muted); }
.cpu-footer { position: sticky; bottom: 0; z-index: 1; display: flex; align-items: center; justify-content: flex-end; gap: 16px; padding: 18px 28px; border-top: 1px solid var(--panel-border); background: var(--panel-bg-soft); }
.cpu-footer > div { display: flex; gap: 10px; flex-shrink: 0; }
button:focus-visible { outline: 2px solid var(--accent); outline-offset: 3px; }
button:disabled, input:disabled { opacity: .5; cursor: not-allowed; }
@media (max-width: 540px) {
  .modal-overlay { padding: 12px; }
  .cpu-heading, .cpu-content { padding: 20px; }
  .cpu-fields { grid-template-columns: 1fr; gap: 16px; }
  .cpu-footer { padding: 16px 20px; flex-wrap: wrap; }
  .cpu-footer > div { width: 100%; justify-content: flex-end; }
}
@media (prefers-reduced-motion: reduce) { .cpu-switch, .cpu-switch span, .cpu-enable { transition: none; } }
</style>
