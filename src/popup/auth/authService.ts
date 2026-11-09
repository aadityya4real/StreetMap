import type { AuthChangeEvent, Session } from '@supabase/supabase-js';
import {
  resendSignupConfirmationUsing,
  signInWithEmailUsing,
  signOutLocally,
  signUpWithEmailUsing,
  type EmailSignInResult,
  type EmailSignUpResult,
} from './emailPassword';
import { getSupabaseClient, hasSupabaseConfiguration, signInWithGoogle } from './supabase';

export interface AuthUserSummary {
  id: string;
  email: string | null;
}

export type AuthListener = (user: AuthUserSummary | null) => void;

function userSummary(session: Session | null): AuthUserSummary | null {
  if (!session?.user) return null;
  return { id: session.user.id, email: session.user.email ?? null };
}

export async function readCurrentUser(): Promise<AuthUserSummary | null> {
  if (!hasSupabaseConfiguration()) return null;
  const { data, error } = await (await getSupabaseClient()).auth.getSession();
  if (error) throw new Error('Unable to read the StreetMap session.');
  return userSummary(data.session);
}

export async function subscribeToAuth(listener: AuthListener): Promise<() => void> {
  if (!hasSupabaseConfiguration()) return () => undefined;
  const client = await getSupabaseClient();
  const { data } = client.auth.onAuthStateChange((_event: AuthChangeEvent, session) => {
    listener(userSummary(session));
  });
  return () => data.subscription.unsubscribe();
}

export async function signInWithEmail(email: string, password: string): Promise<EmailSignInResult> {
  return signInWithEmailUsing(await getSupabaseClient(), email, password);
}

export async function signUpWithEmail(email: string, password: string): Promise<EmailSignUpResult> {
  return signUpWithEmailUsing(await getSupabaseClient(), email, password);
}

export async function resendSignupConfirmation(email: string): Promise<void> {
  return resendSignupConfirmationUsing(await getSupabaseClient(), email);
}

export async function signOut(): Promise<void> {
  return signOutLocally(await getSupabaseClient());
}

export { signInWithGoogle };
