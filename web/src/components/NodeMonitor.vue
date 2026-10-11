<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { LineChart } from 'echarts/charts'
import { GridComponent, LegendComponent, TooltipComponent } from 'echarts/components'
import { init, use } from 'echarts/core'
import type { ECharts, EChartsCoreOption } from 'echarts/core'
import { CanvasRenderer } from 'echarts/renderers'
import { fetchMonitorNodes, fetchNodeMonitor } from '../services/hysteriaService'
import type { MonitorNode, NodeMonitorData, NodeMonitorPoint } from '../types'

use([LineChart, GridComponent, LegendComponent, TooltipComponent, CanvasRenderer])
const props = defineProps<{ initialNodeId?: number }>()
const nodes = ref<MonitorNode[]>([])
const nodeId = ref<number | null>(null)
const range = ref('1h')
const data = ref<NodeMonitorData | null>(null)
const error = ref('')
const loading = ref(false)
const now = ref(Date.now())
const cpuEl = ref<HTMLElement>()
const memoryEl = ref<HTMLElement>()
const networkEl = ref<HTMLElement>()
const diskEl = ref<HTMLElement>()
const charts = new Map<string, ECharts>()
const node = computed(() => nodes.value.find(n => n.id === nodeId.value))
const latest = computed(() => data.value?.latest)
const fresh = computed(() => !error.value && data.value?.fresh && latest.value && now.value - latest.value.sampled_at_ms < 20000)
const ranges = [{ id: '1h', label: '近 1 小时' }, { id: '6h', label: '近 6 小时' }, { id: '24h', label: '近 24 小时' }, { id: '7d', label: '近 7 天' }]
const status = computed(() => !node.value?.agent_enabled ? '未启用 Agent' : !latest.value ? '等待首次上报' : fresh.value ? '实时监控中' : '数据已过期')
let timer: ReturnType<typeof setInterval> | undefined
let observer: ResizeObserver | undefined
let controller: AbortController | undefined
let nodesController: AbortController | undefined
let disposed = false
let revision = 0

function bytes(value?: number) {
  if (value == null) return '—'
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB']
  let i = 0
  while (value >= 1024 && i < units.length - 1) { value /= 1024; i++ }
  return `${value.toFixed(i ? 1 : 0)} ${units[i]}`
}
function percent(value?: number | null) { return value == null ? '—' : `${value.toFixed(1)}%` }
function speed(value?: number | null) { return value == null ? '—' : `${(value * 8 / 1e6).toFixed(1)} Mbps` }
function uptime(value?: number) {
  if (value == null) return '—'
  return `${Math.floor(value / 86400)} 天 ${Math.floor(value / 3600) % 24} 小时`
}

async function refresh(reset = false) {
  if (disposed || nodeId.value === null || document.hidden) return
  if (controller && !reset) return
  controller?.abort()
  const request = new AbortController()
  controller = request
  const id = nodeId.value, selectedRange = range.value, currentRevision = ++revision
  if (reset) { data.value = null; error.value = ''; loading.value = true; void render() }
  const timeout = window.setTimeout(() => request.abort(), 8000)
  try {
    const result = await fetchNodeMonitor(id, selectedRange, request.signal)
    if (disposed || currentRevision !== revision) return
    data.value = result; error.value = ''; now.value = Date.now()
    await render()
  } catch {
    if (!disposed && currentRevision === revision) error.value = '监控数据暂时无法刷新，稍后将自动重试。'
  } finally {
    window.clearTimeout(timeout)
    if (currentRevision === revision) { loading.value = false; controller = undefined }
  }
}

function series(name: string, field: keyof NodeMonitorPoint, color: string, divisor = 1, dashed = false) {
  const d = data.value
  const values: Array<[number, number | null]> = []
  if (d) {
    const lookup = new Map(d.points.map(p => [p.sampled_at_ms, p]))
    for (let t = Math.floor(d.from_ms / d.bucket_ms) * d.bucket_ms; t <= d.to_ms; t += d.bucket_ms) {
      const value = lookup.get(t)?.[field]
      values.push([t, typeof value === 'number' ? value / divisor : null])
    }
  }
  return { name, type: 'line', data: values, showSymbol: false, connectNulls: false, smooth: false, animation: false,
    lineStyle: { width: dashed ? 1.2 : 2, color, type: dashed ? 'dashed' : 'solid' }, itemStyle: { color },
    areaStyle: dashed ? undefined : { color, opacity: .075 } }
}

async function render() {
  await nextTick()
  if (disposed) return
  const d = data.value
  const timeFormat = (value: number) => new Date(value).toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' })
  const common = (unit: string, max?: number): EChartsCoreOption => ({
    backgroundColor: 'transparent', animation: false,
    color: ['#60a5fa', '#34d399'],
    grid: { left: 52, right: 22, top: 43, bottom: 34 },
    legend: { top: 5, left: 12, itemWidth: 14, itemHeight: 7, textStyle: { color: '#91a1b6', fontSize: 11 } },
    tooltip: { trigger: 'axis', confine: true, backgroundColor: '#131d2c', borderColor: '#334155', textStyle: { color: '#e5edf6', fontSize: 12 },
      axisPointer: { type: 'line' }, valueFormatter: (v: unknown) => typeof v === 'number' ? `${v.toFixed(2)}${unit}` : '无采样' },
    xAxis: { type: 'time', min: d?.from_ms, max: d?.to_ms, splitNumber: 4, axisLine: { lineStyle: { color: '#334155' } }, axisTick: { show: false }, axisLabel: { color: '#91a1b6', fontSize: 10, hideOverlap: true, formatter: timeFormat }, splitLine: { show: false } },
    yAxis: { type: 'value', min: 0, max, splitNumber: 4, interval: max === 100 ? 25 : undefined, axisLabel: { color: '#91a1b6', fontSize: 10, formatter: unit === '%' ? '{value}%' : '{value}' }, splitLine: { lineStyle: { color: 'rgba(148,163,184,.10)' } } },
  })
  const cpuSeries = [series('CPU 使用率', 'cpu_percent', '#60a5fa')]
  if (d && d.bucket_ms > 5000) cpuSeries.push(series('区间峰值', 'cpu_peak', '#fbbf24', 1, true))
  const options = [
    ['cpu', cpuEl.value, { ...common('%', 100), series: cpuSeries }],
    ['memory', memoryEl.value, { ...common('%', 100), series: [series('内存', 'memory_percent', '#34d399'), series('Swap', 'swap_percent', '#c084fc')] }],
    ['network', networkEl.value, { ...common(' Mbps'), series: [series('接收', 'rx_bytes_per_second', '#38bdf8', 125000), series('发送', 'tx_bytes_per_second', '#a78bfa', 125000)] }],
    ['disk', diskEl.value, { ...common('%', 100), series: [series('根分区使用率', 'disk_percent', '#fbbf24')] }],
  ] as const
  for (const [key, el, option] of options) {
    if (!el) continue
    let chart = charts.get(key)
    if (!chart) { chart = init(el); charts.set(key, chart); observer?.observe(el) }
    chart.setOption(option as EChartsCoreOption, { notMerge: true })
  }
}

function visible() { if (!document.hidden) { now.value = Date.now(); void refresh() } }
watch([nodeId, range], () => { void refresh(true) })
onMounted(async () => {
  observer = new ResizeObserver(() => charts.forEach(c => c.resize()))
  document.addEventListener('visibilitychange', visible)
  timer = setInterval(() => { now.value = Date.now(); void refresh() }, 5000)
  nodesController = new AbortController()
  const timeout = setTimeout(() => nodesController?.abort(), 8000)
  try {
    const list = await fetchMonitorNodes(nodesController.signal)
    if (disposed) return
    nodes.value = list
    nodeId.value = list.some(n => n.id === props.initialNodeId) ? props.initialNodeId! : list[0]?.id ?? null
    await render()
  } catch { if (!disposed) error.value = '节点列表暂时无法读取，请重新进入监控页面。' }
  finally { clearTimeout(timeout) }
})
onBeforeUnmount(() => {
  disposed = true; revision++; controller?.abort(); nodesController?.abort()
  clearInterval(timer); observer?.disconnect(); document.removeEventListener('visibilitychange', visible)
  charts.forEach(c => c.dispose()); charts.clear()
})
</script>

<template>
  <section class="content-page monitor-page">
    <article class="card monitor-toolbar">
      <div><p class="eyebrow">NODE MONITOR</p><h3>节点监控</h3><p class="monitor-subtitle">每 5 秒采样 · 系统资源与网卡流量 · 保留 7 天</p></div>
      <div class="monitor-controls"><label>监控节点<select aria-label="监控节点" v-model="nodeId" :disabled="!nodes.length"><option v-for="n in nodes" :key="n.id" :value="n.id">{{ n.name }} · {{ n.host }}</option></select></label><div class="monitor-ranges" aria-label="监控时间范围"><button v-for="r in ranges" :key="r.id" :class="{ active: range === r.id }" @click="range = r.id">{{ r.label }}</button></div></div>
    </article>
    <div class="monitor-live-row"><span class="monitor-live" :class="{ live: fresh }"><i />{{ status }}</span><span v-if="latest">最后采样 {{ new Date(latest.sampled_at_ms).toLocaleTimeString('zh-CN') }}{{ fresh ? '' : ' · 下方为最后一次记录' }}</span><span v-else>{{ loading ? '正在读取监控记录…' : '新监控从部署后开始记录' }}</span></div>
    <p v-if="error" class="monitor-error" role="status">{{ error }}</p>
    <article v-if="!nodes.length && !error" class="card monitor-empty">暂无节点，请先添加节点。</article>
    <template v-if="node">
      <div class="monitor-summary" :class="{ stale: !fresh }">
        <article class="card monitor-stat"><span>整机 CPU</span><strong>{{ percent(latest?.cpu_percent) }}</strong><small>{{ latest?.cpu_cores ?? '—' }} vCPU · 全核平均利用率</small><div class="monitor-meter"><i :style="{ width: `${latest?.cpu_percent ?? 0}%`, background: '#60a5fa' }" /></div></article>
        <article class="card monitor-stat"><span>内存使用</span><strong>{{ percent(latest?.memory_percent) }}</strong><small>{{ bytes(latest?.memory_used) }} / {{ bytes(latest?.memory_total) }}</small><div class="monitor-meter"><i :style="{ width: `${latest?.memory_percent ?? 0}%`, background: '#34d399' }" /></div></article>
        <article class="card monitor-stat"><span>网卡实时发送</span><strong>{{ speed(latest?.tx_bytes_per_second) }}</strong><small>接收 {{ speed(latest?.rx_bytes_per_second) }}</small><div class="monitor-network-key"><i />全部非回环网卡</div></article>
        <article class="card monitor-stat"><span>磁盘使用</span><strong>{{ percent(latest?.disk_percent) }}</strong><small>{{ bytes(latest?.disk_used) }} / {{ bytes(latest?.disk_total) }}</small><div class="monitor-meter"><i :style="{ width: `${latest?.disk_percent ?? 0}%`, background: '#fbbf24' }" /></div></article>
      </div>
      <article class="card monitor-details"><span>运行时间 <b>{{ uptime(latest?.uptime_seconds) }}</b></span><span>1 分钟负载 <b>{{ latest?.load1.toFixed(2) ?? '—' }}</b></span><span>Swap <b>{{ bytes(latest?.swap_used) }} / {{ bytes(latest?.swap_total) }}</b></span><span>Agent <b>{{ data?.agent?.version ?? '未部署' }}</b></span><span>流量混淆 <b>{{ node.obfs_enabled ? '开启' : '关闭' }}</b></span></article>
      <div class="monitor-charts">
        <article class="card monitor-chart"><div class="monitor-chart-heading"><h4>CPU 使用率</h4><span>{{ data && data.bucket_ms > 5000 ? '平均值与区间峰值' : '全核平均 · %' }}</span></div><div ref="cpuEl" class="monitor-canvas" /><div v-if="!data?.points.length" class="monitor-chart-empty">{{ loading ? '加载中…' : '等待监控数据' }}</div></article>
        <article class="card monitor-chart"><div class="monitor-chart-heading"><h4>内存与 Swap</h4><span>使用率 · %</span></div><div ref="memoryEl" class="monitor-canvas" /><div v-if="!data?.points.length" class="monitor-chart-empty">{{ loading ? '加载中…' : '等待监控数据' }}</div></article>
        <article class="card monitor-chart"><div class="monitor-chart-heading"><h4>网络收发速率</h4><span>系统网卡 · Mbps</span></div><div ref="networkEl" class="monitor-canvas" /><div v-if="!data?.points.length" class="monitor-chart-empty">{{ loading ? '加载中…' : '等待监控数据' }}</div></article>
        <article class="card monitor-chart"><div class="monitor-chart-heading"><h4>磁盘使用率</h4><span>根分区 · %</span></div><div ref="diskEl" class="monitor-canvas" /><div v-if="!data?.points.length" class="monitor-chart-empty">{{ loading ? '加载中…' : '等待监控数据' }}</div></article>
      </div>
      <article class="card monitor-counters"><div><span>本次启动累计接收</span><strong>{{ bytes(latest?.network_rx) }}</strong></div><div><span>本次启动累计发送</span><strong>{{ bytes(latest?.network_tx) }}</strong></div><p>累计计数在重启后重新开始；图表保留历史采样。内存使用量扣除可用内存，离线或漏采期间显示空档。</p></article>
    </template>
  </section>
</template>

<style scoped>
.monitor-page{gap:18px}.monitor-toolbar{display:flex;align-items:center;justify-content:space-between;gap:24px}.monitor-toolbar h3{margin:0}.monitor-subtitle{font-size:12px;color:var(--text-muted);margin:8px 0 0}.monitor-controls{display:flex;align-items:flex-end;gap:16px;flex-wrap:wrap;justify-content:flex-end}.monitor-controls label{display:grid;gap:6px;color:var(--text-muted);font-size:11px}.monitor-controls select{min-width:210px;max-width:340px;height:40px;padding:8px 12px;background:var(--panel-bg-soft);color:var(--text-main);border:1px solid var(--panel-border-strong);border-radius:10px;font:inherit;font-size:12px}.monitor-ranges{display:flex;gap:3px;padding:4px;border:1px solid var(--panel-border);border-radius:12px;background:var(--panel-bg-soft)}.monitor-ranges button{border:0;border-radius:8px;padding:9px 12px;font-size:12px;background:transparent;color:var(--text-muted);cursor:pointer;white-space:nowrap}.monitor-ranges button.active{background:var(--accent-soft);color:#93c5fd}.monitor-live-row{display:flex;justify-content:space-between;gap:14px;align-items:center;font-size:11px;color:var(--text-muted);padding:0 4px}.monitor-live{display:flex;gap:7px;align-items:center}.monitor-live i{width:6px;height:6px;border-radius:50%;background:var(--text-muted)}.monitor-live.live{color:var(--success)}.monitor-live.live i{background:var(--success);box-shadow:0 0 0 4px #34d39914}.monitor-error{padding:12px 16px;border:1px solid #fbbf2430;border-radius:12px;color:var(--warning);font-size:12px;margin:0}.monitor-summary{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:16px}.monitor-summary.stale{opacity:.65}.monitor-stat{padding:20px;min-width:0;display:flex;flex-direction:column;gap:8px}.monitor-stat>span{font-size:12px;color:var(--text-muted)}.monitor-stat strong{font-size:28px;font-weight:650;letter-spacing:-.7px;white-space:nowrap}.monitor-stat small{font-size:11px;color:var(--text-muted);white-space:nowrap}.monitor-meter{height:4px;border-radius:4px;background:#94a3b814;overflow:hidden;margin-top:7px}.monitor-meter i{display:block;height:100%;border-radius:4px;transition:width .25s}.monitor-network-key{display:flex;align-items:center;gap:6px;font-size:10px;color:var(--text-muted);margin-top:4px}.monitor-network-key i{height:4px;width:15px;background:#a78bfa;border-radius:3px}.monitor-details{padding:14px 20px;display:flex;gap:20px;flex-wrap:wrap;justify-content:space-between;font-size:11px;color:var(--text-muted)}.monitor-details b{font-weight:500;color:var(--text-main);margin-left:8px}.monitor-charts{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:16px}.monitor-chart{position:relative;min-width:0;padding:18px 14px 10px}.monitor-chart-heading{display:flex;align-items:center;justify-content:space-between;padding:0 6px;gap:12px}.monitor-chart-heading h4{font-size:13px;margin:0;font-weight:600}.monitor-chart-heading span{font-size:10px;color:var(--text-muted)}.monitor-canvas{width:100%;height:240px;margin-top:10px}.monitor-chart-empty{position:absolute;inset:45% 0 auto;text-align:center;pointer-events:none;color:var(--text-muted);font-size:12px}.monitor-counters{display:flex;gap:30px;align-items:center;padding:18px 20px}.monitor-counters div{display:grid;gap:6px;flex:none}.monitor-counters span{font-size:11px;color:var(--text-muted)}.monitor-counters strong{font-size:18px;font-weight:600}.monitor-counters p{margin:0 0 0 auto;max-width:400px;font-size:11px;line-height:1.7;color:var(--text-muted)}.monitor-empty{font-size:13px;color:var(--text-muted)}@media(max-width:1100px){.monitor-toolbar{align-items:flex-start;flex-direction:column}.monitor-controls{justify-content:flex-start;width:100%}.monitor-summary{grid-template-columns:repeat(2,minmax(0,1fr))}.monitor-counters{flex-wrap:wrap}.monitor-counters p{margin:0;max-width:none}}@media(max-width:650px){.monitor-charts{grid-template-columns:1fr}.monitor-canvas{height:220px}.monitor-summary{gap:10px}.monitor-stat{padding:16px}.monitor-stat strong{font-size:23px}.monitor-stat small{font-size:10px}.monitor-controls{flex-direction:column;align-items:stretch}.monitor-controls select{max-width:none;width:100%}.monitor-ranges{justify-content:space-between}.monitor-ranges button{padding:9px 8px}.monitor-live-row{align-items:flex-start;flex-direction:column;gap:5px}.monitor-details{gap:12px}.monitor-counters{gap:18px}}
</style>
