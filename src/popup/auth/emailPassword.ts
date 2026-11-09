import type { AuthError, Session, SupabaseClient } from '@supabase/supabase-js';

export type EmailSignInResult = 'authenticated' | 'email-unverified';
export type EmailSignUpResult = 'authenticated' | 'verification-pending';

export type EmailAuthFailureKind = 'login' | 'signup' | 'missing-session' | 'resend' | 'rate-limited' | 'sign-out' | 'network';

export class EmailAuthFailure extends Error {
  constructor(
    readonly kind: EmailAuthFailureKind,
    message: string,
    readonly code?: string,
    readonly status?: number,
    readonly returnedUser = false,
    readonly returnedSession = false,
  ) {
    super(message);
    this.name = 'EmailAuthFailure';
  }
}

const GENERIC_LOGIN_ERROR = 'Email or password was not accepted. Check your details and try again.';

interface SafeAuthErrorMetadata {
  code?: string;
  status?: number;
}

function safeAuthErrorMetadata(error: unknown): SafeAuthErrorMetadata {
  if (typeof error !== 'object' || error === null) return {};
  const candidate = error as { code?: unknown; status?: unknown };
  const code = typeof candidate.code === 'string' && /^[a-z][a-z0-9_]{0,63}$/.test(candidate.code)
    ? candidate.code
    : undefined;
  const status = typeof candidate.status === 'number' && Number.isInteger(candidate.status)
    && candidate.status >= 100 && candidate.status <= 599
    ? candidate.status
    : undefined;
  return { code, status };
}

export function classifySignupFailure(
  error: unknown,
  response?: { user?: unknown; session?: unknown },
): EmailAuthFailure {
  const metadata = safeAuthErrorMetadata(error);
  const returnedUser = response?.user != null;
  const returnedSession = response?.session != null;
  const code = metadata.code;

  switch (code) {
    case 'email_address_not_authorized':
      return new EmailAuthFailure('signup', 'Supabase could not send a confirmation email to this address. With the built-in email service, only addresses belonging to the Supabase project organization are allowed. No account sign-in was completed.', code, metadata.status, returnedUser, returnedSession);
    case 'over_email_send_rate_limit':
      return new EmailAuthFailure('rate-limited', 'Supabase’s email sending limit has been reached. Wait before trying to create the account again.', code, metadata.status, returnedUser, returnedSession);
    case 'over_request_rate_limit':
      return new EmailAuthFailure('rate-limited', 'Too many signup requests were made. Wait a few minutes before trying again.', code, metadata.status, returnedUser, returnedSession);
    case 'weak_password':
      return new EmailAuthFailure('signup', 'Choose a stronger password that meets the account security requirements.', code, metadata.status, returnedUser, returnedSession);
    case 'user_already_exists':
    case 'email_exists':
      return new EmailAuthFailure('signup', 'An account may already exist for this email. Try signing in instead.', code, metadata.status, returnedUser, returnedSession);
    case 'signup_disabled':
    case 'email_provider_disabled':
      return new EmailAuthFailure('signup', 'Email signup is currently unavailable. Please contact StreetMap support.', code, metadata.status, returnedUser, returnedSession);
    case 'unexpected_failure':
      return new EmailAuthFailure('signup', 'Supabase could not complete signup because of a temporary service problem. Please try again later.', code, metadata.status, returnedUser, returnedSession);
    default:
      if (metadata.status === 429) {
        return new EmailAuthFailure('rate-limited', 'Too many signup requests were made. Wait a few minutes before trying again.', code, metadata.status, returnedUser, returnedSession);
      }
      if (isNetworkFailure(error)) {
        return new EmailAuthFailure('network', 'StreetMap could not reach Supabase. Check your internet connection and try again.', code, metadata.status, returnedUser, returnedSession);
      }
      return new EmailAuthFailure('signup', 'Unable to create the account. Check your details and try again.', code, metadata.status, returnedUser, returnedSession);
  }
}

function isNetworkFailure(error: unknown): boolean {
  if (!(error instanceof Error)) return false;
  return error.name === 'AuthRetryableFetchError' || error.name === 'TypeError'
    || /network|fetch failed|failed to fetch|load failed/i.test(error.message);
}

export async function signInWithEmailUsing(
  client: SupabaseClient,
  email: string,
  password: string,
): Promise<EmailSignInResult> {
  let response;
  try {
    response = await client.auth.signInWithPassword({ email, password });
  } catch {
    throw new EmailAuthFailure('login', GENERIC_LOGIN_ERROR);
  }
  const { data, error } = response;
  if (error?.code === 'email_not_confirmed') return 'email-unverified';
  if (error || !data.session) throw new EmailAuthFailure('login', GENERIC_LOGIN_ERROR);
  return 'authenticated';
}

export async function signUpWithEmailUsing(
  client: SupabaseClient,
  email: string,
  password: string,
): Promise<EmailSignUpResult> {
  // Deliberately omit emailRedirectTo. Supabase verifies the confirmation link
  // and redirects to the project's configured Site URL; users then return to
  // the extension and sign in. The email link does not restore this session.
  let response;
  try {
    response = await client.auth.signUp({ email, password });
  } catch (cause) {
    throw classifySignupFailure(cause);
  }
  const { data, error } = response;
  if (error) throw classifySignupFailure(error, data);
  if (!data.session && data.user) return 'verification-pending';
  if (!data.session) {
    throw new EmailAuthFailure('signup', 'Supabase did not confirm that an account was created. Please try again.');
  }

  const session = await readSession(client);
  if (!session) throw new EmailAuthFailure('missing-session', 'The account was created, but StreetMap could not confirm an active session. Please sign in.');
  return 'authenticated';
}

export async function resendSignupConfirmationUsing(client: SupabaseClient, email: string): Promise<void> {
  let error;
  try {
    ({ error } = await client.auth.resend({ type: 'signup', email }));
  } catch {
    throw new EmailAuthFailure('resend', 'Unable to resend the confirmation email. Please try again later.');
  }
  if (!error) return;
  if (isRateLimitError(error)) {
    throw new EmailAuthFailure('rate-limited', 'Too many confirmation email requests. Please wait before requesting another email.');
  }
  throw new EmailAuthFailure('resend', 'Unable to resend the confirmation email. Please try again later.');
}

function isRateLimitError(error: AuthError): boolean {
  return error.status === 429 || /rate.?limit|too many requests/i.test(error.code ?? '');
}

async function readSession(client: SupabaseClient): Promise<Session | null> {
  try {
    const { data, error } = await client.auth.getSession();
    if (error) return null;
    return data.session;
  } catch {
    return null;
  }
}

export async function signOutLocally(client: SupabaseClient): Promise<void> {
  let error: AuthError | null;
  let requestFailed = false;
  try {
    ({ error } = await client.auth.signOut({ scope: 'local' }));
  } catch {
    error = null;
    requestFailed = true;
  }
  if (!error && !requestFailed) return;

  // Supabase clears the local session even when its remote logout request
  // fails. Treat that as a successful local sign-out, but surface an error if
  // the session is still present or cannot be checked.
  try {
    const { data, error: sessionError } = await client.auth.getSession();
    if (!sessionError && !data.session) return;
  } catch {
    // Fall through to the generic UI-safe failure below.
  }
  throw new EmailAuthFailure('sign-out', 'Unable to sign out. Please try again.');
}
