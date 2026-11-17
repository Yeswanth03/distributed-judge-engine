import type { ReactNode } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Separator } from '@/components/ui/separator'
import { apiUrl } from '@/lib/api'
import { auth } from '@/lib/endpoints'
import { oauthReturnUrl } from '@/lib/oauth'

const PROVIDER_NAMES: Record<string, string> = { google: 'Google', github: 'GitHub' }

/** Buttons for the providers the User Service has enabled; nothing when none are configured. */
export function OAuthButtons() {
  const { data } = useQuery({ queryKey: ['oauth-providers'], queryFn: auth.providers, staleTime: Infinity })
  const providers = data?.providers ?? []
  if (providers.length === 0) return null
  return (
    <>
      <div className="flex items-center gap-3 text-xs text-muted-foreground">
        <Separator className="flex-1" /> or <Separator className="flex-1" />
      </div>
      <div className="grid gap-2">
        {providers.map((p) => (
          <Button key={p} variant="outline" asChild>
            {/* A full-page navigation: the User Service redirects on to the provider. */}
            <a href={apiUrl(`/auth/oauth/${p}/start?redirect_uri=${encodeURIComponent(oauthReturnUrl())}`)}>
              Continue with {PROVIDER_NAMES[p] ?? p}
            </a>
          </Button>
        ))}
      </div>
    </>
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
