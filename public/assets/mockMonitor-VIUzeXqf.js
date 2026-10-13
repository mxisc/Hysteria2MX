import { t as getMockNodes } from "./index-_kQq3amT.js";
//#region web/src/services/mockMonitor.ts
function createMockMonitor(nodeId, range) {
	const now = Date.now();
	const settings = {
		"1h": [36e5, 5e3],
		"6h": [216e5, 3e4],
		"24h": [864e5, 12e4],
		"7d": [6048e5, 9e5]
	};
	const [window, bucket] = settings[range] ?? settings["1h"];
	const from = now - window;
	const points = [];
	for (let t = Math.floor(from / bucket) * bucket; t <= now; t += bucket) {
		const x = t / 6e4;
		if (x % 40 > 29 && x % 40 < 31) continue;
		const cpu = Math.max(0, Math.min(100, 32 + 22 * Math.sin(x / 3) + 13 * Math.sin(x / .8)));
		points.push({
			sampled_at_ms: t,
			cpu_percent: cpu,
			cpu_peak: Math.min(100, cpu + 7),
			memory_percent: 42 + 4 * Math.sin(x / 6),
			swap_percent: 0,
			disk_percent: 31.4,
			load1: cpu / 100,
			rx_bytes_per_second: (70 + 55 * Math.sin(x / 3)) * 125e3,
			tx_bytes_per_second: (72 + 56 * Math.sin(x / 3)) * 125e3
		});
	}
	const p = points.at(-1);
	return {
		node_id: nodeId,
		latest: {
			sampled_at_ms: now,
			boot_id: "mock-boot",
			cpu_cores: 1,
			cpu_percent: p.cpu_percent,
			memory_total: 492830720,
			memory_used: 218103808,
			memory_percent: p.memory_percent,
			swap_total: 268435456,
			swap_used: 0,
			swap_percent: 0,
			disk_total: 21474836480,
			disk_used: 6743098654,
			disk_percent: p.disk_percent,
			load1: p.load1,
			uptime_seconds: 26142,
			network_rx: 52741928400,
			network_tx: 53419512300,
			rx_bytes_per_second: p.rx_bytes_per_second,
			tx_bytes_per_second: p.tx_bytes_per_second
		},
		points,
		fresh: true,
		agent: getMockNodes().find((n) => n.id === nodeId)?.agent ?? null,
		from_ms: from,
		to_ms: now,
		bucket_ms: bucket,
		retention_days: 7
	};
}
//#endregion
export { createMockMonitor };
