import { readFileSync } from 'node:fs'
import { spawnSync } from 'node:child_process'

const pkg = JSON.parse(readFileSync(new URL('../package.json', import.meta.url), 'utf-8'))
const version = String(pkg.version || '').trim()

if (!version) {
  console.error('缺少 package.json version，无法注入前端版本号')
  process.exit(1)
}

const env = {
  ...process.env,
  VITE_APP_VERSION: version,
}

for (const command of [
  'npx vue-tsc -b',
  'npx vite build',
  'node scripts/obfuscate.js',
]) {
  const result = spawnSync(command, {
    stdio: 'inherit',
    shell: true,
    env,
  })

  if (result.status !== 0) {
    process.exit(result.status ?? 1)
  }
}
