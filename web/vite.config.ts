import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import path from 'path'

const backendPort = process.env.DENOVA_BACKEND_PORT || process.env.NOVA_BACKEND_PORT || '8080'
const testExecArgv = process.allowedNodeEnvironmentFlags.has('--no-experimental-webstorage')
  ? ['--no-experimental-webstorage']
  : []

// Keep pure transformation/state-machine tests out of jsdom. Opt-in paths are
// intentional: tests that touch API auth, MSW, TipTap, or browser persistence
// still need the DOM project even when their filename ends in `.test.ts`.
const nodeUnitTests = [
  'src/hooks/useWritingSkillOptions.test.ts',
  'src/lib/agent-legacy-message.test.ts',
  'src/lib/agent-message-view.test.ts',
  'src/lib/dialogue-highlight.test.ts',
  'src/lib/plan-mode.test.ts',
  'src/lib/revision-conflict.test.ts',
  'src/lib/text-file.test.ts',
  'src/lib/three-way-rebase.test.ts',
  'src/components/Chat/ModelProfileSwitcher.test.ts',
  'src/components/Chat/composer-token-input.test.ts',
  'src/components/Chat/context-compaction-message.test.ts',
  'src/components/ai-elements/code-block.test.ts',
  'src/features/automations/automation-catalog.test.ts',
  'src/features/automations/automation-task-draft.test.ts',
  'src/features/changes/diff-stats.test.ts',
  'src/features/changes/types.test.ts',
  'src/features/interactive/preset-ownership.test.ts',
  'src/features/interactive/stream-parser.test.ts',
  'src/features/settings/update-reload.test.ts',
  'src/features/writing-quick-actions/quick-actions.test.ts',
  'src/lib/autosave/save-lane.test.ts',
  'src/lib/streaming/markdown-blocks.test.ts',
  'src/lib/streaming/raf-update-batcher.test.ts',
  'src/features/changes/agent/ReviewFeedbackTray.test.ts',
  'src/features/changes/review/ReviewFileDiffSection.test.ts',
  'src/features/changes/review/review-group-projection.test.ts',
  'src/features/changes/review/monaco/review-model-lifecycle.test.ts',
  'src/features/changes/review/monaco/review-monaco-theme.test.ts',
  'src/features/changes/review/monaco/unified-review-projection.test.ts',
  'src/features/changes/review/monaco/utf8-offset-index.test.ts',
  'src/features/interactive/components/story-state/field-layout.test.ts',
  'src/features/interactive/components/story-state/model.test.ts',
  'src/features/interactive/components/preset-config/actor-state-explorer/validation.test.ts',
]

export default defineConfig({
  plugins: [react(), tailwindcss()],
  test: {
    globals: true,
    css: true,
    execArgv: testExecArgv,
    projects: [
      {
        extends: true,
        test: {
          name: 'node',
          environment: 'node',
          include: nodeUnitTests,
          sequence: { groupOrder: 0 },
        },
      },
      {
        extends: true,
        test: {
          name: 'jsdom',
          environment: 'jsdom',
          include: ['src/**/*.test.{ts,tsx}'],
          exclude: nodeUnitTests,
          setupFiles: './src/test/setup.ts',
          sequence: { groupOrder: 1 },
        },
      },
    ],
  },
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  build: {
    rolldownOptions: {
      output: {
        codeSplitting: {
          // Keep size caps on individual groups: a global cap can split tightly coupled SDKs into cyclic chunks.
          minSize: 20 * 1024,
          groups: [
            { name: 'shiki', test: /node_modules[\\/](?:shiki|@shikijs)[\\/]/, priority: 40 },
            { name: 'monaco', test: /node_modules[\\/](?:monaco-editor|@monaco-editor)[\\/]/, priority: 30 },
            { name: 'ai-sdk', test: /node_modules[\\/](?:ai|@ai-sdk)[\\/]/, priority: 20 },
            { name: 'markdown', test: /node_modules[\\/](?:react-markdown|remark-|rehype-|micromark|mdast|hast|unified)[^\\/]*[\\/]/, priority: 10 },
            { name: 'vendor', test: /node_modules[\\/]/, maxSize: 450 * 1024, priority: 1, entriesAware: true },
          ],
        },
      },
    },
  },
  server: {
    proxy: {
      '/api': {
        target: `http://localhost:${backendPort}`,
        changeOrigin: true,
        xfwd: true,
      },
    },
  },
})
