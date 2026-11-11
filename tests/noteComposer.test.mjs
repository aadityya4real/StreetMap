import assert from 'node:assert/strict';
import test from 'node:test';
import { Window } from 'happy-dom';
import React, { act } from 'react';
import { createRoot } from 'react-dom/client';

import NoteComposer, {
  countCharacters,
  getComposerLocation,
  validateComposerFields,
} from '../src/popup/components/NoteComposer.tsx';
import { CreateNoteApiError, createNoteAttempt } from '../src/popup/services/createNote.ts';

const window = new Window();
globalThis.window = window;
globalThis.document = window.document;
globalThis.HTMLElement = window.HTMLElement;
globalThis.MouseEvent = window.MouseEvent;
Object.defineProperty(globalThis, 'navigator', { value: window.navigator, configurable: true });
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

const validNote = {
  id: '8bc2b4ce-4277-4139-825d-c559694c8524',
  title: 'A useful local note',
  body: 'A detailed local note description.',
  category: 'local_tip',
  author: { display_name: 'StreetMap member' },
  created_at: '2026-11-12T08:30:00Z',
  helpful_vote_count: 0,
  match: { type: 'unverified_reference_label', location_label: 'Riverside Garden' },
};

const placeContext = {
  status: 'place-page-unverified',
  sourceHostname: 'www.google.com',
  label: 'Riverside Garden',
  mapCenter: { latitude: 28.5, longitude: 77.2 },
};

function mount(element) {
  const container = globalThis.document.createElement('div');
  globalThis.document.body.append(container);
  const root = createRoot(container);
  act(() => root.render(element));
  return {
    container,
    rerender(nextElement) {
      act(() => root.render(nextElement));
    },
    unmount() {
      act(() => root.unmount());
      container.remove();
    },
  };
}

function setInput(container, selector, value) {
  const input = container.querySelector(selector);
  assert.ok(input, `expected input ${selector}`);
  const prototype = input instanceof window.HTMLTextAreaElement
    ? window.HTMLTextAreaElement.prototype
    : window.HTMLInputElement.prototype;
  const setter = Object.getOwnPropertyDescriptor(prototype, 'value').set;
  act(() => {
    input._valueTracker?.setValue(input.value);
    setter.call(input, value);
    input.dispatchEvent(new window.Event('input', { bubbles: true }));
    input.dispatchEvent(new window.Event('change', { bubbles: true }));
  });
  return input;
}

async function submit(container) {
  const form = container.querySelector('form');
  assert.ok(form);
  await act(async () => {
    form.dispatchEvent(new window.Event('submit', { bubbles: true, cancelable: true }));
    await Promise.resolve();
  });
}

test('uses only broad labels and unverified place labels as creation contexts', () => {
  const broad = getComposerLocation({ status: 'broad-area', sourceHostname: 'www.google.com', source: 'search-query', label: 'Greater Noida', mapCenter: { latitude: 28.4, longitude: 77.5 } });
  assert.deepEqual(broad.choices.map((choice) => choice.type), ['broad', 'coordinates']);
  assert.deepEqual(broad.choices[0].requestLocation, { type: 'broad', label: 'Greater Noida' });
  assert.match(broad.details[1], /Map center: 28.4, 77.5/);

  const place = getComposerLocation(placeContext);
  assert.deepEqual(place.choices.map((choice) => choice.type), ['unverified', 'coordinates']);
  assert.deepEqual(place.choices[0].requestLocation, { type: 'unverified', label: 'Riverside Garden' });
  assert.match(place.details[0], /not been independently verified/);
  assert.match(place.details[1], /not the venue coordinates/);
  assert.match(place.details.join(' '), /not.*verified/i);
});

test('does not treat map center or an unresolved place ID as a confirmed creation location', () => {
  const mapCenter = getComposerLocation({ status: 'map-center', sourceHostname: 'www.google.com', mapCenter: { latitude: 28.5, longitude: 77.2 } });
  assert.deepEqual(mapCenter.choices.map((choice) => choice.type), ['coordinates']);
  assert.equal(mapCenter.choices[0].requestLocation, null);
  assert.deepEqual(mapCenter.choices[0].coordinates, { latitude: 28.5, longitude: 77.2 });
  assert.match(mapCenter.details[0], /not a selected place/);

  const reference = getComposerLocation({ status: 'unverified-place-reference', sourceHostname: 'www.google.com', placeId: 'ChIJsynthetic' });
  assert.deepEqual(reference.choices, []);
  assert.match(reference.details[0], /cannot safely attach/);

  const invalidCenter = getComposerLocation({ status: 'map-center', sourceHostname: 'www.google.com', mapCenter: { latitude: 91, longitude: 77.2 } });
  assert.deepEqual(invalidCenter.choices, []);
  const noContext = getComposerLocation({ status: 'maps-no-location', sourceHostname: 'www.google.com' });
  assert.deepEqual(noContext.choices, []);
});

test('does not invent a verified StreetMap ID for Google Maps context', () => {
  const location = getComposerLocation(placeContext);
  assert.equal(location.choices.some((choice) => choice.type === 'verified'), false);
  assert.match(location.details.join(' '), /not been independently verified/);
});

test('validates required fields and backend rune-count limits', () => {
  assert.deepEqual(validateComposerFields('', '', 'local_tip'), {
    title: 'Enter a title with at least 3 characters.',
    body: 'Add a description of the local knowledge.',
  });
  assert.equal(validateComposerFields('A good title', 'Description', 'other').title, undefined);
  assert.match(validateComposerFields('a'.repeat(121), 'Description', 'other').title, /120 characters/);
  assert.match(validateComposerFields('A good title', 'x'.repeat(5001), 'other').body, /5,000 characters/);
  assert.equal(countCharacters('😀'.repeat(120)), 120);
  assert.equal(validateComposerFields('😀'.repeat(120), '😀', 'safety').title, undefined);
});

test('renders an accessible place-aware form with all backend categories', () => {
  const mounted = mount(React.createElement(NoteComposer, { context: placeContext, onClose: () => undefined }));
  try {
    assert.equal(mounted.container.querySelector('[role="dialog"]').getAttribute('aria-modal'), 'true');
    assert.equal(mounted.container.querySelector('label[for="composer-title-input"]').textContent, 'Title');
    assert.equal(mounted.container.querySelector('label[for="composer-body-input"]').textContent, 'Description');
    assert.equal(mounted.container.querySelector('label[for="composer-category"]').textContent, 'Category');
    assert.match(mounted.container.textContent, /Riverside Garden/);
    assert.match(mounted.container.textContent, /Map center: 28.5, 77.2 \(not the venue coordinates\)/);
    assert.equal(mounted.container.querySelectorAll('#composer-category option').length, 8);
    assert.equal(mounted.container.querySelectorAll('#composer-location-type option').length, 2);
    assert.equal(mounted.container.querySelector('#composer-location-type').value, 'unverified');
    assert.equal(mounted.container.querySelector('#composer-title-count').textContent, '0 / 120');
    assert.equal(mounted.container.querySelector('#composer-body-count').textContent, '0 / 5000');
  } finally {
    mounted.unmount();
  }
});

test('requires explicit confirmation before sending map-center coordinates', async () => {
  let attempts = 0;
  let submissions = 0;
  let authRequests = 0;
  let isAuthenticated = false;
  let request;
  const renderComposer = () => React.createElement(NoteComposer, {
    context: { status: 'map-center', sourceHostname: 'www.google.com', mapCenter: { latitude: 28.5, longitude: 77.2 } },
    isAuthenticated,
    onAuthenticationRequired: () => { authRequests++; },
    onClose: () => undefined,
    actions: {
      createNoteAttempt(value) {
        attempts++;
        request = value;
        return {
          submit: async () => {
            submissions++;
            if (submissions === 1) throw new CreateNoteApiError('authentication', 'Sign in is required.');
            return { note: validNote, replayed: false };
          },
        };
      },
    },
  });
  const mounted = mount(renderComposer());
  try {
    setInput(mounted.container, '#composer-title-input', 'Useful map-center note');
    setInput(mounted.container, '#composer-body-input', 'The marked point is the right meeting location.');
    await submit(mounted.container);
    assert.equal(attempts, 0);
    assert.match(mounted.container.textContent, /Confirm that you want to use these map-center coordinates/);

    const checkbox = mounted.container.querySelector('.composer-coordinate-confirmation input');
    assert.ok(checkbox);
    act(() => {
      checkbox.click();
    });
    await submit(mounted.container);
    assert.equal(attempts, 1);
    assert.equal(submissions, 1);
    assert.equal(authRequests, 1);
    assert.equal(checkbox.checked, true);

    isAuthenticated = true;
    mounted.rerender(renderComposer());
    await act(async () => Promise.resolve());
    assert.equal(submissions, 1, 'login must not submit the coordinate draft automatically');
    assert.equal(checkbox.checked, true, 'the explicit coordinate confirmation is part of the in-memory draft');
    await submit(mounted.container);
    assert.equal(submissions, 2);
    assert.deepEqual(request.location, {
      type: 'coordinates',
      latitude: 28.5,
      longitude: 77.2,
      user_confirmed: true,
    });
    assert.match(mounted.container.textContent, /Note shared/);
  } finally {
    mounted.unmount();
  }
});

test('shows inline validation and does not create an attempt for empty fields', async () => {
  let attempts = 0;
  const mounted = mount(React.createElement(NoteComposer, {
    context: placeContext,
    onClose: () => undefined,
    actions: { createNoteAttempt: () => { attempts++; throw new Error('must not submit'); } },
  }));
  try {
    await submit(mounted.container);
    assert.match(mounted.container.textContent, /Enter a title with at least 3 characters/);
    assert.match(mounted.container.textContent, /Add a description of the local knowledge/);
    assert.equal(attempts, 0);
    assert.equal(mounted.container.querySelector('#composer-title-input').getAttribute('aria-invalid'), 'true');
  } finally {
    mounted.unmount();
  }
});

test('submits only user content and the unverified context, then confirms publication', async () => {
  let submittedRequest;
  let published;
  const mounted = mount(React.createElement(NoteComposer, {
    context: placeContext,
    onClose: () => undefined,
    onPublished: (note) => { published = note; },
    actions: {
      createNoteAttempt(request) {
        submittedRequest = request;
        return { submit: async () => ({ note: validNote, replayed: false }) };
      },
    },
  }));
  try {
    setInput(mounted.container, '#composer-title-input', 'Helpful garden entrance');
    setInput(mounted.container, '#composer-body-input', 'Use the public path along the east wall.');
    assert.equal(mounted.container.querySelector('#composer-title-count').textContent, '23 / 120');
    assert.equal(mounted.container.querySelector('#composer-body-count').textContent, '40 / 5000');
    await submit(mounted.container);

    assert.deepEqual(submittedRequest, {
      location: { type: 'unverified', label: 'Riverside Garden' },
      title: 'Helpful garden entrance',
      body: 'Use the public path along the east wall.',
      category: 'local_tip',
    });
    assert.equal('author_user_id' in submittedRequest, false);
    assert.equal('status' in submittedRequest, false);
    assert.equal('latitude' in submittedRequest.location, false);
    assert.equal(published, validNote);
    assert.match(mounted.container.textContent, /Note shared/);
    assert.match(mounted.container.textContent, /published and public/);
  } finally {
    mounted.unmount();
  }
});

test('composer, authenticated API client, publication response, and feed-refresh callback work together for supported contexts', async (t) => {
  const cases = [
    {
      name: 'unverified place',
      context: placeContext,
      expectedLocation: { type: 'unverified', label: 'Riverside Garden' },
    },
    {
      name: 'broad area',
      context: { status: 'broad-area', sourceHostname: 'www.google.com', source: 'search-query', label: 'Garden District' },
      expectedLocation: { type: 'broad', label: 'Garden District' },
    },
    {
      name: 'user-confirmed map-center coordinates',
      context: { status: 'map-center', sourceHostname: 'www.google.com', mapCenter: { latitude: 28.5, longitude: 77.2 } },
      expectedLocation: { type: 'coordinates', latitude: 28.5, longitude: 77.2, user_confirmed: true },
      confirmCoordinates: true,
    },
  ];

  for (const item of cases) {
    await t.test(item.name, async () => {
      let requestURL;
      let requestOptions;
      let feedRefreshes = 0;
      let published;
      const mounted = mount(React.createElement(NoteComposer, {
        context: item.context,
        onClose: () => undefined,
        onPublished: (note) => { published = note; feedRefreshes++; },
        actions: {
          createNoteAttempt: (request) => createNoteAttempt(request, {
            getApiBaseUrl: () => 'http://127.0.0.1:8080/api/v1',
            getAccessToken: async () => 'synthetic-test-access-token',
            createIdempotencyKey: () => '8bc2b4ce-4277-4139-825d-c559694c8524',
            fetcher: async (url, options) => {
              requestURL = url;
              requestOptions = options;
              return new globalThis.Response(JSON.stringify({ data: { note: validNote } }), {
                status: 201,
                headers: { 'content-type': 'application/json' },
              });
            },
          }),
        },
      }));

      try {
        setInput(mounted.container, '#composer-title-input', 'Helpful local entrance');
        setInput(mounted.container, '#composer-body-input', 'Follow the public path beside the garden.');
        if (item.confirmCoordinates) {
          act(() => mounted.container.querySelector('.composer-coordinate-confirmation input').click());
        }
        await submit(mounted.container);

        assert.equal(requestURL.href, 'http://127.0.0.1:8080/api/v1/notes');
        assert.equal(requestOptions.method, 'POST');
        assert.equal(requestOptions.headers.Authorization, 'Bearer synthetic-test-access-token');
        assert.equal(requestOptions.headers['Content-Type'], 'application/json');
        assert.equal(requestOptions.headers['Idempotency-Key'], '8bc2b4ce-4277-4139-825d-c559694c8524');
        assert.equal(requestOptions.credentials, 'omit');
        assert.deepEqual(JSON.parse(requestOptions.body).location, item.expectedLocation);
        assert.deepEqual(published, validNote);
        assert.equal(feedRefreshes, 1);
        assert.match(mounted.container.textContent, /Note shared/);
      } finally {
        mounted.unmount();
      }
    });
  }
});

test('disables duplicate submissions while pending and shows safe API errors', async () => {
  let releaseSubmit;
  let submitted = 0;
  const pending = new Promise((resolve) => { releaseSubmit = resolve; });
  const mounted = mount(React.createElement(NoteComposer, {
    context: placeContext,
    onClose: () => undefined,
    actions: {
      createNoteAttempt: () => ({
        submit: async () => {
          submitted++;
          await pending;
          throw new CreateNoteApiError('authentication', 'Your StreetMap session is missing or expired. Sign in and try again.');
        },
      }),
    },
  }));
  try {
    setInput(mounted.container, '#composer-title-input', 'Helpful garden entrance');
    setInput(mounted.container, '#composer-body-input', 'Use the public path along the east wall.');
    const form = mounted.container.querySelector('form');
    let submission;
    await act(async () => {
      form.dispatchEvent(new window.Event('submit', { bubbles: true, cancelable: true }));
      await Promise.resolve();
      submission = Promise.resolve();
    });
    assert.equal(mounted.container.querySelector('button[type="submit"]').disabled, true);
    assert.equal(mounted.container.querySelector('button[type="submit"]').textContent, 'Sharing note…');
    assert.equal(submitted, 1);
    await act(async () => {
      form.dispatchEvent(new window.Event('submit', { bubbles: true, cancelable: true }));
      await Promise.resolve();
    });
    assert.equal(submitted, 1, 'a second submit while the first is pending must be ignored');
    await act(async () => {
      releaseSubmit();
      await new Promise((resolve) => window.setTimeout(resolve, 0));
    });
    await submission;
    assert.match(mounted.container.textContent, /Sign in to publish this note/);
    assert.equal(mounted.container.querySelector('button[type="submit"]').disabled, false);
  } finally {
    mounted.unmount();
  }
});

test('preserves the complete draft through authentication and requires an explicit publish after login', async () => {
  let isAuthenticated = false;
  let authRequests = 0;
  let submitCalls = 0;
  let currentContext = placeContext;
  let createdRequest;
  let published;
  const attempt = {
    async submit() {
      submitCalls++;
      if (submitCalls === 1) throw new CreateNoteApiError('authentication', 'Your StreetMap session is missing or expired. Sign in and try again.');
      return { note: validNote, replayed: false };
    },
  };
  const renderComposer = () => React.createElement(NoteComposer, {
    context: currentContext,
    isAuthenticated,
    onAuthenticationRequired: () => { authRequests++; },
    onPublished: (note) => { published = note; },
    onClose: () => undefined,
    actions: { createNoteAttempt: (request) => { createdRequest = request; return attempt; } },
  });
  const mounted = mount(renderComposer());
  try {
    setInput(mounted.container, '#composer-title-input', 'Helpful garden entrance');
    setInput(mounted.container, '#composer-body-input', 'Use the public path along the east wall.');
    await submit(mounted.container);

    assert.equal(authRequests, 1);
    assert.equal(submitCalls, 1);
    assert.equal(published, undefined);
    assert.equal(mounted.container.querySelector('#composer-title-input').value, 'Helpful garden entrance');
    assert.equal(mounted.container.querySelector('#composer-body-input').value, 'Use the public path along the east wall.');
    assert.equal(mounted.container.querySelector('#composer-category').value, 'local_tip');
    assert.match(mounted.container.textContent, /Sign in to publish this note/);

    currentContext = { status: 'broad-area', sourceHostname: 'www.google.com', source: 'search-query', label: 'Different area' };
    isAuthenticated = true;
    mounted.rerender(renderComposer());
    await act(async () => Promise.resolve());
    assert.equal(submitCalls, 1, 'authentication must not publish the draft automatically');
    assert.match(mounted.container.textContent, /You’re signed in/);
    assert.equal(mounted.container.querySelector('#composer-title-input').value, 'Helpful garden entrance');
    assert.equal(mounted.container.querySelector('.composer-location-label').textContent, 'Riverside Garden');

    await submit(mounted.container);
    assert.equal(submitCalls, 2);
    assert.deepEqual(createdRequest.location, { type: 'unverified', label: 'Riverside Garden' });
    assert.equal(published, validNote);
    assert.match(mounted.container.textContent, /Note shared/);
  } finally {
    mounted.unmount();
  }
});

test('cancelled authentication leaves the draft in place and can be reopened', async () => {
  let authRequests = 0;
  let submitCalls = 0;
  const mounted = mount(React.createElement(NoteComposer, {
    context: placeContext,
    onAuthenticationRequired: () => { authRequests++; },
    onClose: () => undefined,
    actions: {
      createNoteAttempt: () => ({
        submit: async () => {
          submitCalls++;
          throw new CreateNoteApiError('authentication', 'Your StreetMap session is missing or expired. Sign in and try again.');
        },
      }),
    },
  }));
  try {
    setInput(mounted.container, '#composer-title-input', 'Quiet courtyard entrance');
    setInput(mounted.container, '#composer-body-input', 'Walk around the north side.');
    await submit(mounted.container);
    assert.equal(authRequests, 1);

    mounted.rerender(React.createElement(NoteComposer, {
      context: placeContext,
      authPanelOpen: true,
      onAuthenticationRequired: () => { authRequests++; },
      onClose: () => undefined,
      actions: { createNoteAttempt: () => ({ submit: async () => { submitCalls++; throw new Error('unused'); } }) },
    }));
    mounted.rerender(React.createElement(NoteComposer, {
      context: placeContext,
      authPanelOpen: false,
      onAuthenticationRequired: () => { authRequests++; },
      onClose: () => undefined,
      actions: { createNoteAttempt: () => ({ submit: async () => { submitCalls++; throw new Error('unused'); } }) },
    }));

    assert.equal(mounted.container.querySelector('#composer-title-input').value, 'Quiet courtyard entrance');
    assert.equal(mounted.container.querySelector('#composer-body-input').value, 'Walk around the north side.');
    assert.equal(submitCalls, 1, 'closing the auth panel must not retry the submission');
    assert.equal(mounted.container.querySelector('button').textContent, '×');
    const signIn = [...mounted.container.querySelectorAll('button')].find((button) => button.textContent === 'Sign in to publish');
    assert.ok(signIn);
    act(() => signIn.click());
    assert.equal(authRequests, 2);
    assert.equal(submitCalls, 1);
  } finally {
    mounted.unmount();
  }
});

test('an expired authenticated session opens account recovery without losing the draft', async () => {
  let authRequests = 0;
  const mounted = mount(React.createElement(NoteComposer, {
    context: placeContext,
    isAuthenticated: true,
    onAuthenticationRequired: () => { authRequests++; },
    onClose: () => undefined,
    actions: {
      createNoteAttempt: () => ({
        submit: async () => { throw new CreateNoteApiError('authentication', 'Your StreetMap session is missing or expired. Sign in and try again.'); },
      }),
    },
  }));
  try {
    setInput(mounted.container, '#composer-title-input', 'Safer evening entrance');
    setInput(mounted.container, '#composer-body-input', 'Use the lit entrance after dusk.');
    await submit(mounted.container);
    assert.equal(authRequests, 1);
    assert.match(mounted.container.textContent, /could not verify the current session/);
    assert.match(mounted.container.textContent, /Sign out in the account panel, then sign in again/);
    assert.equal(mounted.container.querySelector('#composer-title-input').value, 'Safer evening entrance');
    assert.equal(mounted.container.querySelector('#composer-body-input').value, 'Use the lit entrance after dusk.');
  } finally {
    mounted.unmount();
  }
});

test('shows map-center coordinates separately and leaves confirmation unchecked by default', () => {
  const mounted = mount(React.createElement(NoteComposer, {
    context: { status: 'map-center', sourceHostname: 'www.google.com', mapCenter: { latitude: 28.5, longitude: 77.2 } },
    onClose: () => undefined,
  }));
  try {
    assert.match(mounted.container.textContent, /Map center coordinates/);
    assert.match(mounted.container.textContent, /28.5, 77.2/);
    assert.match(mounted.container.textContent, /not a selected place/);
    assert.ok(mounted.container.querySelector('form'));
    assert.equal(mounted.container.querySelector('.composer-coordinate-confirmation input').checked, false);
    assert.equal(mounted.container.querySelector('button[type="submit"]').disabled, true);
  } finally {
    mounted.unmount();
  }
});
