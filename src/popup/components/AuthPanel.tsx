import { useEffect, useRef, useState, type FormEvent } from 'react';
import {
  resendSignupOtp,
  signInWithEmail,
  signInWithGoogle,
  signOut,
  signUpWithEmail,
  verifySignupOtp,
} from '../auth/authService';
import { EmailAuthFailure, type EmailSignInResult, type EmailSignUpResult } from '../auth/emailPassword';
import { GoogleOAuthFailure } from '../auth/googleOAuthFlow';
import type { AuthState } from '../hooks/useAuth';

export interface AuthPanelActions {
  signInWithEmail(email: string, password: string): Promise<EmailSignInResult>;
  signUpWithEmail(email: string, password: string): Promise<EmailSignUpResult>;
  verifySignupOtp(email: string, token: string): Promise<void>;
  resendSignupOtp(email: string): Promise<void>;
  signInWithGoogle(): Promise<void>;
  signOut(): Promise<void>;
}

interface AuthPanelProps {
  state: AuthState;
  onClose: () => void;
  actions?: AuthPanelActions;
}

type AuthMode = 'sign-in' | 'sign-up';
type AuthView = 'form' | 'verification' | 'verified';

export const RESEND_COOLDOWN_SECONDS = 30;
const RATE_LIMIT_COOLDOWN_SECONDS = 60;

function emailErrorMessage(cause: unknown, fallback: string): string {
  return cause instanceof EmailAuthFailure ? cause.message : fallback;
}

function emailErrorReference(cause: unknown): string {
  if (!(cause instanceof EmailAuthFailure) || !cause.code) return '';
  return ` Reference: ${cause.code}${cause.status ? ` (HTTP ${cause.status})` : ''}.`;
}

function googleOAuthErrorMessage(cause: unknown): string {
  if (!(cause instanceof GoogleOAuthFailure)) return 'Google sign-in could not be completed. Please try again.';
  return `${cause.message} Diagnostic: ${cause.diagnosticReference}.`;
}

const defaultActions: AuthPanelActions = {
  signInWithEmail,
  signUpWithEmail,
  verifySignupOtp,
  resendSignupOtp,
  signInWithGoogle,
  signOut,
};

export function maskEmail(email: string): string {
  const separator = email.lastIndexOf('@');
  if (separator < 1) return 'your email address';
  const local = email.slice(0, separator);
  const domain = email.slice(separator + 1);
  const prefix = local.slice(0, 1);
  const suffix = local.length > 2 ? local.slice(-1) : '';
  const stars = '*'.repeat(Math.max(2, Math.min(6, local.length - prefix.length - suffix.length)));
  return `${prefix}${stars}${suffix}@${domain}`;
}

export default function AuthPanel({ state, onClose, actions = defaultActions }: AuthPanelProps) {
  const panelRef = useRef<HTMLElement>(null);
  const closeButtonRef = useRef<HTMLButtonElement>(null);
  const busyRef = useRef(false);
  const [mode, setMode] = useState<AuthMode>('sign-in');
  const [view, setView] = useState<AuthView>('form');
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [showPassword, setShowPassword] = useState(false);
  const [pendingEmail, setPendingEmail] = useState('');
  const [otp, setOtp] = useState('');
  const [message, setMessage] = useState('');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [resendCooldown, setResendCooldown] = useState(0);

  useEffect(() => {
    const previousFocus = document.activeElement;
    closeButtonRef.current?.focus();
    function handleKeyDown(event: KeyboardEvent) {
      if (event.key === 'Escape') {
        event.preventDefault();
        onClose();
        return;
      }
      if (event.key !== 'Tab') return;
      const focusable = panelRef.current?.querySelectorAll<HTMLElement>(
        'button:not(:disabled), input:not(:disabled), [href], [tabindex]:not([tabindex="-1"])',
      );
      if (!focusable?.length) return;
      const first = focusable[0];
      const last = focusable[focusable.length - 1];
      if (event.shiftKey && document.activeElement === first) {
        event.preventDefault();
        last.focus();
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault();
        first.focus();
      }
    }
    document.addEventListener('keydown', handleKeyDown);
    return () => {
      document.removeEventListener('keydown', handleKeyDown);
      if (previousFocus instanceof HTMLElement) previousFocus.focus();
    };
  }, [onClose]);

  useEffect(() => {
    if (resendCooldown <= 0) return undefined;
    const timer = window.setTimeout(() => setResendCooldown((seconds) => Math.max(0, seconds - 1)), 1000);
    return () => window.clearTimeout(timer);
  }, [resendCooldown]);

  async function runExclusive(action: () => Promise<void>) {
    if (busyRef.current) return;
    busyRef.current = true;
    setBusy(true);
    try {
      await action();
    } finally {
      busyRef.current = false;
      setBusy(false);
    }
  }

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError('');
    setMessage('');
    await runExclusive(async () => {
      const normalizedEmail = email.trim();
      try {
        if (mode === 'sign-in') {
          const result = await actions.signInWithEmail(normalizedEmail, password);
          if (result === 'email-unverified') {
            setPendingEmail(normalizedEmail);
            setView('verification');
            setOtp('');
            setMessage('Email confirmation is still required. Enter the code from your signup email, or request a new one.');
          } else {
            onClose();
          }
          return;
        }

        const result = await actions.signUpWithEmail(normalizedEmail, password);
        if (result === 'authenticated') {
          onClose();
          return;
        }

        setPendingEmail(normalizedEmail);
        setPassword('');
        setOtp('');
        setView('verification');
        setResendCooldown(RESEND_COOLDOWN_SECONDS);
        setMessage(`Supabase accepted your signup request. If this address is eligible for signup, we've sent a verification code to ${maskEmail(normalizedEmail)}. Check your inbox and spam folder.`);
      } catch (cause) {
        setError(`${emailErrorMessage(cause, 'Authentication could not be completed. Please try again.')}${emailErrorReference(cause)}`);
      }
    });
  }

  async function handleGoogle() {
    setError('');
    setMessage('');
    await runExclusive(async () => {
      try {
        await actions.signInWithGoogle();
        onClose();
      } catch (cause) {
        setError(googleOAuthErrorMessage(cause));
      }
    });
  }

  async function handleVerify(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError('');
    setMessage('');
    await runExclusive(async () => {
      try {
        await actions.verifySignupOtp(pendingEmail, otp.trim());
        setView('verified');
        setOtp('');
        setMessage('Email verified. Your StreetMap account is signed in.');
      } catch (cause) {
        setError(emailErrorMessage(cause, 'The code could not be verified. Check it and try again.'));
      }
    });
  }

  async function handleResend() {
    if (resendCooldown > 0) return;
    setError('');
    setMessage('');
    await runExclusive(async () => {
      try {
        await actions.resendSignupOtp(pendingEmail || email.trim());
        setResendCooldown(RESEND_COOLDOWN_SECONDS);
        setView('verification');
        setMessage('If a signup is pending for this address, a new verification code has been sent. Check your inbox and spam folder.');
      } catch (cause) {
        const safeMessage = emailErrorMessage(cause, 'Unable to resend the verification code. Please try again later.');
        if (/too many verification requests/i.test(safeMessage)) setResendCooldown(RATE_LIMIT_COOLDOWN_SECONDS);
        setError(safeMessage);
      }
    });
  }

  async function handleSignOut() {
    setError('');
    setMessage('');
    await runExclusive(async () => {
      try {
        await actions.signOut();
        setView('form');
        setMessage('You have signed out.');
      } catch (cause) {
        setError(emailErrorMessage(cause, 'Sign-out could not be completed. Please try again.'));
      }
    });
  }

  function returnToLogin() {
    setView('form');
    setMode('sign-in');
    setOtp('');
    setError('');
    setMessage('');
  }

  const signedInEmail = view === 'verified' ? (state.user?.email ?? pendingEmail) : state.user?.email;
  const hasVerifiedAccount = view === 'verified' || (view === 'verification' && state.status === 'authenticated');

  return (
    <div className="auth-backdrop" onMouseDown={(event) => { if (event.target === event.currentTarget) onClose(); }}>
      <section ref={panelRef} className="auth-panel" role="dialog" aria-modal="true" aria-labelledby="auth-title">
        <button ref={closeButtonRef} className="auth-close" type="button" onClick={onClose} aria-label="Close account panel">×</button>
        <p className="eyebrow">STREETMAP ACCOUNT</p>
        <h2 id="auth-title">
          {hasVerifiedAccount || state.status === 'authenticated' ? 'Your account' : view === 'verification' ? 'Check your email' : mode === 'sign-in' ? 'Welcome back' : 'Create your account'}
        </h2>
        {state.status === 'unconfigured' ? (
          <p className="auth-message">Sign-in is unavailable until this build has public Supabase project configuration.</p>
        ) : state.status === 'error' ? (
          <p className="auth-error" role="alert">The saved session could not be read. Close and reopen the popup to try again.</p>
        ) : state.status === 'loading' ? (
          <p className="auth-message" role="status">Checking your session…</p>
        ) : hasVerifiedAccount ? (
          <div className="auth-success" role="status">
            <p className="auth-success-title">Email verified</p>
            <p className="auth-message">You're signed in{signedInEmail ? ` as ${signedInEmail}` : ''}. Your account is ready.</p>
            <button className="auth-submit" type="button" onClick={() => void handleSignOut()} disabled={busy}>
              {busy ? 'Signing out…' : 'Sign out'}
            </button>
          </div>
        ) : state.status === 'authenticated' ? (
          <>
            <p className="auth-message">Signed in{state.user?.email ? ` as ${state.user.email}` : ''}.</p>
            <button className="auth-submit" type="button" onClick={() => void handleSignOut()} disabled={busy}>
              {busy ? 'Signing out…' : 'Sign out'}
            </button>
          </>
        ) : view === 'verification' ? (
          <>
            <p className="auth-message">Enter the six-digit verification code sent to <strong>{maskEmail(pendingEmail)}</strong>. Check your inbox and spam folder.</p>
            {message && <p className="auth-message" role="status">{message}</p>}
            <form onSubmit={(event) => void handleVerify(event)}>
              <label className="auth-label" htmlFor="auth-verification-code">Verification code</label>
              <input
                id="auth-verification-code"
                className="auth-input auth-otp-input"
                type="text"
                inputMode="numeric"
                autoComplete="one-time-code"
                pattern="[0-9]{6}"
                maxLength={6}
                required
                aria-describedby="auth-code-help"
                value={otp}
                onChange={(event) => setOtp(event.currentTarget.value.replace(/\D/g, '').slice(0, 6))}
              />
              <p id="auth-code-help" className="auth-field-help">Enter all six digits from the confirmation email.</p>
              <button className="auth-submit" type="submit" disabled={busy || otp.length !== 6}>
                {busy ? 'Verifying code…' : 'Verify email'}
              </button>
            </form>
            <button className="auth-secondary-button" type="button" onClick={() => void handleResend()} disabled={busy || resendCooldown > 0}>
              {busy ? 'Sending…' : resendCooldown > 0 ? `Resend code in ${resendCooldown}s` : 'Resend verification code'}
            </button>
            <button className="auth-mode-toggle" type="button" onClick={returnToLogin} disabled={busy}>Back to login</button>
          </>
        ) : (
          <>
            <button className="auth-google" type="button" onClick={() => void handleGoogle()} disabled={busy}>
              {busy ? 'Please wait…' : 'Continue with Google'}
            </button>
            <div className="auth-divider"><span>or use email</span></div>
            <form onSubmit={(event) => void handleSubmit(event)}>
              <label className="auth-label" htmlFor="auth-email">Email</label>
              <input id="auth-email" className="auth-input" type="email" autoComplete="email" required value={email} onChange={(event) => setEmail(event.currentTarget.value)} />
              <label className="auth-label" htmlFor="auth-password">Password</label>
              <div className="auth-password-wrap">
                <input id="auth-password" className="auth-input" type={showPassword ? 'text' : 'password'} autoComplete={mode === 'sign-in' ? 'current-password' : 'new-password'} minLength={6} required value={password} onChange={(event) => setPassword(event.currentTarget.value)} />
                <button className="auth-password-toggle" type="button" onClick={() => setShowPassword((visible) => !visible)} aria-label={showPassword ? 'Hide password' : 'Show password'} aria-pressed={showPassword}>
                  {showPassword ? 'Hide' : 'Show'}
                </button>
              </div>
              <button className="auth-submit" type="submit" disabled={busy}>
                {busy ? mode === 'sign-up' ? 'Creating account…' : 'Signing in…' : mode === 'sign-in' ? 'Sign in' : 'Create account'}
              </button>
            </form>
            {mode === 'sign-in' && pendingEmail && (
              <button className="auth-secondary-button" type="button" onClick={() => void handleResend()} disabled={busy || resendCooldown > 0}>
                {busy ? 'Sending…' : resendCooldown > 0 ? `Resend code in ${resendCooldown}s` : 'Resend verification code'}
              </button>
            )}
            <button className="auth-mode-toggle" type="button" onClick={() => { setMode(mode === 'sign-in' ? 'sign-up' : 'sign-in'); setError(''); setMessage(''); }} disabled={busy}>
              {mode === 'sign-in' ? 'New to StreetMap? Create an account' : 'Already have an account? Sign in'}
            </button>
            {message && <p className="auth-message" role="status">{message}</p>}
          </>
        )}
        {error && <p className="auth-error" role="alert">{error}</p>}
        <p className="auth-footnote">Your account lets StreetMap prepare future community features. Public notes remain available without signing in.</p>
      </section>
    </div>
  );
}
