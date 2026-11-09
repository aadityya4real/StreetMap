export interface SupabasePublicConfig {
  url: string;
  anonKey: string;
}

export interface SupabasePublicEnvironment {
  VITE_SUPABASE_URL?: string;
  VITE_SUPABASE_ANON_KEY?: string;
}

export function isSupabaseProjectUrl(value: string): boolean {
  try {
    const url = new URL(value);
    const hostname = url.hostname.toLowerCase();
    const canonicalOrigin = `https://${hostname}`;
    return url.protocol === 'https:'
      && url.username === ''
      && url.password === ''
      && url.port === ''
      && url.pathname === '/'
      && url.search === ''
      && url.hash === ''
      && /^[a-z0-9]{20}\.supabase\.co$/.test(hostname)
      && (value === canonicalOrigin || value === `${canonicalOrigin}/`);
  } catch {
    return false;
  }
}

export function readSupabasePublicConfig(env: SupabasePublicEnvironment): SupabasePublicConfig | null {
  const url = env.VITE_SUPABASE_URL?.trim() ?? '';
  const anonKey = env.VITE_SUPABASE_ANON_KEY?.trim() ?? '';
  if (!isSupabaseProjectUrl(url) || !isSupabasePublicKey(anonKey)) return null;
  return { url: url.replace(/\/$/, ''), anonKey };
}

/** Fail the build before Vite creates client assets from an unsafe VITE_ key. */
export function validateSupabaseBuildConfig(env: SupabasePublicEnvironment): void {
  const url = env.VITE_SUPABASE_URL?.trim() ?? '';
  const key = env.VITE_SUPABASE_ANON_KEY?.trim() ?? '';

  if (key && !isSupabasePublicKey(key)) {
    throw new Error('VITE_SUPABASE_ANON_KEY must be a Supabase anon or publishable key. Secret keys are not allowed.');
  }
  if (url && !isSupabaseProjectUrl(url)) {
    throw new Error('VITE_SUPABASE_URL must be the HTTPS origin of a standard Supabase project.');
  }
  if (key && !url) {
    throw new Error('VITE_SUPABASE_URL is required when a public Supabase key is configured.');
  }
}

export function isSupabasePublicKey(key: string): boolean {
  if (!key || /^sb_(?:secret|service[-_]role)_/i.test(key)) return false;

  if (key.startsWith('sb_publishable_')) {
    return /^[A-Za-z0-9_-]+$/.test(key.slice('sb_publishable_'.length));
  }

  // Supabase's legacy public anon keys are JWTs with an explicit anon role.
  const parts = key.split('.');
  if (parts.length !== 3 || parts.some((part) => !/^[A-Za-z0-9_-]+$/.test(part))) {
    return false;
  }

  try {
    const header = decodeBase64UrlJson(parts[0]);
    const payload = decodeBase64UrlJson(parts[1]);
    return isRecord(header) && isRecord(payload) && payload.role === 'anon';
  } catch {
    return false;
  }
}

function decodeBase64UrlJson(segment: string): unknown {
  const encoded = segment.replace(/-/g, '+').replace(/_/g, '/');
  const padded = encoded.padEnd(Math.ceil(encoded.length / 4) * 4, '=');
  const bytes = Uint8Array.from(atob(padded), (character) => character.charCodeAt(0));
  return JSON.parse(new TextDecoder().decode(bytes)) as unknown;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}
