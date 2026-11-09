import { useEffect, useState } from 'react';
import { observeAuthState } from '../auth/authStateObserver';
import { hasSupabaseConfiguration } from '../auth/supabase';
import { readCurrentUser, subscribeToAuth } from '../auth/authService';

import type { AuthUserSummary } from '../auth/authService';

export interface AuthState {
  status: 'loading' | 'unconfigured' | 'anonymous' | 'authenticated' | 'error';
  user: AuthUserSummary | null;
}

export function useAuth(): AuthState {
  const [state, setState] = useState<AuthState>(() => ({
    status: hasSupabaseConfiguration() ? 'loading' : 'unconfigured',
    user: null,
  }));

  useEffect(() => {
    if (!hasSupabaseConfiguration()) return;
    return observeAuthState({ readCurrentUser, subscribeToAuth }, setState);
  }, []);

  return state;
}
