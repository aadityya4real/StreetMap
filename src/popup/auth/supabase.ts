import { createClient, type SupabaseClient } from '@supabase/supabase-js';
import { browser } from 'wxt/browser';
import { readSupabasePublicConfig } from './config';
import { createAuthStorage, restrictAuthStorageToTrustedContexts } from './extensionStorage';
import { createGoogleOAuthFailure, GoogleOAuthFailure, runGoogleOAuthFlowWithLock } from './googleOAuthFlow';

let clientPromise: Promise<SupabaseClient> | null = null;
const buildEnv = import.meta.env as {
  VITE_SUPABASE_URL?: string;
  VITE_SUPABASE_ANON_KEY?: string;
};

export function hasSupabaseConfiguration(): boolean {
  return readSupabasePublicConfig(buildEnv) !== null;
}

export function getSupabaseClient(): Promise<SupabaseClient> {
  if (!clientPromise) {
    clientPromise = createSupabaseClient().catch((error: unknown) => {
      clientPromise = null;
      throw error;
    });
  }
  return clientPromise;
}

async function createSupabaseClient(): Promise<SupabaseClient> {
  const config = readSupabasePublicConfig(buildEnv);
  if (!config) throw new Error('Supabase authentication is not configured for this build.');

  await restrictAuthStorageToTrustedContexts(browser.storage.local);
  return createClient(config.url, config.anonKey, {
    auth: {
      storage: createAuthStorage(browser.storage.local),
      flowType: 'pkce',
      persistSession: true,
      autoRefreshToken: true,
      detectSessionInUrl: false,
    },
  });
}

export async function signInWithGoogle(): Promise<void> {
  let client: SupabaseClient;
  try {
    client = await getSupabaseClient();
  } catch (cause) {
    throw createGoogleOAuthFailure('configuration', cause);
  }

  let redirectUrl: string;
  try {
    redirectUrl = browser.identity.getRedirectURL();
  } catch {
    throw new GoogleOAuthFailure('identity', 'redirect_url_unavailable', 'Chrome could not prepare the extension sign-in callback. Reload StreetMap and try again.');
  }
  if (!navigator.locks) {
    throw new GoogleOAuthFailure('coordination', 'web_locks_unavailable', 'Secure sign-in coordination is unavailable in this Chrome context. Reload the extension in Chrome and try again.');
  }
  // The Supabase Auth server validates Google's OAuth state; PKCE binds its
  // returned code to the verifier stored by this extension's trusted storage.
  try {
    await runGoogleOAuthFlowWithLock({
      request: (name, callback) => navigator.locks.request(name, { mode: 'exclusive' }, callback),
    }, redirectUrl, {
      async getAuthorizationUrl(redirectTo) {
        const { data, error } = await client.auth.signInWithOAuth({
          provider: 'google',
          options: { redirectTo, skipBrowserRedirect: true },
        });
        if (error) throw createGoogleOAuthFailure('authorization', error);
        if (!data.url) throw new GoogleOAuthFailure('authorization', 'authorization_url_missing', 'Supabase did not provide a Google sign-in URL. Check the Google provider configuration and try again.');
        return data.url;
      },
      async launchWebAuthFlow(url) {
        return browser.identity.launchWebAuthFlow({ url, interactive: true });
      },
      async exchangeCode(code) {
        const { error } = await client.auth.exchangeCodeForSession(code);
        if (error) throw createGoogleOAuthFailure('exchange', error);
      },
    });
  } catch (cause) {
    throw createGoogleOAuthFailure('coordination', cause);
  }
}

export async function getCurrentAccessToken(): Promise<string | null> {
  if (!hasSupabaseConfiguration()) return null;
  const { data, error } = await (await getSupabaseClient()).auth.getSession();
  if (error) throw new Error('Unable to refresh the StreetMap session.');
  return data.session?.access_token ?? null;
}
