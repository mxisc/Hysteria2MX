<script setup lang="ts">
defineProps<{ modelValue: boolean }>()
const emit = defineEmits<{ 'update:modelValue': [value: boolean] }>()
</script>
<template>
  <div class="obfs-settings">
    <div class="obfs-settings-head">
      <div class="obfs-settings-icon" aria-hidden="true"><svg viewBox="0 0 24 24"><path d="M3 12h4l3-8 4 16 3-8h4" /></svg></div>
      <div class="obfs-settings-copy"><strong>Brutal 丢包补偿 <span>{{ modelValue ? '已关闭' : '已开启' }}</span></strong><p>{{ modelValue ? '减少额外发送，降低流量与 CPU 消耗；丢包时下载速度可能下降。' : '丢包时增加发送速率，尝试维持客户端下载速度。' }}</p></div>
      <button class="obfs-switch" :class="{ enabled: !modelValue }" type="button" role="switch" :aria-checked="!modelValue" aria-label="Brutal 丢包补偿" @click="emit('update:modelValue', !modelValue)"><span /></button>
    </div>
    <p class="obfs-settings-note">仅影响 Brutal 模式下的客户端下载方向。保存后下发并重启节点服务；客户端无需修改，可靠重传与 TLS 加密仍有效。</p>
  </div>
</template>
<style scoped>
.obfs-settings{border:1px solid var(--panel-border);background:var(--panel-bg-soft);border-radius:16px;padding:18px 20px;margin:18px 0}.obfs-settings-head{display:flex;align-items:center;gap:14px}.obfs-settings-icon{display:grid;place-items:center;width:42px;height:42px;flex:none;border-radius:12px;background:var(--accent-soft);color:var(--accent)}.obfs-settings-icon svg{width:24px;height:24px;fill:none;stroke:currentColor;stroke-width:1.6}.obfs-settings-copy{flex:1}.obfs-settings-copy strong{font-size:14px}.obfs-settings-copy strong span{font-size:11px;font-weight:500;color:var(--text-muted);margin-left:8px}.obfs-settings-copy p,.obfs-settings-note{font-size:12px;color:var(--text-muted);line-height:1.6;margin:5px 0 0}.obfs-settings-note{margin-top:14px;padding-top:12px;border-top:1px solid var(--panel-border)}.obfs-switch{width:46px;height:26px;border:1px solid var(--panel-border-strong);border-radius:20px;background:#334155;padding:3px;flex:none;cursor:pointer;transition:background .15s}.obfs-switch span{display:block;width:18px;height:18px;border-radius:50%;background:#fff;transition:transform .15s;box-shadow:0 2px 5px #0003}.obfs-switch.enabled{background:var(--accent);border-color:var(--accent)}.obfs-switch.enabled span{transform:translateX(19px)}.obfs-switch:focus-visible{outline:2px solid var(--accent);outline-offset:3px}.obfs-password-row{margin:16px 0 0 56px}.obfs-password-input{display:flex;gap:8px}.obfs-password-input input{min-width:0;flex:1}@media(max-width:600px){.obfs-settings{padding:15px}.obfs-settings-icon{display:none}.obfs-password-row{margin-left:0}}
</style>
