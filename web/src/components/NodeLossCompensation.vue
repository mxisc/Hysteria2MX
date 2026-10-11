<script setup lang="ts">
import ToggleSwitch from './ToggleSwitch.vue'
defineProps<{ modelValue: boolean }>()
const emit = defineEmits<{ 'update:modelValue': [value: boolean] }>()
</script>
<template>
  <div class="obfs-settings">
    <div class="obfs-settings-head">
      <div class="obfs-settings-icon" aria-hidden="true"><svg viewBox="0 0 24 24"><path d="M3 12h4l3-8 4 16 3-8h4" /></svg></div>
      <div class="obfs-settings-copy"><strong>Brutal 丢包补偿</strong></div>
      <ToggleSwitch :model-value="!modelValue" label="Brutal 丢包补偿" @update:model-value="emit('update:modelValue', !$event)" />
    </div>
  </div>
</template>
<style scoped>
.obfs-settings{border:1px solid var(--panel-border);background:var(--panel-bg-soft);border-radius:16px;padding:14px 16px;margin:0}.obfs-settings-head{display:flex;align-items:center;gap:14px}.obfs-settings-icon{display:grid;place-items:center;width:42px;height:42px;flex:none;border-radius:12px;background:var(--accent-soft);color:var(--accent)}.obfs-settings-icon svg{width:24px;height:24px;fill:none;stroke:currentColor;stroke-width:1.6}.obfs-settings-copy{flex:1}.obfs-settings-copy strong{font-size:13px}.obfs-settings-copy strong span{font-size:11px;font-weight:500;color:var(--text-muted);margin-left:8px}.obfs-settings-copy p{font-size:12px;color:var(--text-muted);line-height:1.6;margin:5px 0 0}@media(max-width:600px){.obfs-settings{padding:15px}.obfs-settings-icon{display:none}}
</style>
