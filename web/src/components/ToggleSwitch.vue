<script setup lang="ts">
defineProps<{ modelValue: boolean; label: string; disabled?: boolean }>()
const emit=defineEmits<{ 'update:modelValue':[value:boolean] }>()
</script>
<template><button class="toggle-switch" :class="{enabled:modelValue}" type="button" role="switch" :aria-label="label" :aria-checked="modelValue" :disabled="disabled" @click="emit('update:modelValue',!modelValue)"><span /></button></template>
<style scoped>
.toggle-switch{width:46px;height:26px;border:1px solid var(--panel-border-strong);border-radius:20px;background:#334155;padding:3px;flex:none;cursor:pointer;transition:background .15s;display:flex;align-items:center}.toggle-switch span{display:block;width:18px;height:18px;border-radius:50%;background:#fff;transition:transform .15s;box-shadow:0 2px 5px #0003}.toggle-switch.enabled{background:var(--accent);border-color:var(--accent)}.toggle-switch.enabled span{transform:translateX(19px)}.toggle-switch:focus-visible{outline:2px solid var(--accent);outline-offset:3px}.toggle-switch:disabled{opacity:.45;cursor:not-allowed}
</style>
