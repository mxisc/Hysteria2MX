import { existsSync, mkdirSync, readFileSync, renameSync } from 'node:fs'
import { spawnSync } from 'node:child_process'
import { fileURLToPath } from 'node:url'

const projectRoot = fileURLToPath(new URL('..', import.meta.url))
const assetsDir = fileURLToPath(new URL('../public/assets', import.meta.url))
const trashRoot = fileURLToPath(new URL('../Trash/public/assets', import.meta.url))
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

if (existsSync(assetsDir)) {
  mkdirSync(trashRoot, { recursive: true })
  const suffix = `${Date.now()}-${process.pid}`
  renameSync(assetsDir, `${trashRoot}/build-${suffix}`)
}
mkdirSync(assetsDir, { recursive: true })

for (const command of [
  'npx vue-tsc -b',
  'npx vite build',
  'node scripts/obfuscate.js',
]) {
  const result = spawnSync(command, {
    stdio: 'inherit',
    shell: true,
    env,
    cwd: projectRoot,
  })

  if (result.status !== 0) {
    process.exit(result.status ?? 1)
  }
}
