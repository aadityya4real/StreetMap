import assert from 'node:assert/strict';
import { Buffer } from 'node:buffer';
import { URL } from 'node:url';
import { setImmediate as immediate } from 'node:timers/promises';
import test from 'node:test';
import { createClient } from '@supabase/supabase-js';

import {
  isSupabaseProjectUrl,
  isSupabasePublicKey,
  readSupabasePublicConfig,
  validateSupabaseBuildConfig,
} from '../src/popup/auth/config.ts';
import { createAuthStorage, restrictAuthStorageToTrustedContexts } from '../src/popup/auth/extensionStorage.ts';
import {
  resendSignupConfirmationUsing,
  classifySignupFailure,
  signInWithEmailUsing,
  signOutLocally,
  signUpWithEmailUsing,
} from '../src/popup/auth/emailPassword.ts';
import { observeAuthState } from '../src/popup/auth/authStateObserver.ts';
import { GoogleOAuthFailure, runGoogleOAuthFlow, runGoogleOAuthFlowWithLock } from '../src/popup/auth/googleOAuthFlow.ts';

function fakeResponse(status, payload) {
  return {
    status,
    ok: status >= 200 && status < 300,
    headers: { get: () => 'application/json' },
    async text() { return JSON.stringify(payload); },
    async json() { return payload; },
  };
}

async function expectOAuthFailure(promise, phase, code) {
  await assert.rejects(promise, (error) => {
    assert.ok(error instanceof GoogleOAuthFailure);
    assert.equal(error.phase, phase);
    assert.equal(error.code, code);
    return true;
  });
}

test('accepts a canonical Supabase project URL and public key', () => {
  assert.deepEqual(readSupabasePublicConfig({
    VITE_SUPABASE_URL: 'https://abcdefghijklmnopqrst.supabase.co',
    VITE_SUPABASE_ANON_KEY: 'sb_publishable_test-key',
  }), {
    url: 'https://abcdefghijklmnopqrst.supabase.co',
    anonKey: 'sb_publishable_test-key',
  });
});

test('accepts a valid legacy Supabase anon JWT key', () => {
  const header = Buffer.from(JSON.stringify({ alg: 'HS256', typ: 'JWT' })).toString('base64url');
  const payload = Buffer.from(JSON.stringify({ role: 'anon' })).toString('base64url');
  assert.equal(isSupabasePublicKey(`${header}.${payload}.c3ludGhldGljLXNpZ25hdHVyZQ`), true);
});

test('rejects unsafe Supabase key formats and malformed JWT values', () => {
  const header = Buffer.from(JSON.stringify({ alg: 'HS256', typ: 'JWT' })).toString('base64url');
  const servicePayload = Buffer.from(JSON.stringify({ role: 'service_role' })).toString('base64url');
  assert.equal(isSupabasePublicKey('sb_secret_synthetic-test-key'), false);
  assert.equal(isSupabasePublicKey('sb_service_role_synthetic-test-key'), false);
  assert.equal(isSupabasePublicKey(`${header}.${servicePayload}.c3ludGhldGljLXNpZ25hdHVyZQ`), false);
  assert.equal(isSupabasePublicKey(`@@.${servicePayload}.c3ludGhldGljLXNpZ25hdHVyZQ`), false);
  assert.equal(isSupabasePublicKey(`${header}.not-base64!.c3ludGhldGljLXNpZ25hdHVyZQ`), false);
  assert.equal(isSupabasePublicKey('not-a-recognized-supabase-key'), false);
});

test('permits missing public credentials while rejecting invalid configured URLs', () => {
  assert.equal(readSupabasePublicConfig({}), null);
  assert.equal(readSupabasePublicConfig({ VITE_SUPABASE_URL: 'https://abcdefghijklmnopqrst.supabase.co' }), null);
  assert.doesNotThrow(() => validateSupabaseBuildConfig({}));
  assert.doesNotThrow(() => validateSupabaseBuildConfig({ VITE_SUPABASE_URL: 'https://abcdefghijklmnopqrst.supabase.co' }));
  assert.equal(isSupabaseProjectUrl('http://abcdefghijklmnopqrst.supabase.co'), false);
  assert.equal(isSupabaseProjectUrl('https://supabase.co.attacker.example'), false);
  assert.equal(isSupabaseProjectUrl('https://user:password@abcdefghijklmnopqrst.supabase.co'), false);
  assert.equal(isSupabaseProjectUrl('https://abcdefghijklmnopqrst.supabase.co:8443'), false);
  assert.equal(isSupabaseProjectUrl('https://abcdefghijklmnopqrst.supabase.co:443'), false);
  assert.equal(isSupabaseProjectUrl('https://abcdefghijklmnopqrst.supabase.co/auth/v1'), false);
  assert.equal(isSupabaseProjectUrl('https://abcdefghijklmnopqrst.supabase.co/?next=other'), false);
  assert.equal(isSupabaseProjectUrl('https://abcdefghijklmnopqrst.supabase.co/#fragment'), false);
  assert.equal(isSupabaseProjectUrl('https://short.supabase.co'), false);
});

test('build validation rejects secret key material without revealing the value', () => {
  const header = Buffer.from(JSON.stringify({ alg: 'HS256', typ: 'JWT' })).toString('base64url');
  const payload = Buffer.from(JSON.stringify({ role: 'service_role' })).toString('base64url');
  const secret = `${header}.${payload}.c3ludGhldGljLXNpZ25hdHVyZQ`;
  for (const key of ['sb_secret_synthetic-test-key', 'sb_service_role_synthetic-test-key', secret]) {
    assert.equal(readSupabasePublicConfig({
      VITE_SUPABASE_URL: 'https://abcdefghijklmnopqrst.supabase.co',
      VITE_SUPABASE_ANON_KEY: key,
    }), null);
    assert.throws(() => validateSupabaseBuildConfig({
      VITE_SUPABASE_URL: 'https://abcdefghijklmnopqrst.supabase.co',
      VITE_SUPABASE_ANON_KEY: key,
    }), (error) => {
      assert.match(error.message, /anon or publishable key/);
      assert.equal(error.message.includes(key), false);
      return true;
    });
  }
});

test('build validation rejects malformed or arbitrary URLs before accepting a key', () => {
  assert.throws(() => validateSupabaseBuildConfig({
    VITE_SUPABASE_URL: 'http://abcdefghijklmnopqrst.supabase.co',
    VITE_SUPABASE_ANON_KEY: 'sb_publishable_test-key',
  }), /VITE_SUPABASE_URL must be/);
  assert.throws(() => validateSupabaseBuildConfig({
    VITE_SUPABASE_URL: 'https://api.attacker.example',
    VITE_SUPABASE_ANON_KEY: 'sb_publishable_test-key',
  }), /VITE_SUPABASE_URL must be/);
});

test('Supabase session storage delegates only string values to extension local storage', async () => {
  const values = new Map();
  const storage = createAuthStorage({
    async get(key) { return values.has(key) ? { [key]: values.get(key) } : {}; },
    async set(items) { for (const [key, value] of Object.entries(items)) values.set(key, value); },
    async remove(key) { values.delete(key); },
  });

  assert.equal(await storage.getItem('auth-key'), null);
  await storage.setItem('auth-key', '{"access_token":"kept in extension storage"}');
  assert.equal(await storage.getItem('auth-key'), '{"access_token":"kept in extension storage"}');
  values.set('non-string', { access_token: 'not a serialized session' });
  assert.equal(await storage.getItem('non-string'), null);
  await storage.removeItem('auth-key');
  assert.equal(await storage.getItem('auth-key'), null);
});

test('session storage is explicitly restricted to trusted extension contexts', async () => {
  let configured;
  await restrictAuthStorageToTrustedContexts({
    async setAccessLevel(options) { configured = options; },
  });
  assert.deepEqual(configured, { accessLevel: 'TRUSTED_CONTEXTS' });
  await assert.rejects(restrictAuthStorageToTrustedContexts({
    async setAccessLevel() { throw new Error('unavailable'); },
  }), /unavailable/);
});

test('the configured Supabase SDK creates an S256 PKCE challenge and stores its verifier', async () => {
  const values = new Map();
  const storage = createAuthStorage({
    async get(key) { return values.has(key) ? { [key]: values.get(key) } : {}; },
    async set(items) { for (const [key, value] of Object.entries(items)) values.set(key, value); },
    async remove(key) { values.delete(key); },
  });
  const client = createClient('https://abcdefghijklmnopqrst.supabase.co', 'sb_publishable_test-key', {
    auth: {
      storage,
      flowType: 'pkce',
      persistSession: true,
      autoRefreshToken: false,
      detectSessionInUrl: false,
    },
  });
  const callback = 'https://installed-extension.chromiumapp.org/';
  const { data, error } = await client.auth.signInWithOAuth({
    provider: 'google',
    options: { redirectTo: callback, skipBrowserRedirect: true },
  });

  assert.equal(error, null);
  assert.ok(data.url);
  const authorizationUrl = new URL(data.url);
  assert.equal(authorizationUrl.searchParams.get('redirect_to'), callback);
  assert.equal(authorizationUrl.searchParams.get('code_challenge_method'), 's256');
  assert.match(authorizationUrl.searchParams.get('code_challenge') ?? '', /^[A-Za-z0-9_-]{43}$/);
  assert.ok([...values.keys()].some((key) => key.endsWith('-code-verifier')));
  assert.equal(authorizationUrl.searchParams.has('access_token'), false);
  await client.auth.dispose();
});

test('expired sessions exchange the stored refresh token and persist its rotation', async () => {
  const values = new Map();
  const storage = createAuthStorage({
    async get(key) { return values.has(key) ? { [key]: values.get(key) } : {}; },
    async set(items) { for (const [key, value] of Object.entries(items)) values.set(key, value); },
    async remove(key) { values.delete(key); },
  });
  const storageKey = 'streetmap-auth-lifecycle-test';
  const user = {
    id: 'synthetic-user-id',
    aud: 'authenticated',
    app_metadata: {},
    user_metadata: {},
    created_at: new Date().toISOString(),
  };
  const expiredAccessToken = `header.${Buffer.from(JSON.stringify({ exp: 1 })).toString('base64url')}.signature`;
  values.set(storageKey, JSON.stringify({
    access_token: expiredAccessToken,
    token_type: 'bearer',
    expires_in: 3600,
    expires_at: 1,
    refresh_token: 'synthetic-refresh-token',
    user,
  }));
  let refreshCalls = 0;
  let requestRefreshToken;
  const client = createClient('https://abcdefghijklmnopqrst.supabase.co', 'sb_publishable_test-key', {
    auth: { storage, storageKey, flowType: 'pkce', persistSession: true, autoRefreshToken: true, detectSessionInUrl: false },
    global: {
      async fetch(input, init) {
        refreshCalls += 1;
        const request = new URL(input.toString());
        assert.match(request.pathname, /\/token$/);
        assert.equal(request.searchParams.get('grant_type'), 'refresh_token');
        requestRefreshToken = JSON.parse(String(init?.body)).refresh_token;
        return fakeResponse(200, {
          access_token: `header.${Buffer.from(JSON.stringify({ exp: Math.floor(Date.now() / 1000) + 3600 })).toString('base64url')}.signature`,
          token_type: 'bearer',
          expires_in: 3600,
          refresh_token: 'synthetic-rotated-refresh-token',
          user,
        });
      },
    },
  });

  const { data, error } = await client.auth.getSession();
  assert.equal(error, null);
  assert.equal(refreshCalls, 1);
  assert.equal(requestRefreshToken, 'synthetic-refresh-token');
  assert.equal(data.session?.refresh_token, 'synthetic-rotated-refresh-token');
  assert.equal(JSON.parse(values.get(storageKey)).refresh_token, 'synthetic-rotated-refresh-token');
  await client.auth.dispose();
});

test('an expired session is not represented as authenticated after refresh failure', async () => {
  const values = new Map();
  const storage = createAuthStorage({
    async get(key) { return values.has(key) ? { [key]: values.get(key) } : {}; },
    async set(items) { for (const [key, value] of Object.entries(items)) values.set(key, value); },
    async remove(key) { values.delete(key); },
  });
  const storageKey = 'streetmap-auth-refresh-failure-test';
  values.set(storageKey, JSON.stringify({
    access_token: 'header.eyJleHAiOjF9.signature',
    token_type: 'bearer',
    expires_in: 3600,
    expires_at: 1,
    refresh_token: 'synthetic-refresh-token',
    user: { id: 'synthetic-user-id', aud: 'authenticated', app_metadata: {}, user_metadata: {}, created_at: new Date().toISOString() },
  }));
  const client = createClient('https://abcdefghijklmnopqrst.supabase.co', 'sb_publishable_test-key', {
    auth: { storage, storageKey, flowType: 'pkce', persistSession: true, autoRefreshToken: true, detectSessionInUrl: false },
    global: {
      async fetch() {
        return fakeResponse(400, { error: 'invalid_grant', error_description: 'synthetic refresh rejected' });
      },
    },
  });

  const { data } = await client.auth.getSession();
  assert.equal(data.session, null);
  assert.equal(values.has(storageKey), false);
  await client.auth.dispose();
});

test('local sign-out clears the stored session even if the remote logout endpoint is unavailable', async () => {
  const values = new Map();
  const storage = createAuthStorage({
    async get(key) { return values.has(key) ? { [key]: values.get(key) } : {}; },
    async set(items) { for (const [key, value] of Object.entries(items)) values.set(key, value); },
    async remove(key) { values.delete(key); },
  });
  const storageKey = 'streetmap-auth-local-signout-test';
  const user = { id: 'synthetic-user-id', aud: 'authenticated', app_metadata: {}, user_metadata: {}, created_at: new Date().toISOString() };
  values.set(storageKey, JSON.stringify({
    access_token: 'synthetic-access-token',
    token_type: 'bearer',
    expires_in: 3600,
    expires_at: Math.floor(Date.now() / 1000) + 3600,
    refresh_token: 'synthetic-refresh-token',
    user,
  }));
  const client = createClient('https://abcdefghijklmnopqrst.supabase.co', 'sb_publishable_test-key', {
    auth: { storage, storageKey, persistSession: true, autoRefreshToken: false, detectSessionInUrl: false },
    global: {
      async fetch(input) {
        const request = new URL(input.toString());
        assert.equal(request.searchParams.get('scope'), 'local');
        return fakeResponse(503, { error: 'synthetic_network_error' });
      },
    },
  });

  await signOutLocally(client);
  assert.equal(values.has(storageKey), false);
  await client.auth.dispose();
});

test('session restoration publishes persisted state and observes sign-out', async () => {
  const states = [];
  let listener;
  let unsubscribed = false;
  const user = { id: 'internal-test-id', email: 'reader@example.test' };
  const dispose = observeAuthState({
    async readCurrentUser() { return user; },
    async subscribeToAuth(onChange) { listener = onChange; return () => { unsubscribed = true; }; },
  }, (state) => states.push(state));

  await immediate();
  assert.deepEqual(states.at(-1), { status: 'authenticated', user });
  listener(null);
  assert.deepEqual(states.at(-1), { status: 'anonymous', user: null });
  dispose();
  assert.equal(unsubscribed, true);
  assert.deepEqual(Object.keys(states[0].user).sort(), ['email', 'id']);
});

test('a late restore failure cannot overwrite a newer auth event', async () => {
  let rejectRestore;
  let listener;
  const states = [];
  const restore = new Promise((_resolve, reject) => { rejectRestore = reject; });
  const dispose = observeAuthState({
    readCurrentUser() { return restore; },
    async subscribeToAuth(onChange) { listener = onChange; return () => undefined; },
  }, (state) => states.push(state));

  listener({ id: 'internal-test-id', email: null });
  rejectRestore(new Error('stale restore failure'));
  await immediate();
  assert.equal(states.at(-1).status, 'authenticated');
  dispose();
});

test('unmounted auth observers ignore late restoration results', async () => {
  let resolveRestore;
  const states = [];
  const restore = new Promise((resolve) => { resolveRestore = resolve; });
  const dispose = observeAuthState({
    readCurrentUser() { return restore; },
    async subscribeToAuth() { return () => undefined; },
  }, (state) => states.push(state));
  dispose();
  resolveRestore({ id: 'internal-test-id', email: null });
  await immediate();
  assert.deepEqual(states, []);
});

test('email login and signup use Supabase, including confirmation-required signup', async () => {
  const calls = [];
  const session = { user: { email: 'reader@example.test' } };
  const client = {
    auth: {
      async signInWithPassword(credentials) { calls.push(['login', credentials]); return { data: { session }, error: null }; },
      async signUp(credentials) { calls.push(['signup', credentials]); return { data: { session: null, user: { identities: [{}] } }, error: null }; },
      async getSession() { return { data: { session }, error: null }; },
    },
  };
  assert.equal(await signInWithEmailUsing(client, 'reader@example.test', 'example-password'), 'authenticated');
  assert.equal(await signUpWithEmailUsing(client, 'reader@example.test', 'example-password'), 'verification-pending');
  assert.deepEqual(calls.map(([method]) => method), ['login', 'signup']);
});

test('signup with an immediate session checks current session before treating the account as authenticated', async () => {
  const session = { user: { email: 'reader@example.test' } };
  const user = { id: 'synthetic-user', email: 'reader@example.test' };
  let signupCredentials;
  let sessionReads = 0;
  const client = { auth: {
    async signUp(credentials) { signupCredentials = credentials; return { data: { user, session }, error: null }; },
    async getSession() { sessionReads += 1; return { data: { session }, error: null }; },
  } };
  assert.equal(await signUpWithEmailUsing(client, 'reader@example.test', 'synthetic-password'), 'authenticated');
  assert.deepEqual(signupCredentials, { email: 'reader@example.test', password: 'synthetic-password' });
  assert.equal(sessionReads, 1);
});

test('signup cannot report authenticated when Supabase returned a session but current session is absent', async () => {
  const user = { id: 'synthetic-user', email: 'reader@example.test' };
  const client = { auth: {
    async signUp() { return { data: { user, session: { user: { email: 'reader@example.test' } } }, error: null }; },
    async getSession() { return { data: { session: null }, error: null }; },
  } };
  await assert.rejects(signUpWithEmailUsing(client, 'reader@example.test', 'synthetic-password'), /could not confirm an active session/);
});

test('signup with neither a user nor session does not claim verification email was sent', async () => {
  const client = { auth: {
    async signUp() { return { data: { user: null, session: null }, error: null }; },
  } };
  await assert.rejects(signUpWithEmailUsing(client, 'reader@example.test', 'synthetic-password'), /did not confirm that an account was created/);
});

test('signup errors preserve safe Supabase code and status while mapping known failures to actionable messages', () => {
  const cases = [
    ['email_address_not_authorized', 403, /built-in email service.*organization/i, 'signup'],
    ['over_email_send_rate_limit', 429, /email sending limit.*wait/i, 'rate-limited'],
    ['over_request_rate_limit', 429, /Too many signup requests.*few minutes/i, 'rate-limited'],
    ['weak_password', 422, /stronger password/i, 'signup'],
    ['user_already_exists', 422, /account may already exist.*signing in/i, 'signup'],
    ['unexpected_failure', 500, /temporary service problem/i, 'signup'],
  ];
  for (const [code, status, message, kind] of cases) {
    const failure = classifySignupFailure({ code, status, message: 'private provider body must not escape' });
    assert.equal(failure.code, code);
    assert.equal(failure.status, status);
    assert.equal(failure.kind, kind);
    assert.match(failure.message, message);
    assert.doesNotMatch(failure.message, /private provider body/);
  }
});

test('signup network failure is classified without exposing the underlying message', async () => {
  const client = { auth: { async signUp() { throw new TypeError('Failed to fetch private-url?token=secret'); } } };
  await assert.rejects(signUpWithEmailUsing(client, 'reader@example.test', 'synthetic-password'), (failure) => {
    assert.equal(failure.kind, 'network');
    assert.match(failure.message, /internet connection/);
    assert.doesNotMatch(failure.message, /private-url|secret/);
    return true;
  });
});

test('signup error response records returned user/session but never treats error as success', async () => {
  const client = { auth: {
    async signUp() {
      return { data: { user: { id: 'synthetic-user' }, session: { access_token: 'synthetic-token' } }, error: { code: 'email_address_not_authorized', status: 403 } };
    },
  } };
  await assert.rejects(signUpWithEmailUsing(client, 'reader@example.test', 'synthetic-password'), (failure) => {
    assert.equal(failure.code, 'email_address_not_authorized');
    assert.equal(failure.status, 403);
    assert.equal(failure.returnedUser, true);
    assert.equal(failure.returnedSession, true);
    assert.match(failure.message, /could not send a confirmation email/);
    return true;
  });
});

test('malformed provider error metadata is not retained or displayed', () => {
  const failure = classifySignupFailure({ code: 'unexpected provider text <script>', status: 999, message: 'sensitive body' });
  assert.equal(failure.code, undefined);
  assert.equal(failure.status, undefined);
  assert.doesNotMatch(failure.message, /sensitive|script/);
});

test('unverified login offers a distinct safe result without treating the user as signed in', async () => {
  const client = { auth: {
    async signInWithPassword() { return { data: { session: null }, error: { code: 'email_not_confirmed' } }; },
  } };
  assert.equal(await signInWithEmailUsing(client, 'reader@example.test', 'synthetic-password'), 'email-unverified');
});

test('signup requiring confirmation does not exchange an OTP or create a local session', async () => {
  const client = { auth: {
    async signUp() { return { data: { user: { id: 'synthetic-user' }, session: null }, error: null }; },
    async verifyOtp() { assert.fail('confirmation link flow must not verify an OTP in the extension'); },
    async getSession() { return { data: { session: null }, error: null }; },
  } };
  assert.equal(await signUpWithEmailUsing(client, 'reader@example.test', 'synthetic-password'), 'verification-pending');
});

test('signup confirmation resend uses Supabase signup resend without an extension redirect', async () => {
  let request;
  await resendSignupConfirmationUsing({ auth: {
    async resend(params) { request = params; return { data: { user: null, session: null }, error: null }; },
  } }, 'reader@example.test');
  assert.deepEqual(request, { type: 'signup', email: 'reader@example.test' });
});

test('signup confirmation resend maps rate limits without exposing provider errors', async () => {
  const client = { auth: {
    async resend() { return { data: { user: null, session: null }, error: { code: 'over_email_send_rate_limit', status: 429, message: 'private provider detail' } }; },
  } };
  await assert.rejects(resendSignupConfirmationUsing(client, 'reader@example.test'), (error) => {
    assert.match(error.message, /Too many confirmation email requests/);
    assert.equal(error.message.includes('private provider detail'), false);
    return true;
  });
});

test('email auth reports generic failures and signs out only the local session', async () => {
  let signOutScope;
  const failingClient = { auth: {
    async signInWithPassword() { return { error: new Error('provider detail') }; },
    async signUp() { return { data: { session: null }, error: new Error('provider detail') }; },
  } };
  await assert.rejects(signInWithEmailUsing(failingClient, 'reader@example.test', 'bad'), /Email or password was not accepted/);
  await assert.rejects(signUpWithEmailUsing(failingClient, 'reader@example.test', 'bad'), /Unable to create the account/);
  await signOutLocally({ auth: { async signOut(options) { signOutScope = options.scope; return { error: null }; } } });
  assert.equal(signOutScope, 'local');
});

test('Google OAuth requests an extension callback and exchanges its returned code', async () => {
  const calls = [];
  const redirectUrl = 'https://extension-id.chromiumapp.org/';
  await runGoogleOAuthFlow(redirectUrl, {
    async getAuthorizationUrl(redirect) {
      calls.push(['authorize', redirect]);
      return 'https://abcdefghijklmnopqrst.supabase.co/auth/v1/authorize?provider=google';
    },
    async launchWebAuthFlow(url) {
      calls.push(['launch', url]);
      return `${redirectUrl}?code=one-time-code`;
    },
    async exchangeCode(code) { calls.push(['exchange', code]); },
  });
  assert.deepEqual(calls, [
    ['authorize', redirectUrl],
    ['launch', 'https://abcdefghijklmnopqrst.supabase.co/auth/v1/authorize?provider=google'],
    ['exchange', 'one-time-code'],
  ]);
});

test('Google OAuth diagnoses invalid callback origin, path, and missing code without exposing callback data', async () => {
  const ports = {
    async getAuthorizationUrl() { return 'https://auth.example/authorize'; },
    async launchWebAuthFlow() { return 'https://attacker.example/?code=not-accepted'; },
    async exchangeCode() { assert.fail('unexpected code exchange'); },
  };
  await expectOAuthFailure(runGoogleOAuthFlow('https://extension-id.chromiumapp.org/', ports), 'callback', 'invalid_callback_origin_or_path');

  await expectOAuthFailure(runGoogleOAuthFlow('https://extension-id.chromiumapp.org/', {
    ...ports,
    async launchWebAuthFlow() { return 'https://extension-id.chromiumapp.org/'; },
  }), 'callback', 'authorization_code_missing');

  await expectOAuthFailure(runGoogleOAuthFlow('https://extension-id.chromiumapp.org/', {
    ...ports,
    async launchWebAuthFlow() { return 'https://extension-id.chromiumapp.org/other/?code=not-accepted'; },
  }), 'callback', 'invalid_callback_origin_or_path');

  await expectOAuthFailure(runGoogleOAuthFlow('not-a-url', ports), 'callback', 'invalid_callback_url');
});

test('Google OAuth classifies provider errors from query and fragment without showing descriptions', async () => {
  const base = {
    async getAuthorizationUrl() { return 'https://auth.example/authorize'; },
    async exchangeCode() { assert.fail('unexpected code exchange'); },
  };
  await expectOAuthFailure(runGoogleOAuthFlow('https://extension-id.chromiumapp.org/', {
    ...base,
    async launchWebAuthFlow() { return 'https://extension-id.chromiumapp.org/?error=access_denied&error_description=private-token'; },
  }), 'callback', 'access_denied');

  await expectOAuthFailure(runGoogleOAuthFlow('https://extension-id.chromiumapp.org/', {
    ...base,
    async launchWebAuthFlow() { return 'https://extension-id.chromiumapp.org/#error_code=temporarily_unavailable&error_description=private-token'; },
  }), 'callback', 'temporarily_unavailable');

  await assert.rejects(runGoogleOAuthFlow('https://extension-id.chromiumapp.org/', {
    ...base,
    async launchWebAuthFlow() { return 'https://extension-id.chromiumapp.org/?error_code=unexpected_failure&error_description=private-server-details'; },
  }), (error) => {
    assert.ok(error instanceof GoogleOAuthFailure);
    assert.equal(error.phase, 'callback');
    assert.equal(error.code, 'unexpected_failure');
    assert.equal(error.callbackLocation, 'query');
    assert.equal(error.diagnosticReference, 'callback/query/unexpected_failure');
    assert.match(error.message, /Supabase Auth logs/);
    assert.doesNotMatch(error.message, /private-server-details|extension-id|error_code/);
    return true;
  });

  await assert.rejects(runGoogleOAuthFlow('https://extension-id.chromiumapp.org/', {
    ...base,
    async launchWebAuthFlow() { return 'https://extension-id.chromiumapp.org/#error=unexpected_failure&error_description=private-server-details'; },
  }), (error) => {
    assert.ok(error instanceof GoogleOAuthFailure);
    assert.equal(error.callbackLocation, 'fragment');
    assert.equal(error.diagnosticReference, 'callback/fragment/unexpected_failure');
    assert.doesNotMatch(error.message, /private-server-details|extension-id|error=/);
    return true;
  });

  await expectOAuthFailure(runGoogleOAuthFlow('https://extension-id.chromiumapp.org/', {
    ...base,
    async launchWebAuthFlow() { return undefined; },
  }), 'identity', 'user_cancelled');
});

test('Google OAuth classifies URL creation, Chrome identity, and network failures safely', async () => {
  await expectOAuthFailure(runGoogleOAuthFlow('https://extension-id.chromiumapp.org/', {
    async getAuthorizationUrl() { throw Object.assign(new Error('secret detail'), { code: 'provider_disabled', status: 400 }); },
    async launchWebAuthFlow() { assert.fail('should not open identity flow'); },
    async exchangeCode() { assert.fail('should not exchange'); },
  }), 'authorization', 'provider_disabled');

  await assert.rejects(runGoogleOAuthFlow('https://extension-id.chromiumapp.org/', {
    async getAuthorizationUrl() { return 'https://auth.example/authorize'; },
    async launchWebAuthFlow() { throw new Error('The user did not approve access. access_token=never-show'); },
    async exchangeCode() { assert.fail('should not exchange'); },
  }), (error) => {
    assert.ok(error instanceof GoogleOAuthFailure);
    assert.equal(error.phase, 'identity');
    assert.equal(error.code, 'user_cancelled');
    assert.doesNotMatch(error.message, /access_token|never-show/);
    return true;
  });

  await assert.rejects(runGoogleOAuthFlow('https://extension-id.chromiumapp.org/', {
    async getAuthorizationUrl() { return 'https://auth.example/authorize'; },
    async launchWebAuthFlow() { return 'https://extension-id.chromiumapp.org/?code=synthetic-code'; },
    async exchangeCode() { throw new TypeError('Failed to fetch private-url?secret=never-show'); },
  }), (error) => {
    assert.ok(error instanceof GoogleOAuthFailure);
    assert.equal(error.phase, 'exchange');
    assert.equal(error.code, 'network_error');
    assert.doesNotMatch(error.message, /private-url|never-show/);
    return true;
  });
});

test('Google OAuth diagnoses PKCE verifier and authorization code exchange errors safely', async () => {
  const ports = {
    async getAuthorizationUrl() { return 'https://auth.example/authorize'; },
    async launchWebAuthFlow() { return 'https://extension-id.chromiumapp.org/?code=synthetic-code'; },
    async exchangeCode() { throw Object.assign(new Error('private verifier detail'), { code: 'bad_code_verifier', status: 400 }); },
  };
  await assert.rejects(runGoogleOAuthFlow('https://extension-id.chromiumapp.org/', ports), (error) => {
    assert.ok(error instanceof GoogleOAuthFailure);
    assert.equal(error.phase, 'exchange');
    assert.equal(error.code, 'bad_code_verifier');
    assert.equal(error.status, 400);
    assert.match(error.message, /same Chrome profile/);
    assert.doesNotMatch(error.message, /private verifier detail|synthetic-code/);
    return true;
  });

  await assert.rejects(runGoogleOAuthFlow('https://extension-id.chromiumapp.org/', {
    ...ports,
    async exchangeCode() { throw Object.assign(new Error('private verifier detail'), { name: 'AuthPKCECodeVerifierMissingError', code: 'pkce_code_verifier_not_found', status: 400 }); },
  }), (error) => {
    assert.ok(error instanceof GoogleOAuthFailure);
    assert.equal(error.phase, 'exchange');
    assert.equal(error.code, 'pkce_code_verifier_not_found');
    assert.match(error.message, /same Chrome profile/);
    return true;
  });
});

test('concurrent Google OAuth requests are rejected while one flow is active', async () => {
  let releaseLaunch;
  let didLaunch;
  const launched = new Promise((resolve) => { didLaunch = resolve; });
  const launchGate = new Promise((resolve) => { releaseLaunch = resolve; });
  let authorizations = 0;
  const ports = {
    async getAuthorizationUrl() { authorizations += 1; return 'https://auth.example/authorize'; },
    async launchWebAuthFlow() {
      didLaunch();
      await launchGate;
      return 'https://extension-id.chromiumapp.org/?code=one';
    },
    async exchangeCode() {},
  };

  const first = runGoogleOAuthFlow('https://extension-id.chromiumapp.org/', ports);
  await launched;
  await assert.rejects(runGoogleOAuthFlow('https://extension-id.chromiumapp.org/', ports), /already in progress/);
  releaseLaunch();
  await first;
  assert.equal(authorizations, 1);
});

test('Google PKCE flows in separate popup contexts share an exclusive Web Lock', async () => {
  let queue = Promise.resolve();
  const requestedNames = [];
  const lock = {
    request(name, callback) {
      requestedNames.push(name);
      const task = queue.then(callback);
      queue = task.catch(() => undefined);
      return task;
    },
  };
  let releaseFirst;
  let firstLaunched;
  const firstReady = new Promise((resolve) => { firstLaunched = resolve; });
  const firstGate = new Promise((resolve) => { releaseFirst = resolve; });
  const exchanged = [];
  let authorizationCount = 0;
  const firstPorts = {
    async getAuthorizationUrl() { authorizationCount += 1; return 'https://auth.example/first'; },
    async launchWebAuthFlow() { firstLaunched(); await firstGate; return 'https://extension.chromiumapp.org/?code=first'; },
    async exchangeCode(code) { exchanged.push(code); },
  };
  const secondPorts = {
    async getAuthorizationUrl() { authorizationCount += 1; return 'https://auth.example/second'; },
    async launchWebAuthFlow() { return 'https://extension.chromiumapp.org/?code=second'; },
    async exchangeCode(code) { exchanged.push(code); },
  };
  const redirect = 'https://extension.chromiumapp.org/';
  const first = runGoogleOAuthFlowWithLock(lock, redirect, firstPorts);
  await firstReady;
  let secondCompleted = false;
  const second = runGoogleOAuthFlowWithLock(lock, redirect, secondPorts).then(() => { secondCompleted = true; });
  await immediate();
  assert.equal(authorizationCount, 1);
  assert.equal(secondCompleted, false);
  releaseFirst();
  await Promise.all([first, second]);
  assert.deepEqual(exchanged, ['first', 'second']);
  assert.equal(authorizationCount, 2);
  assert.deepEqual(requestedNames, ['streetmap-google-oauth-pkce', 'streetmap-google-oauth-pkce']);
});
