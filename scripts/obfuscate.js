import { readFileSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import JavaScriptObfuscator from 'javascript-obfuscator'

const ASSETS_DIR = new URL('../public/assets', import.meta.url).pathname
const INDEX_HTML = new URL('../public/index.html', import.meta.url).pathname
const indexHtml = readFileSync(INDEX_HTML, 'utf-8')
const files = [...indexHtml.matchAll(/\/assets\/([^"'?#]+\.js)\b/g)].map((match) => match[1])

for (const file of files) {
  const filePath = join(ASSETS_DIR, file)
  const code = readFileSync(filePath, 'utf-8')

  console.log(`Obfuscating ${file}...`)

  const obfuscated = JavaScriptObfuscator.obfuscate(code, {
    compact: true,
    controlFlowFlattening: false,
    deadCodeInjection: false,
    debugProtection: false,
    disableConsoleOutput: false,
    identifierNamesGenerator: 'hexadecimal',
    log: false,
    numbersToExpressions: true,
    renameGlobals: false,
    selfDefending: false,
    simplify: true,
    splitStrings: true,
    splitStringsChunkLength: 5,
    stringArray: true,
    stringArrayCallsTransform: false,
    stringArrayEncoding: ['rc4'],
    stringArrayIndexShift: true,
    stringArrayRotate: true,
    stringArrayShuffle: true,
    stringArrayThreshold: 0.75,
    transformObjectKeys: false,
    unicodeEscapeSequence: false,
  })

  writeFileSync(filePath, obfuscated.getObfuscatedCode())
}

console.log('Obfuscation done.')
