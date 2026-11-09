import assert from 'node:assert/strict';
import test from 'node:test';
import { setImmediate as immediate } from 'node:timers/promises';
import { Window } from 'happy-dom';
import React from 'react';

const window = new Window();
globalThis.window = window;
globalThis.document = window.document;
globalThis.HTMLElement = window.HTMLElement;
globalThis.HTMLInputElement = window.HTMLInputElement;
globalThis.Event = window.Event;
globalThis.InputEvent = window.InputEvent;
globalThis.MouseEvent = window.MouseEvent;
Object.defineProperty(globalThis, 'navigator', { value: window.navigator, configurable: true });
globalThis.IS_REACT_ACT_ENVIRONMENT = true;
const domDocument = window.document;
const { act } = await import('react');
const { createRoot } = await import('react-dom/client');
const { default: AuthPanel, maskEmail, RESEND_COOLDOWN_SECONDS } = await import('../src/popup/components/AuthPanel.tsx');
const { EmailAuthFailure } = await import('../src/popup/auth/emailPassword.ts');
const { GoogleOAuthFailure } = await import('../src/popup/auth/googleOAuthFlow.ts');

function actions(overrides = {}) {
  return {
    signInWithEmail: async () => 'authenticated',
    signUpWithEmail: async () => 'verification-pending',
    resendSignupConfirmation: async () => undefined,
    signInWithGoogle: async () => undefined,
    signOut: async () => undefined,
    ...overrides,
  };
}

function mount(panelActions, state = { status: 'anonymous', user: null }) {
  const container = domDocument.createElement('div');
  domDocument.body.append(container);
  const root = createRoot(container);
  act(() => root.render(React.createElement(AuthPanel, { state, onClose: () => undefined, actions: panelActions })));
  return {
    container,
    unmount() {
      act(() => root.unmount());
      container.remove();
    },
  };
}

function setInput(container, selector, value) {
  const input = container.querySelector(selector);
  assert.ok(input, `expected input ${selector}`);
  const setter = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value').set;
  act(() => {
    input._valueTracker?.setValue(input.value);
    setter.call(input, value);
    input.dispatchEvent(new window.Event('input', { bubbles: true }));
    input.dispatchEvent(new window.Event('change', { bubbles: true }));
  });
  return input;
}

async function click(element) {
  assert.ok(element);
  await act(async () => {
    element.click();
    await immediate();
  });
}

async function submit(container, selector = 'form') {
  const form = container.querySelector(selector);
  assert.ok(form, `expected form ${selector}`);
  await act(async () => {
    form.dispatchEvent(new window.Event('submit', { bubbles: true, cancelable: true }));
    await immediate();
  });
}

async function openSignup(mounted, address = 'reader@example.test') {
  setInput(mounted.container, '#auth-email', address);
  setInput(mounted.container, '#auth-password', 'synthetic-password');
  await click(mounted.container.querySelector('.auth-mode-toggle'));
  await submit(mounted.container);
}

test('signup requiring confirmation shows link instructions without claiming an authenticated session', async () => {
  let closed = false;
  const panelActions = actions({ async signUpWithEmail(email) { assert.equal(email, 'reader@example.test'); return 'verification-pending'; } });
  const container = domDocument.createElement('div');
  domDocument.body.append(container);
  const root = createRoot(container);
  act(() => root.render(React.createElement(AuthPanel, { state: { status: 'anonymous', user: null }, onClose: () => { closed = true; }, actions: panelActions })));
  const mounted = { container, unmount() { act(() => root.unmount()); container.remove(); } };
  try {
    await openSignup(mounted);
    assert.match(container.textContent, /Check your email/);
    assert.match(container.textContent, /confirmation email to r\*\*\*\*r@example\.test/i);
    assert.match(container.textContent, /click its link.*return to StreetMap and sign in/i);
    assert.doesNotMatch(container.textContent, /Signed in as/);
    assert.equal(container.querySelector('#auth-verification-code'), null);
    assert.equal(container.querySelector('input[autocomplete="one-time-code"]'), null);
    assert.match(container.querySelector('.auth-secondary-button').textContent, /Resend email/);
    assert.match(container.querySelector('.auth-secondary-button').textContent, new RegExp(`${RESEND_COOLDOWN_SECONDS}s`));
    assert.equal(closed, false);
    assert.equal(maskEmail('reader@example.test'), 'r****r@example.test');
  } finally {
    mounted.unmount();
  }
});

test('signup failure explains the built-in email recipient restriction and shows safe reference metadata', async () => {
  const mounted = mount(actions({
    async signUpWithEmail() {
      throw new EmailAuthFailure(
        'signup',
        'Supabase could not send a confirmation email to this address. With the built-in email service, only addresses belonging to the Supabase project organization are allowed. No account sign-in was completed.',
        'email_address_not_authorized',
        403,
      );
    },
  }));
  try {
    await openSignup(mounted);
    assert.match(mounted.container.querySelector('[role="alert"]').textContent, /built-in email service.*project organization/i);
    assert.match(mounted.container.querySelector('[role="alert"]').textContent, /email_address_not_authorized \(HTTP 403\)/);
    assert.equal(mounted.container.querySelector('#auth-verification-code'), null);
    assert.doesNotMatch(mounted.container.textContent, /confirmation email sent/i);
  } finally {
    mounted.unmount();
  }
});

test('confirmation resend succeeds, masks account enumeration, and starts cooldown', async () => {
  let resendCalls = 0;
  const mounted = mount(actions({
    async signInWithEmail() { return 'email-unverified'; },
    async resendSignupConfirmation(email) { resendCalls += 1; assert.equal(email, 'reader@example.test'); },
  }));
  try {
    setInput(mounted.container, '#auth-email', 'reader@example.test');
    setInput(mounted.container, '#auth-password', 'synthetic-password');
    await submit(mounted.container);
    const verificationResend = mounted.container.querySelector('.auth-secondary-button');
    await click(verificationResend);
    assert.equal(resendCalls, 1);
    assert.match(mounted.container.textContent, /If a signup is pending.*confirmation email/i);
    assert.match(mounted.container.querySelector('.auth-secondary-button').textContent, /Resend email in 30s/);
  } finally {
    mounted.unmount();
  }
});

test('unverified login offers link resend and back-to-login retains email', async () => {
  const mounted = mount(actions({ async signInWithEmail() { return 'email-unverified'; } }));
  try {
    setInput(mounted.container, '#auth-email', 'reader@example.test');
    setInput(mounted.container, '#auth-password', 'synthetic-password');
    await submit(mounted.container);
    assert.match(mounted.container.textContent, /Email confirmation is still required/);
    assert.match(mounted.container.textContent, /confirmation link/);
    assert.match(mounted.container.textContent, /return to StreetMap and sign in/i);
    assert.equal(mounted.container.querySelector('#auth-verification-code'), null);
    await click(mounted.container.querySelector('.auth-mode-toggle'));
    assert.equal(mounted.container.querySelector('#auth-email').value, 'reader@example.test');
  } finally {
    mounted.unmount();
  }
});

test('resend failures never claim delivery and display rate-limit cooldown', async () => {
  const mounted = mount(actions({
    async signInWithEmail() { return 'email-unverified'; },
    async resendSignupConfirmation() { throw new EmailAuthFailure('rate-limited', 'Too many confirmation email requests. Please wait before requesting another email.'); },
  }));
  try {
    setInput(mounted.container, '#auth-email', 'reader@example.test');
    setInput(mounted.container, '#auth-password', 'synthetic-password');
    await submit(mounted.container);
    await click(mounted.container.querySelector('.auth-secondary-button'));
    assert.match(mounted.container.querySelector('[role="alert"]').textContent, /Too many confirmation email requests/);
    assert.doesNotMatch(mounted.container.textContent, /confirmation email has been sent/i);
    assert.match(mounted.container.querySelector('.auth-secondary-button').textContent, /Resend email in 60s/);
  } finally {
    mounted.unmount();
  }
});

test('duplicate resend requests are prevented while one is in flight', async () => {
  let resolveResend;
  let resendCalls = 0;
  const mounted = mount(actions({
    async signInWithEmail() { return 'email-unverified'; },
    resendSignupConfirmation() {
      resendCalls += 1;
      return new Promise((resolve) => { resolveResend = resolve; });
    },
  }));
  try {
    setInput(mounted.container, '#auth-email', 'reader@example.test');
    setInput(mounted.container, '#auth-password', 'synthetic-password');
    await submit(mounted.container);
    const button = mounted.container.querySelector('.auth-secondary-button');
    await act(async () => {
      button.click();
      button.click();
      await immediate();
    });
    assert.equal(resendCalls, 1);
    assert.equal(button.disabled, true);
    assert.match(button.textContent, /Sending/);
    await act(async () => {
      resolveResend();
      await immediate();
    });
    assert.match(mounted.container.textContent, /confirmation email/i);
  } finally {
    mounted.unmount();
  }
});

test('password visibility is accessible and Google sign-in remains a separate action', async () => {
  let googleCalls = 0;
  let closeCalls = 0;
  const panelActions = actions({ async signInWithGoogle() { googleCalls += 1; } });
  const container = domDocument.createElement('div');
  domDocument.body.append(container);
  const root = createRoot(container);
  act(() => root.render(React.createElement(AuthPanel, { state: { status: 'anonymous', user: null }, onClose: () => { closeCalls += 1; }, actions: panelActions })));
  try {
    assert.equal(container.querySelector('#auth-password').type, 'password');
    await click(container.querySelector('.auth-password-toggle'));
    assert.equal(container.querySelector('#auth-password').type, 'text');
    await click(container.querySelector('.auth-password-toggle'));
    await click(container.querySelector('.auth-google'));
    assert.equal(googleCalls, 1);
    assert.equal(closeCalls, 1);
  } finally {
    act(() => root.unmount());
    container.remove();
  }
});

test('Google OAuth error shows safe phase and code diagnostics without secrets', async () => {
  const mounted = mount(actions({
    async signInWithGoogle() {
      throw new GoogleOAuthFailure(
        'exchange',
        'bad_code_verifier',
        'StreetMap could not verify this sign-in attempt. Start Google sign-in again in the same Chrome profile.',
        400,
      );
    },
  }));
  try {
    await click(mounted.container.querySelector('.auth-google'));
    const message = mounted.container.querySelector('[role="alert"]').textContent;
    assert.match(message, /same Chrome profile/);
    assert.match(message, /Diagnostic: exchange\/bad_code_verifier\/http_400/);
    assert.doesNotMatch(message, /access_token|refresh_token|code=|verifier value/);
  } finally {
    mounted.unmount();
  }
});

test('Supabase unexpected callback failure identifies only the callback component and safe error code', async () => {
  const mounted = mount(actions({
    async signInWithGoogle() {
      throw new GoogleOAuthFailure(
        'callback',
        'unexpected_failure',
        'Supabase Auth reported an unexpected failure while processing Google sign-in. Check the Supabase Auth logs around this time for the underlying cause, then try again.',
        undefined,
        'query',
      );
    },
  }));
  try {
    await click(mounted.container.querySelector('.auth-google'));
    const message = mounted.container.querySelector('[role="alert"]').textContent;
    assert.match(message, /Supabase Auth logs/);
    assert.match(message, /Diagnostic: callback\/query\/unexpected_failure/);
    assert.doesNotMatch(message, /callback URL|error_description|private|token|code=/i);
  } finally {
    mounted.unmount();
  }
});
