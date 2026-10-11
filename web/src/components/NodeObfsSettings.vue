<script setup lang="ts">
import { ref } from 'vue'
defineProps<{ enabled: boolean; password: string }>()
const emit = defineEmits<{ 'update:enabled': [value: boolean]; 'update:password': [value: string] }>()
const visible = ref(false)
</script>

<template>
  <div class="obfs-settings">
    <div class="obfs-settings-head">
      <div class="obfs-settings-icon" aria-hidden="true"><svg viewBox="0 0 24 24"><path d="M12 3 4 6v6c0 5 8 9 8 9s8-4 8-9V6z" /><path d="m8 12 3 3 5-6" /></svg></div>
      <div class="obfs-settings-copy"><strong>流量混淆 <span>{{ enabled ? '已开启' : '已关闭' }}</span></strong><p>{{ enabled ? '隐藏 QUIC 协议特征，客户端需使用相同混淆配置。' : '使用标准 QUIC，TLS 加密仍然有效。' }}</p></div>
      <button class="obfs-switch" :class="{ enabled }" type="button" role="switch" :aria-checked="enabled" aria-label="流量混淆" @click="emit('update:enabled', !enabled)"><span /></button>
    </div>
    <div v-if="enabled" class="obfs-password-row">
      <label class="form-field"><span>混淆密码</span><div class="obfs-password-input"><input :value="password" :type="visible ? 'text' : 'password'" autocomplete="new-password" placeholder="设置混淆密码" @input="emit('update:password', ($event.target as HTMLInputElement).value)" /><button type="button" class="secondary compact-button" :aria-label="visible ? '隐藏混淆密码' : '显示混淆密码'" @click="visible = !visible">{{ visible ? '隐藏' : '显示' }}</button></div></label>
    </div>
  </div>
</template>

<style scoped>
.obfs-settings{border:1px solid var(--panel-border);background:var(--panel-bg-soft);border-radius:16px;padding:18px 20px;margin:18px 0}.obfs-settings-head{display:flex;align-items:center;gap:14px}.obfs-settings-icon{display:grid;place-items:center;width:42px;height:42px;flex:none;border-radius:12px;background:var(--accent-soft);color:var(--accent)}.obfs-settings-icon svg{width:24px;height:24px;fill:none;stroke:currentColor;stroke-width:1.6}.obfs-settings-copy{flex:1}.obfs-settings-copy strong{font-size:14px}.obfs-settings-copy strong span{font-size:11px;font-weight:500;color:var(--text-muted);margin-left:8px}.obfs-settings-copy p{font-size:12px;color:var(--text-muted);line-height:1.6;margin:5px 0 0}.obfs-switch{width:46px;height:26px;border:1px solid var(--panel-border-strong);border-radius:20px;background:#334155;padding:3px;flex:none;cursor:pointer;transition:background .15s}.obfs-switch span{display:block;width:18px;height:18px;border-radius:50%;background:#fff;transition:transform .15s;box-shadow:0 2px 5px #0003}.obfs-switch.enabled{background:var(--accent);border-color:var(--accent)}.obfs-switch.enabled span{transform:translateX(19px)}.obfs-switch:focus-visible{outline:2px solid var(--accent);outline-offset:3px}.obfs-password-row{margin:16px 0 0 56px}.obfs-password-input{display:flex;gap:8px}.obfs-password-input input{min-width:0;flex:1}@media(max-width:600px){.obfs-settings{padding:15px}.obfs-settings-icon{display:none}.obfs-password-row{margin-left:0}}
</style>
