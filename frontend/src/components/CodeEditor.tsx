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
      theme={resolvedTheme === 'dark' ? 'vs-dark' : 'vs'}
      value={value}
      onChange={(v) => onChange?.(v ?? '')}
      options={{
        readOnly,
        minimap: { enabled: false },
        fontSize: 14,
        tabSize: 4,
        insertSpaces: true,
        scrollBeyondLastLine: false,
        automaticLayout: true,
      }}
    />
  )
}
