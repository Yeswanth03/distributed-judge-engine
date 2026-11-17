import { lazy, Suspense, useState, type FormEvent } from 'react'
import { useNavigate, useParams } from 'react-router'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Plus, Upload, X } from 'lucide-react'
import { toast } from 'sonner'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Textarea } from '@/components/ui/textarea'
import { ErrorState, Loading, PageContainer } from '@/components/states'
import { errorMessage } from '@/lib/errors'
import { admin } from '@/lib/endpoints'
import { formatRelative } from '@/lib/format'
import type { AdminProblemView, CaseInput, Difficulty, FunctionParameter, ProblemInput } from '@/lib/types'
import { AdminTabs } from './AdminTabs'

const CodeEditor = lazy(() => import('@/components/CodeEditor'))

// The types Catalog accepts: a base type, optionally one- or two-dimensional.
const TYPES = ['int', 'float', 'bool', 'string'].flatMap((t) => [t, `${t}[]`, `${t}[][]`])

const EMPTY: ProblemInput & { slug: string } = {
  slug: '',
  title: '',
  statement_md: '',
  difficulty: 'easy',
  points: 100,
  time_limit_ms: 1000,
  memory_limit_mb: 256,
  function_name: '',
  parameters: [{ name: '', type: 'int' }],
  return_type: 'int',
  starter_code: 'class Solution:\n    def solve(self):\n        pass\n',
}

export default function AdminProblemEditPage() {
  const { id } = useParams()
  const problem = useQuery({ queryKey: ['admin-problem', id], queryFn: () => admin.get(id!), enabled: !!id })

  if (id && problem.isPending) return <Loading />
  if (id && problem.isError) return <ErrorState error={problem.error} title="Problem not found" />
  return (
    <PageContainer wide>
      <AdminTabs />
      {/* Keyed by id and update time: the form starts from the saved problem after each save. */}
      <ProblemForm key={`${problem.data?.id}-${problem.data?.updated_at}`} existing={problem.data} />
      {problem.data && <Testsets problem={problem.data} />}
    </PageContainer>
  )
}

function ProblemForm({ existing }: { existing?: AdminProblemView }) {
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [form, setForm] = useState<typeof EMPTY>(() =>
    existing
      ? {
          slug: existing.slug,
          title: existing.title,
          statement_md: existing.statement_md,
          difficulty: existing.difficulty,
          points: existing.points,
          time_limit_ms: existing.time_limit_ms,
          memory_limit_mb: existing.memory_limit_mb,
          function_name: existing.function_name ?? '',
          parameters: existing.parameters ?? [{ name: '', type: 'int' }],
          return_type: existing.return_type ?? 'int',
          starter_code: existing.starter_code ?? EMPTY.starter_code,
        }
      : EMPTY,
  )
  // Once published, these are locked by Catalog: verdicts, scores and test cases depend on them.
  const locked = existing?.status === 'published'
  // A problem from before function details existed may still be given them after publishing.
  const functionLocked = locked && existing.function_name !== null

  const save = useMutation({
    mutationFn: async () => {
      const { slug, ...body } = form
      if (existing) {
        await admin.update(existing.id, body)
        return existing.id
      }
      return (await admin.create({ slug, ...body })).id
    },
    onSuccess: (savedId) => {
      queryClient.invalidateQueries({ queryKey: ['admin-problems'] })
      queryClient.invalidateQueries({ queryKey: ['admin-problem', savedId] })
      toast.success('Problem saved.')
      if (!existing) navigate(`/admin/problems/${savedId}`, { replace: true })
    },
    onError: (e) => toast.error(errorMessage(e)),
  })

  const set = <K extends keyof typeof form>(key: K, value: (typeof form)[K]) =>
    setForm((f) => ({ ...f, [key]: value }))
  const setParam = (i: number, p: Partial<FunctionParameter>) =>
    set(
      'parameters',
      form.parameters.map((q, j) => (j === i ? { ...q, ...p } : q)),
    )

  return (
    <form
      className="grid gap-4"
      onSubmit={(e: FormEvent) => {
        e.preventDefault()
        save.mutate()
      }}
    >
      <div className="flex items-center gap-3">
        <h1 className="text-2xl font-semibold">{existing ? existing.title : 'New problem'}</h1>
        {existing && (
          <Badge variant={existing.status === 'published' ? 'default' : 'secondary'}>{existing.status}</Badge>
        )}
        <Button type="submit" className="ml-auto" disabled={save.isPending}>
          {existing ? 'Save changes' : 'Create draft'}
        </Button>
      </div>
      {locked && (
        <p className="text-sm text-muted-foreground">
          Published: difficulty, points, limits and the function are locked. Title and statement can still
          change.
        </p>
      )}

      <Card>
        <CardContent className="grid gap-4 pt-6 md:grid-cols-2">
          <Field label="Title" id="title">
            <Input id="title" required value={form.title} onChange={(e) => set('title', e.target.value)} />
          </Field>
          <Field label="Slug (URL name, set once)" id="slug">
            <Input
              id="slug"
              required
              disabled={!!existing}
              pattern="[a-z0-9]+(-[a-z0-9]+)*"
              placeholder="two-sum"
              value={form.slug}
              onChange={(e) => set('slug', e.target.value)}
            />
          </Field>
          <Field label="Difficulty" id="difficulty">
            <Select
              value={form.difficulty}
              disabled={locked}
              onValueChange={(v) => set('difficulty', v as Difficulty)}
            >
              <SelectTrigger id="difficulty">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="easy">Easy</SelectItem>
                <SelectItem value="medium">Medium</SelectItem>
                <SelectItem value="hard">Hard</SelectItem>
              </SelectContent>
            </Select>
          </Field>
          <div className="grid grid-cols-3 gap-3">
            <NumberField
              label="Points"
              id="points"
              min={1}
              max={1000}
              value={form.points}
              disabled={locked}
              onChange={(v) => set('points', v)}
            />
            <NumberField
              label="CPU ms"
              id="time"
              min={100}
              max={60000}
              value={form.time_limit_ms}
              disabled={locked}
              onChange={(v) => set('time_limit_ms', v)}
            />
            <NumberField
              label="Memory MB"
              id="memory"
              min={16}
              max={1024}
              value={form.memory_limit_mb}
              disabled={locked}
              onChange={(v) => set('memory_limit_mb', v)}
            />
          </div>
          <div className="md:col-span-2">
            <Field label="Statement (Markdown)" id="statement">
              <Textarea
                id="statement"
                required
                className="min-h-48 font-mono text-sm"
                value={form.statement_md}
                onChange={(e) => set('statement_md', e.target.value)}
              />
            </Field>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Function</CardTitle>
          <CardDescription>
            The judge calls this method of class Solution with each test case's arguments and compares the
            return value.
          </CardDescription>
        </CardHeader>
        <CardContent className="grid gap-4">
          <div className="grid gap-4 md:grid-cols-2">
            <Field label="Function name" id="function_name">
              <Input
                id="function_name"
                required
                disabled={functionLocked}
                pattern="[A-Za-z_][A-Za-z0-9_]*"
                placeholder="twoSum"
                value={form.function_name}
                onChange={(e) => set('function_name', e.target.value)}
              />
            </Field>
            <Field label="Return type" id="return_type">
              <TypeSelect
                id="return_type"
                value={form.return_type}
                disabled={functionLocked}
                onChange={(v) => set('return_type', v)}
              />
            </Field>
          </div>
          <div className="grid gap-2">
            <Label>Parameters</Label>
            {form.parameters.map((p, i) => (
              <div key={i} className="flex gap-2">
                <Input
                  aria-label={`Parameter ${i + 1} name`}
                  required
                  disabled={functionLocked}
                  pattern="[A-Za-z_][A-Za-z0-9_]*"
                  placeholder="nums"
                  value={p.name}
                  onChange={(e) => setParam(i, { name: e.target.value })}
                />
                <TypeSelect
                  value={p.type}
                  disabled={functionLocked}
                  onChange={(type) => setParam(i, { type })}
                />
                <Button
                  type="button"
                  variant="ghost"
                  size="icon"
                  aria-label={`Remove parameter ${i + 1}`}
                  disabled={functionLocked || form.parameters.length === 1}
                  onClick={() =>
                    set(
                      'parameters',
                      form.parameters.filter((_, j) => j !== i),
                    )
                  }
                >
                  <X />
                </Button>
              </div>
            ))}
            <Button
              type="button"
              variant="outline"
              size="sm"
              className="w-fit"
              disabled={functionLocked || form.parameters.length >= 10}
              onClick={() => set('parameters', [...form.parameters, { name: '', type: 'int' }])}
            >
              <Plus /> Add parameter
            </Button>
          </div>
          <div className="grid gap-2">
            <Label>Starter code (must define class Solution and the function)</Label>
            <div className="h-56 overflow-hidden rounded-md border">
              <Suspense fallback={<Loading label="Loading editor…" />}>
                <CodeEditor
                  value={form.starter_code}
                  readOnly={functionLocked}
                  onChange={(v) => set('starter_code', v)}
                />
              </Suspense>
            </div>
          </div>
        </CardContent>
      </Card>
    </form>
  )
}

function Field({ label, id, children }: { label: string; id: string; children: React.ReactNode }) {
  return (
    <div className="grid gap-2">
      <Label htmlFor={id}>{label}</Label>
      {children}
    </div>
  )
}

function NumberField(props: {
  label: string
  id: string
  min: number
  max: number
  value: number
  disabled: boolean
  onChange: (v: number) => void
}) {
  return (
    <Field label={props.label} id={props.id}>
      <Input
        id={props.id}
        type="number"
        required
        min={props.min}
        max={props.max}
        step={1}
        disabled={props.disabled}
        value={props.value}
        onChange={(e) => props.onChange(Number(e.target.value))}
      />
    </Field>
  )
}

function TypeSelect({
  id,
  value,
  disabled,
  onChange,
}: {
  id?: string
  value: string
  disabled: boolean
  onChange: (v: string) => void
}) {
  return (
    <Select value={value} disabled={disabled} onValueChange={onChange}>
      <SelectTrigger id={id} className="w-36">
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {TYPES.map((t) => (
          <SelectItem key={t} value={t}>
            {t}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}

// ---- test sets ----

interface Row extends CaseInput {
  key: number
}

let nextKey = 0
const newRow = (c?: Partial<CaseInput>): Row => ({
  key: nextKey++,
  input: c?.input ?? '{}',
  expected_output: c?.expected_output ?? '',
  is_sample: c?.is_sample ?? false,
})

function isJson(text: string) {
  try {
    JSON.parse(text)
    return true
  } catch {
    return false
  }
}

function Testsets({ problem }: { problem: AdminProblemView }) {
  const queryClient = useQueryClient()
  const versions = useQuery({ queryKey: ['testsets', problem.id], queryFn: () => admin.versions(problem.id) })
  const [rows, setRows] = useState<Row[]>(() => [newRow({ is_sample: true })])

  const refresh = () => {
    queryClient.invalidateQueries({ queryKey: ['testsets', problem.id] })
    queryClient.invalidateQueries({ queryKey: ['admin-problem', problem.id] })
    queryClient.invalidateQueries({ queryKey: ['admin-problems'] })
  }

  const upload = useMutation({
    mutationFn: () =>
      admin.upload(
        problem.id,
        rows.map(({ input, expected_output, is_sample }) => ({ input, expected_output, is_sample })),
      ),
    onSuccess: (r) => {
      toast.success(`Uploaded as draft version ${r.version}.`)
      refresh()
    },
    onError: (e) => toast.error(errorMessage(e)),
  })

  const publish = useMutation({
    mutationFn: (version: number) => admin.publish(problem.id, version),
    onSuccess: () => {
      toast.success('Published. The judge now uses this version.')
      refresh()
    },
    onError: (e) => toast.error(errorMessage(e)),
  })

  /** Loads a .json file: an array of {input, expected_output, is_sample}; input and output may be JSON values or strings. */
  async function importFile(file: File) {
    try {
      const data = JSON.parse(await file.text()) as unknown
      const cases = Array.isArray(data) ? data : (data as { cases?: unknown[] }).cases
      if (!Array.isArray(cases)) throw new Error('Expected an array of cases, or {"cases": [...]}.')
      const asText = (v: unknown) => (typeof v === 'string' ? v : JSON.stringify(v))
      setRows(
        cases.map((c) => {
          const o = c as { input: unknown; expected_output: unknown; is_sample?: boolean }
          return newRow({
            input: asText(o.input),
            expected_output: asText(o.expected_output),
            is_sample: !!o.is_sample,
          })
        }),
      )
      toast.success(`Loaded ${cases.length} cases. Check them, then upload.`)
    } catch (e) {
      toast.error(`Could not read the file: ${errorMessage(e)}`)
    }
  }

  const invalid = rows.some((r) => !isJson(r.input) || !isJson(r.expected_output))
  const noSample = !rows.some((r) => r.is_sample)
  const current = problem.current_version ?? 0

  return (
    <Card className="mt-4">
      <CardHeader>
        <CardTitle>Test sets</CardTitle>
        <CardDescription>
          Upload creates a new draft version; publishing makes it current. Published versions never change, so
          to fix a case, upload and publish a new version.
        </CardDescription>
      </CardHeader>
      <CardContent className="grid gap-6">
        {versions.isPending ? (
          <Loading />
        ) : versions.isError ? (
          <ErrorState error={versions.error} />
        ) : versions.data.length > 0 ? (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Version</TableHead>
                <TableHead>Status</TableHead>
                <TableHead className="text-right">Cases</TableHead>
                <TableHead className="text-right">Samples</TableHead>
                <TableHead className="text-right">Uploaded</TableHead>
                <TableHead />
              </TableRow>
            </TableHeader>
            <TableBody>
              {versions.data.map((v) => (
                <TableRow key={v.version}>
                  <TableCell className="tabular-nums">
                    v{v.version} {v.current && <Badge className="ml-2">current</Badge>}
                  </TableCell>
                  <TableCell>{v.status}</TableCell>
                  <TableCell className="text-right tabular-nums">{v.case_count}</TableCell>
                  <TableCell className="text-right tabular-nums">{v.sample_count}</TableCell>
                  <TableCell className="text-right text-muted-foreground">
                    {formatRelative(v.created_at)}
                  </TableCell>
                  <TableCell className="text-right">
                    {v.status === 'draft' && v.version > current && (
                      <Button
                        size="sm"
                        disabled={publish.isPending}
                        onClick={() => publish.mutate(v.version)}
                      >
                        Publish
                      </Button>
                    )}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        ) : (
          <p className="text-sm text-muted-foreground">No test sets yet.</p>
        )}

        <div className="grid gap-3">
          <div className="flex items-center gap-2">
            <h3 className="font-medium">New version</h3>
            <span className="text-sm text-muted-foreground">
              Input: JSON object with{' '}
              {problem.parameters?.map((p) => `${p.name}: ${p.type}`).join(', ') || 'the arguments'} ·
              expected: JSON {problem.return_type ?? 'value'}
            </span>
            <Button variant="outline" size="sm" className="ml-auto" asChild>
              <label>
                <Upload /> Import .json
                <input
                  type="file"
                  accept="application/json,.json"
                  className="hidden"
                  onChange={(e) => {
                    const file = e.target.files?.[0]
                    if (file) importFile(file)
                    e.target.value = ''
                  }}
                />
              </label>
            </Button>
          </div>
          {rows.map((r, i) => (
            <div key={r.key} className="grid gap-2 rounded-md border p-3 md:grid-cols-[1fr_1fr_auto]">
              <Textarea
                aria-label={`Case ${i + 1} input`}
                aria-invalid={!isJson(r.input)}
                className="min-h-16 font-mono text-xs"
                value={r.input}
                onChange={(e) =>
                  setRows(rows.map((x) => (x.key === r.key ? { ...x, input: e.target.value } : x)))
                }
              />
              <Textarea
                aria-label={`Case ${i + 1} expected output`}
                aria-invalid={!isJson(r.expected_output)}
                placeholder="expected output"
                className="min-h-16 font-mono text-xs"
                value={r.expected_output}
                onChange={(e) =>
                  setRows(rows.map((x) => (x.key === r.key ? { ...x, expected_output: e.target.value } : x)))
                }
              />
              <div className="flex items-start gap-2">
                <label className="flex items-center gap-1.5 text-sm">
                  <input
                    type="checkbox"
                    checked={r.is_sample}
                    onChange={(e) =>
                      setRows(rows.map((x) => (x.key === r.key ? { ...x, is_sample: e.target.checked } : x)))
                    }
                  />
                  Sample
                </label>
                <Button
                  variant="ghost"
                  size="icon"
                  aria-label={`Remove case ${i + 1}`}
                  disabled={rows.length === 1}
                  onClick={() => setRows(rows.filter((x) => x.key !== r.key))}
                >
                  <X />
                </Button>
              </div>
            </div>
          ))}
          <div className="flex flex-wrap items-center gap-2">
            <Button
              variant="outline"
              size="sm"
              disabled={rows.length >= 200}
              onClick={() => setRows([...rows, newRow()])}
            >
              <Plus /> Add case
            </Button>
            <span className="text-sm text-muted-foreground">
              {rows.length} cases{invalid && ' · fix the fields marked invalid'}
              {noSample && ' · mark at least one sample'}
            </span>
            <Button
              className="ml-auto"
              disabled={upload.isPending || invalid || noSample || problem.function_name === null}
              onClick={() => upload.mutate()}
            >
              Upload as draft
            </Button>
          </div>
        </div>
      </CardContent>
    </Card>
  )
}
