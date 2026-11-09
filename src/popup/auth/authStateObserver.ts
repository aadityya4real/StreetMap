import type { AuthListener, AuthUserSummary } from './authService';

export interface AuthStateSource {
  readCurrentUser(): Promise<AuthUserSummary | null>;
  subscribeToAuth(listener: AuthListener): Promise<() => void>;
}

export type AuthStateSnapshot =
  | { status: 'anonymous'; user: null }
  | { status: 'authenticated'; user: AuthUserSummary }
  | { status: 'error'; user: null };

export function observeAuthState(
  source: AuthStateSource,
  publish: (state: AuthStateSnapshot) => void,
): () => void {
  let active = true;
  let changedByEvent = false;
  let unsubscribe: (() => void) | undefined;
  const publishUser = (user: AuthUserSummary | null) => {
    publish(user ? { status: 'authenticated', user } : { status: 'anonymous', user: null });
  };

  void source.subscribeToAuth((user) => {
    changedByEvent = true;
    if (active) publishUser(user);
  }).then((dispose) => {
    if (active) unsubscribe = dispose;
    else dispose();
  }).catch(() => {
    if (active && !changedByEvent) publish({ status: 'error', user: null });
  });

  void source.readCurrentUser().then((user) => {
    if (active && !changedByEvent) publishUser(user);
  }).catch(() => {
    if (active && !changedByEvent) publish({ status: 'error', user: null });
  });

  return () => {
    active = false;
    unsubscribe?.();
  };
}
