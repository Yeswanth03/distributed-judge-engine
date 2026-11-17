import Editor, { loader } from '@monaco-editor/react'
import * as monaco from 'monaco-editor/editor/editor.api'
import 'monaco-editor/languages/definitions/python/register'
import EditorWorker from 'monaco-editor/editor/editor.worker?worker'
import { useTheme } from '@/theme/theme'

// Monaco is bundled with the app instead of loaded from a CDN at runtime: no third-party script, works offline.
// Only Python is registered, the one language the judge runs.
;(self as unknown as { MonacoEnvironment: { getWorker: () => Worker } }).MonacoEnvironment = {
  getWorker: () => new EditorWorker(),
}
loader.config({ monaco })

// Editor themes matching the app's card colours, with the violet accent on keywords.
monaco.editor.defineTheme('mcj-dark', {
  base: 'vs-dark',
  inherit: true,
  rules: [
    { token: 'keyword', foreground: 'b4a0ff' },
    { token: 'string', foreground: '7ee0c3' },
    { token: 'number', foreground: 'f5c97a' },
    { token: 'comment', foreground: '6e6a7c', fontStyle: 'italic' },
  ],
  colors: {
    'editor.background': '#1c1b21',
    'editor.lineHighlightBackground': '#26252d',
    'editorLineNumber.foreground': '#55525f',
    'editorLineNumber.activeForeground': '#b4a0ff',
    'editor.selectionBackground': '#5b4bb055',
    'editorCursor.foreground': '#b4a0ff',
    'editorIndentGuide.background1': '#2a2931',
  },
})
monaco.editor.defineTheme('mcj-light', {
  base: 'vs',
  inherit: true,
  rules: [
    { token: 'keyword', foreground: '6d28d9' },
    { token: 'string', foreground: '0f8a6c' },
    { token: 'number', foreground: 'b45309' },
    { token: 'comment', foreground: '8b8799', fontStyle: 'italic' },
  ],
  colors: {
    'editor.background': '#ffffff',
    'editor.lineHighlightBackground': '#f6f4fb',
    'editorLineNumber.foreground': '#b9b6c4',
    'editorLineNumber.activeForeground': '#6d28d9',
    'editor.selectionBackground': '#ddd6fe',
    'editorCursor.foreground': '#6d28d9',
  },
})

interface Props {
  value: string
  onChange?: (value: string) => void
  readOnly?: boolean
  height?: string
}

export default function CodeEditor({ value, onChange, readOnly = false, height = '100%' }: Props) {
  const { resolvedTheme } = useTheme()
  return (
    <Editor
      height={height}
      language="python"
      theme={resolvedTheme === 'dark' ? 'mcj-dark' : 'mcj-light'}
      value={value}
      onChange={(v) => onChange?.(v ?? '')}
      options={{
        readOnly,
        minimap: { enabled: false },
        fontSize: 14,
        fontFamily: "'Geist Mono Variable', ui-monospace, monospace",
        fontLigatures: true,
        lineHeight: 22,
        padding: { top: 12, bottom: 12 },
        renderLineHighlight: 'all',
        smoothScrolling: true,
        cursorSmoothCaretAnimation: 'on',
        tabSize: 4,
        insertSpaces: true,
        scrollBeyondLastLine: false,
        automaticLayout: true,
      }}
    />
  )
}
