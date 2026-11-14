const dateTime = new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' })

export function formatDateTime(iso: string) {
  return dateTime.format(new Date(iso))
}

export function formatRelative(iso: string) {
  const seconds = Math.round((Date.now() - new Date(iso).getTime()) / 1000)
  if (seconds < 60) return 'just now'
  if (seconds < 3600) return `${Math.floor(seconds / 60)} min ago`
  if (seconds < 86400) return `${Math.floor(seconds / 3600)} h ago`
  return formatDateTime(iso)
}

export function formatMemory(kb: number | null) {
  if (kb === null) return '—'
  return kb >= 1024 ? `${(kb / 1024).toFixed(1)} MB` : `${kb} KB`
}

/** Shows a JSON case value readably; falls back to the raw text when it isn't valid JSON. */
export function prettyJson(text: string | null) {
  if (text === null) return ''
  try {
    return JSON.stringify(JSON.parse(text), null, 2)
  } catch {
    return text
  }
}

/** Shows each argument of a JSON input object on its own line: "nums = [2,7]". */
export function formatArguments(input: string) {
  try {
    const args = JSON.parse(input) as Record<string, unknown>
    return Object.entries(args)
      .map(([k, v]) => `${k} = ${JSON.stringify(v)}`)
      .join('\n')
  } catch {
    return input
  }
}

export function shortId(id: string) {
  return id.slice(0, 8)
}
