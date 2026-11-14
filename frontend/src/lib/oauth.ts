/** Where Google/GitHub sign-in returns: this app's /login, whose origin must be in OAUTH_ALLOWED_REDIRECTS. */
export function oauthReturnUrl() {
  return `${window.location.origin}/login`
}

export const OAUTH_ERRORS: Record<string, string> = {
  cancelled: 'Sign-in was cancelled.',
  provider_error: 'The provider could not sign you in. Try again.',
  email_not_verified: 'That account has no verified email address.',
  email_in_use: 'An account with this email already exists. Sign in with your password instead.',
  account_disabled: 'This account is disabled.',
}
