import type { ReactNode } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Separator } from '@/components/ui/separator'
import { apiUrl } from '@/lib/api'
import { auth } from '@/lib/endpoints'
import { oauthReturnUrl } from '@/lib/oauth'

/** Both providers the User Service supports, always listed so the page looks the same before keys are set. */
const PROVIDERS = [
  { id: 'google', name: 'Google', Logo: GoogleLogo },
  { id: 'github', name: 'GitHub', Logo: GitHubLogo },
] as const

/**
 * Google and GitHub sign-in. A provider works once its client ID and secret are set for the User Service
 * (GOOGLE_* / GITHUB_* in the repo's .env); until then its button is shown disabled.
 */
export function OAuthButtons() {
  const { data, isPending } = useQuery({
    queryKey: ['oauth-providers'],
    queryFn: auth.providers,
    staleTime: 60_000,
  })
  const enabled = new Set(data?.providers ?? [])
  const anyMissing = !isPending && PROVIDERS.some((p) => !enabled.has(p.id))
  return (
    <>
      <div className="flex items-center gap-3 text-xs text-muted-foreground">
        <Separator className="flex-1" /> or <Separator className="flex-1" />
      </div>
      <div className="grid gap-2">
        {PROVIDERS.map(({ id, name, Logo }) =>
          enabled.has(id) ? (
            <Button key={id} variant="outline" asChild>
              {/* A full-page navigation: the User Service redirects on to the provider. */}
              <a
                href={apiUrl(`/auth/oauth/${id}/start?redirect_uri=${encodeURIComponent(oauthReturnUrl())}`)}
              >
                <Logo /> Continue with {name}
              </a>
            </Button>
          ) : (
            <Button key={id} variant="outline" disabled>
              <Logo /> Continue with {name}
            </Button>
          ),
        )}
      </div>
      {anyMissing && (
        <p className="text-center text-xs text-muted-foreground">
          {PROVIDERS.filter((p) => !enabled.has(p.id))
            .map((p) => p.name)
            .join(' and ')}{' '}
          sign-in isn&apos;t set up on this server yet.
        </p>
      )}
    </>
  )
}

function GoogleLogo() {
  return (
    <svg viewBox="0 0 24 24" aria-hidden="true" className="size-4">
      <path
        fill="#4285F4"
        d="M23.52 12.27c0-.85-.08-1.67-.22-2.45H12v4.63h6.46a5.52 5.52 0 0 1-2.4 3.62v3h3.88c2.27-2.09 3.58-5.17 3.58-8.8Z"
      />
      <path
        fill="#34A853"
        d="M12 24c3.24 0 5.96-1.07 7.94-2.9l-3.88-3.01c-1.07.72-2.45 1.15-4.06 1.15-3.12 0-5.77-2.11-6.71-4.95H1.28v3.11A12 12 0 0 0 12 24Z"
      />
      <path fill="#FBBC05" d="M5.29 14.29a7.2 7.2 0 0 1 0-4.58V6.6H1.28a12 12 0 0 0 0 10.8l4.01-3.11Z" />
      <path
        fill="#EA4335"
        d="M12 4.75c1.76 0 3.34.61 4.59 1.8l3.44-3.44A11.97 11.97 0 0 0 12 0 12 12 0 0 0 1.28 6.6l4.01 3.11C6.23 6.86 8.88 4.75 12 4.75Z"
      />
    </svg>
  )
}

function GitHubLogo() {
  return (
    <svg viewBox="0 0 24 24" aria-hidden="true" className="size-4" fill="currentColor">
      <path d="M12 .3a12 12 0 0 0-3.8 23.38c.6.12.82-.26.82-.58l-.01-2.04c-3.34.73-4.04-1.6-4.04-1.6-.55-1.4-1.34-1.77-1.34-1.77-1.09-.74.08-.73.08-.73 1.2.09 1.84 1.24 1.84 1.24 1.07 1.83 2.8 1.3 3.49 1 .1-.78.42-1.31.76-1.61-2.67-.3-5.47-1.33-5.47-5.93 0-1.31.47-2.38 1.24-3.22-.13-.3-.54-1.52.12-3.17 0 0 1-.32 3.3 1.23a11.5 11.5 0 0 1 6 0c2.28-1.55 3.29-1.23 3.29-1.23.66 1.65.24 2.87.12 3.17.77.84 1.23 1.91 1.23 3.22 0 4.61-2.8 5.63-5.48 5.92.43.37.82 1.1.82 2.22l-.01 3.29c0 .32.21.7.83.58A12 12 0 0 0 12 .3Z" />
    </svg>
  )
}

export function AuthCard({
  title,
  description,
  children,
}: {
  title: string
  description: ReactNode
  children: ReactNode
}) {
  return (
    <div className="flex flex-1 items-start justify-center p-6 pt-16">
      <Card className="w-full max-w-sm">
        <CardHeader>
          <CardTitle className="text-xl">{title}</CardTitle>
          <CardDescription>{description}</CardDescription>
        </CardHeader>
        <CardContent className="grid gap-4">{children}</CardContent>
      </Card>
    </div>
  )
}
