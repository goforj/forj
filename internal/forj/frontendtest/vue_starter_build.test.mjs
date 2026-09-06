import assert from 'node:assert/strict'
import { createRequire } from 'node:module'
import path from 'node:path'
import test from 'node:test'
import { fileURLToPath, pathToFileURL } from 'node:url'

const testDir = path.dirname(fileURLToPath(import.meta.url))
const repositoryRoot = path.resolve(testDir, '../../..')
const frontendRoot = path.join(repositoryRoot, 'templates/starter-kits/vue/frontend')
const requireFromFrontend = createRequire(path.join(frontendRoot, 'package.json'))
const viteEntry = requireFromFrontend.resolve('vite')
const { build } = await import(pathToFileURL(viteEntry).href)
const { createMemoryHistory, createRouter } = await import(pathToFileURL(requireFromFrontend.resolve('vue-router')).href)
const { z } = await import(pathToFileURL(requireFromFrontend.resolve('zod')).href)
const { zodRule } = await import(pathToFileURL(path.join(frontendRoot, 'src/lib/zod-rule.ts')).href)

test('Vue Router preserves the starter navigation contracts', async () => {
  const view = { template: '<div />' }
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', name: 'dashboard', component: view },
      { path: '/login', name: 'login', component: view, meta: { publicShell: true } },
      {
        path: '/settings',
        component: view,
        children: [
          { path: '', redirect: '/settings/profile' },
          { path: 'profile', name: 'settings-profile', component: view },
        ],
      },
      { path: '/:pathMatch(.*)*', name: 'not-found', component: view },
    ],
  })

  await router.push('/settings')
  await router.isReady()
  assert.equal(router.currentRoute.value.fullPath, '/settings/profile')
  assert.equal(router.currentRoute.value.name, 'settings-profile')

  await router.push('/login')
  assert.equal(router.currentRoute.value.meta.publicShell, true)

  await router.push('/missing/nested')
  assert.equal(router.currentRoute.value.name, 'not-found')
  assert.deepEqual(router.currentRoute.value.params.pathMatch, ['missing', 'nested'])
})

test('Zod field rules preserve successful values and validation messages', () => {
  const rule = zodRule(z.string().min(3, { error: 'Enter at least three characters.' }))
  const emailRule = zodRule(z.email({ error: 'Enter a valid email address.' }))

  assert.equal(rule('valid'), true)
  assert.equal(rule(''), 'Enter at least three characters.')
  assert.equal(emailRule('invalid'), 'Enter a valid email address.')
})

test('the production graph retains the complete component showroom', async () => {
  let moduleIDs = []

  await build({
    root: frontendRoot,
    configFile: path.join(frontendRoot, 'vite.config.ts'),
    logLevel: 'silent',
    plugins: [{
      name: 'goforj:test-production-module-graph',
      generateBundle() {
        moduleIDs = [...this.getModuleIds()]
      },
    }],
    build: {
      reportCompressedSize: false,
      write: false,
    },
  })

  const normalized = moduleIDs.map((id) => id.replaceAll('\\', '/'))
  for (const view of ['Overview', 'Forms', 'Navigation', 'Overlays', 'Data']) {
    assert.ok(normalized.some((id) => id.endsWith(`/src/views/components/Components${view}View.vue`)), `Components${view}View.vue was removed from the production graph`)
  }
  assert.ok(moduleIDs.length < 3400, `expected fewer than 3400 total modules, received ${moduleIDs.length}`)
})
