<script setup lang="ts">
import ToggleSwitch from './ToggleSwitch.vue'
import { ref } from 'vue'
defineProps<{ enabled: boolean; password: string; saved?: boolean }>()
const emit = defineEmits<{ 'update:enabled': [value: boolean]; 'update:password': [value: string]; reveal: [] }>()
const visible = ref(false)
</script>

<template>
  <div class="obfs-settings">
    <div class="obfs-settings-head">
      <div class="obfs-settings-icon" aria-hidden="true"><svg viewBox="0 0 24 24"><path d="M12 3 4 6v6c0 5 8 9 8 9s8-4 8-9V6z" /><path d="m8 12 3 3 5-6" /></svg></div>
      <div class="obfs-settings-copy"><strong>流量混淆</strong></div>
      <ToggleSwitch :model-value="enabled" label="流量混淆" @update:model-value="emit('update:enabled', $event)" />
    </div>
    <div v-if="enabled" class="obfs-password-row">
      <label class="form-field"><span>混淆密码</span><div class="obfs-password-input"><input :value="password" :type="visible ? 'text' : 'password'" autocomplete="new-password" :placeholder="saved ? '••••••••' : '设置混淆密码'" @input="emit('update:password', ($event.target as HTMLInputElement).value)" /><button type="button" class="secondary compact-button" :aria-label="visible ? '隐藏混淆密码' : '显示混淆密码'" @click="emit('reveal'); visible = !visible">{{ visible ? '隐藏' : '显示' }}</button></div></label>
    </div>
  </div>
</template>

<style scoped>
.obfs-settings{border:1px solid var(--panel-border);background:var(--panel-bg-soft);border-radius:16px;padding:14px 16px;margin:0}.obfs-settings-head{display:flex;align-items:center;gap:14px}.obfs-settings-icon{display:grid;place-items:center;width:42px;height:42px;flex:none;border-radius:12px;background:var(--accent-soft);color:var(--accent)}.obfs-settings-icon svg{width:24px;height:24px;fill:none;stroke:currentColor;stroke-width:1.6}.obfs-settings-copy{flex:1}.obfs-settings-copy strong{font-size:13px}.obfs-settings-copy strong span{font-size:11px;font-weight:500;color:var(--text-muted);margin-left:8px}.obfs-settings-copy p{font-size:12px;color:var(--text-muted);line-height:1.6;margin:5px 0 0}.obfs-password-row{margin:16px 0 0 56px}.obfs-password-input{display:flex;gap:8px}.obfs-password-input input{min-width:0;flex:1}@media(max-width:600px){.obfs-settings{padding:15px}.obfs-settings-icon{display:none}.obfs-password-row{margin-left:0}}
</style>
