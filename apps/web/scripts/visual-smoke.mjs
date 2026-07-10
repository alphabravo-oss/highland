#!/usr/bin/env node
/**
 * Visual smoke documentation + optional Playwright screenshot runner.
 *
 * Expected screenshot paths (written by e2e/visual.spec.ts):
 *
 *   test-results/visual/login-light.png
 *   test-results/visual/login-dark.png
 *   test-results/visual/dashboard-light.png
 *   test-results/visual/dashboard-dark.png
 *   test-results/visual/volumes-list.png
 *   test-results/visual/volume-detail.png
 *   test-results/visual/sso-config.png
 *
 * Storybook static build can feed Chromatic:
 *   npm run build-storybook
 *   npx chromatic --storybook-build-dir storybook-static
 *
 * Usage:
 *   node scripts/visual-smoke.mjs           # print contract
 *   node scripts/visual-smoke.mjs --run     # run Playwright visual suite
 */

import { spawnSync } from 'node:child_process'
import { existsSync } from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const root = path.dirname(fileURLToPath(import.meta.url))
const webRoot = path.resolve(root, '..')

const paths = [
  'test-results/visual/login-light.png',
  'test-results/visual/login-dark.png',
  'test-results/visual/dashboard-light.png',
  'test-results/visual/dashboard-dark.png',
  'test-results/visual/volumes-list.png',
  'test-results/visual/volume-detail.png',
  'test-results/visual/sso-config.png',
]

console.log('Highland visual smoke — screenshot contract:\n')
for (const p of paths) {
  const abs = path.join(webRoot, p)
  const mark = existsSync(abs) ? '✓' : '·'
  console.log(`  ${mark} ${p}`)
}
console.log('\nStorybook build dir: storybook-static/')

if (process.argv.includes('--run')) {
  console.log('\nRunning Playwright visual suite…')
  const r = spawnSync('npx', ['playwright', 'test', 'e2e/visual.spec.ts'], {
    cwd: webRoot,
    stdio: 'inherit',
    env: process.env,
  })
  process.exit(r.status ?? 1)
}

console.log('Tip: npm run test:visual  (or node scripts/visual-smoke.mjs --run)')
