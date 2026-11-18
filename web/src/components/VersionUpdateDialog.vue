<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'

const props = defineProps<{
  currentVersion: string
  latestVersion: string
  upgrading: boolean
  complete: boolean
  error: string
}>()
const emit = defineEmits<{ close: []; upgrade: [] }>()
const dialog = ref<HTMLDialogElement | null>(null)

function cancel(event: Event) {
  event.preventDefault()
  if (!props.upgrading) emit('close')
}

onMounted(() => dialog.value?.showModal())
onBeforeUnmount(() => dialog.value?.close())
</script>

<template>
  <dialog ref="dialog" class="modal-card version-update-dialog" aria-labelledby="version-update-title" aria-describedby="version-update-note" @cancel="cancel">
    <div class="version-dialog-heading">
      <span class="version-dialog-icon" :class="{ 'is-loading': upgrading && !complete, 'is-complete': complete }">
        <svg v-if="complete" viewBox="0 0 24 24" aria-hidden="true"><path d="m5 12 4 4L19 6" /></svg>
        <svg v-else-if="upgrading" viewBox="0 0 24 24" aria-hidden="true"><path d="M20 12a8 8 0 1 1-8-8" /></svg>
        <svg v-else viewBox="0 0 24 24" aria-hidden="true"><path d="M12 3v12m-4-4 4 4 4-4M5 17v4h14v-4" /></svg>
      </span>
      <h3 id="version-update-title" aria-live="polite">{{ complete ? '升级完成' : upgrading ? '正在升级' : '发现新版本' }}</h3>
    </div>

    <div class="version-dialog-versions">
      <div><span>当前版本</span><strong>v{{ currentVersion }}</strong></div>
      <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4 12h16m-6-6 6 6-6 6" /></svg>
      <div class="version-dialog-latest"><span>最新版本</span><strong>v{{ latestVersion }}</strong></div>
    </div>

    <p id="version-update-note" class="version-dialog-note">{{ complete ? '页面即将刷新。' : upgrading ? '请稍候，完成后页面会自动刷新。' : '升级前自动备份配置，完成后刷新页面。' }}</p>
    <p v-if="error" class="version-dialog-error" role="alert">{{ error }}</p>

    <div class="version-dialog-actions">
      <button class="secondary" type="button" autofocus :disabled="upgrading" @click="emit('close')">稍后</button>
      <button class="primary" type="button" :disabled="upgrading" @click="emit('upgrade')">{{ complete ? '已完成' : upgrading ? '升级中…' : error ? '重试升级' : '立即升级' }}</button>
    </div>
  </dialog>
</template>

<style scoped>
.version-update-dialog { width: min(440px, calc(100vw - 32px)); margin: auto; padding: 24px; border-radius: 18px; background: var(--panel-bg); color: var(--text-main); }
.version-update-dialog::backdrop { background: rgba(2, 6, 23, .68); backdrop-filter: blur(6px); }
.version-dialog-heading { display: flex; align-items: center; gap: 12px; }
.version-dialog-heading h3 { margin: 0; font-size: 18px; font-weight: 600; }
.version-dialog-icon { display: grid; place-items: center; width: 42px; height: 42px; flex-shrink: 0; border: 1px solid var(--accent-border); border-radius: 12px; background: var(--accent-soft); color: var(--accent); }
svg { width: 20px; height: 20px; fill: none; stroke: currentColor; stroke-width: 1.8; stroke-linecap: round; stroke-linejoin: round; }
.is-loading svg { animation: version-spin 1s linear infinite; }
.is-complete { border-color: rgba(52, 211, 153, .25); background: rgba(52, 211, 153, .1); color: var(--success); }
.version-dialog-versions { display: grid; grid-template-columns: minmax(0, 1fr) 20px minmax(0, 1fr); align-items: center; gap: 14px; margin-top: 24px; padding: 18px 16px; border: 1px solid var(--panel-border); border-radius: 12px; background: var(--panel-bg-soft); }
.version-dialog-versions > div { display: grid; gap: 5px; text-align: center; }
.version-dialog-versions span { color: var(--text-muted); font-size: 12px; }
.version-dialog-versions strong { font-family: ui-monospace, SFMono-Regular, Consolas, monospace; font-size: 20px; font-weight: 600; overflow-wrap: anywhere; }
.version-dialog-versions > svg { color: var(--text-muted); }
.version-dialog-latest strong { color: var(--accent); }
.version-dialog-note { margin: 16px 0 0; color: var(--text-muted); font-size: 12px; text-align: center; }
.version-dialog-error { margin: 12px 0 0; color: var(--danger); font-size: 12px; text-align: center; }
.version-dialog-actions { display: flex; justify-content: flex-end; gap: 10px; margin-top: 24px; }
.version-dialog-actions button { min-width: 96px; min-height: 40px; border-radius: 10px; }
button:focus-visible { outline: 2px solid var(--accent); outline-offset: 3px; }
@keyframes version-spin { to { transform: rotate(360deg); } }
@media (prefers-reduced-motion: reduce) { .is-loading svg { animation: none; } }
</style>
