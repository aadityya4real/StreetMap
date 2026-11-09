export type GoogleOAuthPhase = 'coordination' | 'configuration' | 'authorization' | 'identity' | 'callback' | 'exchange';

export class GoogleOAuthFailure extends Error {
  constructor(
    readonly phase: GoogleOAuthPhase,
    readonly code: string,
    message: string,
    readonly status?: number,
    readonly callbackLocation?: 'query' | 'fragment',
  ) {
    super(message);
    this.name = 'GoogleOAuthFailure';
  }

  get diagnosticReference(): string {
    const location = this.callbackLocation ? `/${this.callbackLocation}` : '';
    return `${this.phase}${location}/${this.code}${this.status ? `/http_${this.status}` : ''}`;
  }
}

export interface GoogleOAuthPorts {
  getAuthorizationUrl(redirectUrl: string): Promise<string>;
  launchWebAuthFlow(url: string): Promise<string | undefined>;
  exchangeCode(code: string): Promise<void>;
}

export interface OAuthFlowLock {
  request<T>(name: string, callback: () => Promise<T>): Promise<T>;
}

const GOOGLE_OAUTH_LOCK_NAME = 'streetmap-google-oauth-pkce';
const SAFE_CODE_PATTERN = /^[a-z][a-z0-9_.-]{0,63}$/;

let flowInProgress = false;

export function createGoogleOAuthFailure(phase: GoogleOAuthPhase, cause: unknown): GoogleOAuthFailure {
  if (cause instanceof GoogleOAuthFailure) return cause;

  const metadata = readSafeErrorMetadata(cause);
  if (isUserCancellation(cause)) {
    return new GoogleOAuthFailure(phase, 'user_cancelled', 'Google sign-in was cancelled. You can try again.', metadata.status);
  }
  if (isNetworkFailure(cause)) {
    return new GoogleOAuthFailure(phase, 'network_error', 'StreetMap could not reach Supabase during Google sign-in. Check your connection and try again.', metadata.status);
  }

  const code = normalizeErrorCode(metadata.code, metadata.name, phase);
  const message = failureMessage(phase, code);
  return new GoogleOAuthFailure(phase, code, message, metadata.status);
}

export async function runGoogleOAuthFlow(
  redirectUrl: string,
  ports: GoogleOAuthPorts,
): Promise<void> {
  if (flowInProgress) {
    throw new GoogleOAuthFailure('coordination', 'flow_in_progress', 'A Google sign-in is already in progress. Wait for it to finish, then try again.');
  }
  flowInProgress = true;
  try {
    let authorizationUrl: string;
    try {
      authorizationUrl = await ports.getAuthorizationUrl(redirectUrl);
    } catch (cause) {
      throw createGoogleOAuthFailure('authorization', cause);
    }

    let responseUrl: string | undefined;
    try {
      responseUrl = await ports.launchWebAuthFlow(authorizationUrl);
    } catch (cause) {
      throw createGoogleOAuthFailure('identity', cause);
    }
    if (!responseUrl) {
      throw new GoogleOAuthFailure('identity', 'user_cancelled', 'Google sign-in was cancelled. You can try again.');
    }

    const callback = parseCallback(responseUrl, redirectUrl);
    const providerError = getCallbackError(callback);
    if (providerError) {
      const code = normalizeProviderErrorCode(providerError.code);
      throw new GoogleOAuthFailure('callback', code, failureMessage('callback', code), undefined, providerError.location);
    }
    const code = callback.searchParams.get('code');
    if (!code) {
      throw new GoogleOAuthFailure('callback', 'authorization_code_missing', failureMessage('callback', 'authorization_code_missing'));
    }

    try {
      await ports.exchangeCode(code);
    } catch (cause) {
      throw createGoogleOAuthFailure('exchange', cause);
    }
  } finally {
    flowInProgress = false;
  }
}

export function runGoogleOAuthFlowWithLock(
  lock: OAuthFlowLock,
  redirectUrl: string,
  ports: GoogleOAuthPorts,
): Promise<void> {
  return lock.request(GOOGLE_OAUTH_LOCK_NAME, () => runGoogleOAuthFlow(redirectUrl, ports));
}

function parseCallback(responseUrl: string, redirectUrl: string): URL {
  let callback: URL;
  let expected: URL;
  try {
    callback = new URL(responseUrl);
    expected = new URL(redirectUrl);
  } catch {
    throw new GoogleOAuthFailure('callback', 'invalid_callback_url', failureMessage('callback', 'invalid_callback_url'));
  }
  if (callback.origin !== expected.origin || callback.pathname !== expected.pathname) {
    throw new GoogleOAuthFailure('callback', 'invalid_callback_origin_or_path', failureMessage('callback', 'invalid_callback_origin_or_path'));
  }
  return callback;
}

function getCallbackError(callback: URL): { code: string; location: 'query' | 'fragment' } | null {
  const queryError = callback.searchParams.get('error_code') ?? callback.searchParams.get('error');
  if (queryError) return { code: queryError, location: 'query' };
  if (!callback.hash) return null;

  // Supabase may return OAuth errors in a fragment. Never display the
  // provider-supplied description or any other callback parameter.
  const fragment = new URLSearchParams(callback.hash.slice(1));
  const fragmentError = fragment.get('error_code') ?? fragment.get('error');
  return fragmentError ? { code: fragmentError, location: 'fragment' } : null;
}

function normalizeProviderErrorCode(value: string): string {
  const normalized = value.trim().toLowerCase();
  if (SAFE_CODE_PATTERN.test(normalized)) return normalized;
  return 'oauth_provider_error';
}

function normalizeErrorCode(code: string | undefined, name: string | undefined, phase: GoogleOAuthPhase): string {
  if (code && SAFE_CODE_PATTERN.test(code)) return code;
  if (phase === 'exchange') {
    if (name === 'AuthPKCECodeVerifierMissingError') return 'pkce_verifier_missing';
    if (name === 'AuthPKCEGrantCodeExchangeError') return 'pkce_code_exchange_failed';
  }
  return phase === 'identity' ? 'chrome_identity_error' : `${phase}_error`;
}

function readSafeErrorMetadata(error: unknown): { code?: string; name?: string; status?: number } {
  if (typeof error !== 'object' || error === null) return {};
  const candidate = error as { code?: unknown; name?: unknown; status?: unknown };
  const code = typeof candidate.code === 'string' && SAFE_CODE_PATTERN.test(candidate.code)
    ? candidate.code
    : undefined;
  const name = typeof candidate.name === 'string' && /^[A-Za-z][A-Za-z0-9]{0,63}$/.test(candidate.name)
    ? candidate.name
    : undefined;
  const status = typeof candidate.status === 'number' && Number.isInteger(candidate.status)
    && candidate.status >= 100 && candidate.status <= 599
    ? candidate.status
    : undefined;
  return { code, name, status };
}

function isUserCancellation(error: unknown): boolean {
  if (!(error instanceof Error)) return false;
  return error.name === 'AbortError'
    || /user (?:cancelled|canceled|did not approve)|(?:cancelled|canceled) by user/i.test(error.message);
}

function isNetworkFailure(error: unknown): boolean {
  if (!(error instanceof Error)) return false;
  return error.name === 'AuthRetryableFetchError' || error.name === 'TypeError'
    || /network|fetch failed|failed to fetch|load failed/i.test(error.message);
}

function failureMessage(phase: GoogleOAuthPhase, code: string): string {
  if (code === 'user_cancelled' || code === 'access_denied') {
    return 'Google sign-in was cancelled or not authorized. Try again and complete the sign-in prompt.';
  }
  if (code === 'network_error') {
    return 'StreetMap could not reach Supabase during Google sign-in. Check your connection and try again.';
  }
  if (phase === 'coordination') return 'Another Google sign-in is already running. Wait for it to finish, then try again.';
  if (phase === 'configuration') return 'StreetMap could not prepare Google sign-in. Check the extension configuration and try again.';
  if (phase === 'authorization') {
    if (code === 'provider_disabled') return 'Google sign-in is disabled in Supabase. Enable the Google provider and try again.';
    return 'Supabase could not start Google sign-in. Check the Google provider and redirect configuration, then try again.';
  }
  if (phase === 'identity') return 'Chrome could not open the Google sign-in window. Check the extension identity permission and try again.';
  if (phase === 'callback') {
    if (code === 'authorization_code_missing') return 'Google sign-in returned no authorization code. Restart sign-in and try again.';
    if (code === 'invalid_callback_url' || code === 'invalid_callback_origin_or_path') return 'Google sign-in returned to an unexpected extension callback. Check the installed extension ID and Supabase redirect allowlist.';
    if (code === 'unexpected_failure') return 'Supabase Auth reported an unexpected failure while processing Google sign-in. Check the Supabase Auth logs around this time for the underlying cause, then try again.';
    return 'Google or Supabase returned an OAuth error. Check the provider and redirect settings, then try again.';
  }
  if (phase === 'exchange') {
    if (code === 'bad_code_verifier' || code === 'pkce_verifier_missing' || code === 'pkce_code_verifier_not_found' || code === 'pkce_code_exchange_failed' || code === 'flow_state_expired' || code === 'flow_state_not_found') {
      return 'StreetMap could not verify this sign-in attempt. Start Google sign-in again in the same Chrome profile.';
    }
    return 'Supabase could not complete Google sign-in. Start a new sign-in attempt and try again.';
  }
  return 'Google sign-in could not be completed. Please try again.';
}
