import assert from 'node:assert/strict';
import test from 'node:test';
import { Window } from 'happy-dom';
import React, { act } from 'react';
import { createRoot } from 'react-dom/client';

import useNotesFeed, { mergeDisplayNotes } from '../src/popup/hooks/useNotesFeed.ts';

const window = new Window();
globalThis.window = window;
globalThis.document = window.document;
globalThis.HTMLElement = window.HTMLElement;
Object.defineProperty(globalThis, 'navigator', { value: window.navigator, configurable: true });
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

const locationA = { status: 'place-page-unverified', sourceHostname: 'www.google.com', label: 'Venue A' };
const locationB = { status: 'place-page-unverified', sourceHostname: 'www.google.com', label: 'Venue B' };

function note(id, title, type = 'unverified_reference_label', group = 'context_matches') {
  return {
    id,
    title,
    body: 'A fixture note for pagination lifecycle tests.',
    category: 'local_tip',
    author: { display_name: 'StreetMap Test' },
    created_at: '2026-10-20T08:30:00Z',
    helpful_vote_count: 0,
    match: { type, location_label: 'Test location', ...(group === 'nearby' ? { distance_m: 300 } : {}) },
  };
}

function page(contextMatches = [], nearby = [], nextCursor = null, hasMore = false) {
  return { exact: [], context_matches: contextMatches, nearby, page: { next_cursor: nextCursor, has_more: hasMore } };
}

function createServices({ readLocationContext, loadNotesPage }) {
  const listeners = new Set();
  return {
    services: {
      readLocationContext,
      loadNotesPage,
      subscribeToLocationChanges(listener) {
        listeners.add(listener);
        return () => listeners.delete(listener);
      },
    },
    notifyLocationChange() {
      for (const listener of listeners) listener();
    },
  };
}

function createHarness(services) {
  return function NotesFeedHarness() {
    const feed = useNotesFeed(services);
    const label = feed.locationContext.status === 'place-page-unverified'
      ? feed.locationContext.label
      : feed.locationContext.status;
    const notes = feed.notesState.status === 'ready' ? feed.notesState.notes : [];
    return React.createElement('section', {
      'data-status': feed.notesState.status,
      'data-context': label,
      'data-loading-more': String(feed.isLoadingMore),
    },
    React.createElement('ul', null, notes.map((entry) => React.createElement('li', {
      key: entry.id,
      'data-note-id': entry.id,
      'data-group': entry.group,
    }, entry.title))),
    feed.loadMoreError && React.createElement('p', { role: 'alert' }, feed.loadMoreError),
    feed.notesState.status === 'ready' && feed.notesState.hasMore && React.createElement('button', {
      type: 'button',
      'data-action': 'load-more',
      disabled: feed.isLoadingMore,
      onClick: () => void feed.loadMore(),
    }, feed.isLoadingMore ? 'Loading more…' : 'Load more notes'),
    React.createElement('button', { type: 'button', 'data-action': 'retry', onClick: feed.retryNotes }, 'Retry'));
  };
}

async function mount(services) {
  const container = globalThis.document.createElement('div');
  globalThis.document.body.append(container);
  const root = createRoot(container);
  const Harness = createHarness(services);
  await act(async () => {
    root.render(React.createElement(Harness));
    await Promise.resolve();
    await Promise.resolve();
  });
  return {
    container,
    get feed() { return container.querySelector('section'); },
    async flush() {
      await act(async () => {
        await Promise.resolve();
        await Promise.resolve();
      });
    },
    unmount() {
      act(() => root.unmount());
      container.remove();
    },
  };
}

function deferred() {
  let resolve;
  let reject;
  const promise = new Promise((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return { promise, resolve, reject };
}

async function resolveRequest(mounted, request, value) {
  await act(async () => {
    request.resolve(value);
    await request.promise;
    await Promise.resolve();
  });
  await mounted.flush();
}

async function rejectRequest(mounted, request, error) {
  await act(async () => {
    request.reject(error);
    await request.promise.catch(() => {});
    await Promise.resolve();
  });
  await mounted.flush();
}

test('an initial response from an old Maps context cannot replace the new context', async () => {
  const oldResponse = deferred();
  const newResponse = deferred();
  let contextRead = 0;
  const services = createServices({
    readLocationContext: () => Promise.resolve(contextRead++ === 0 ? locationA : locationB),
    loadNotesPage: (context) => context.label === 'Venue A' ? oldResponse.promise : newResponse.promise,
  });
  const mounted = await mount(services.services);
  try {
    await mounted.flush();
    act(() => services.notifyLocationChange());
    await mounted.flush();
    await resolveRequest(mounted, newResponse, page([note('bbbbbbbb-0000-4000-8000-000000000001', 'New venue note') ]));
    await resolveRequest(mounted, oldResponse, page([note('aaaaaaaa-0000-4000-8000-000000000001', 'Stale venue note')]));
    assert.equal(mounted.feed.getAttribute('data-context'), 'Venue B');
    assert.deepEqual([...mounted.container.querySelectorAll('li')].map((item) => item.textContent), ['New venue note']);
  } finally {
    mounted.unmount();
  }
});

test('an old load-more response cannot append after a Maps context change', async () => {
  const stalePage = deferred();
  let contextRead = 0;
  const requests = [];
  const services = createServices({
    readLocationContext: () => Promise.resolve(contextRead++ === 0 ? locationA : locationB),
    loadNotesPage: (context, signal, cursor) => {
      requests.push({ context, signal, cursor });
      if (cursor) return stalePage.promise;
      return Promise.resolve(context.label === 'Venue A'
        ? page([note('aaaaaaaa-0000-4000-8000-000000000001', 'Old page one')], [], 'old-cursor', true)
        : page([note('bbbbbbbb-0000-4000-8000-000000000001', 'New page one') ]));
    },
  });
  const mounted = await mount(services.services);
  try {
    await mounted.flush();
    act(() => mounted.container.querySelector('[data-action="load-more"]').click());
    assert.equal(requests.at(-1).cursor, 'old-cursor');
    act(() => services.notifyLocationChange());
    await mounted.flush();
    assert.equal(requests.at(-1).context.label, 'Venue B');
    assert.equal(requests[1].signal.aborted, true);
    await resolveRequest(mounted, stalePage, page([note('aaaaaaaa-0000-4000-8000-000000000002', 'Stale next page')]));
    assert.equal(mounted.feed.getAttribute('data-context'), 'Venue B');
    assert.deepEqual([...mounted.container.querySelectorAll('li')].map((item) => item.textContent), ['New page one']);
  } finally {
    mounted.unmount();
  }
});

test('synchronous repeated load-more clicks issue one request for the cursor', async () => {
  const nextPage = deferred();
  const cursors = [];
  const services = createServices({
    readLocationContext: () => Promise.resolve(locationA),
    loadNotesPage: (_context, _signal, cursor) => {
      cursors.push(cursor);
      return cursor ? nextPage.promise : Promise.resolve(page([note('aaaaaaaa-0000-4000-8000-000000000001', 'First page')], [], 'cursor-one', true));
    },
  });
  const mounted = await mount(services.services);
  try {
    await mounted.flush();
    const button = mounted.container.querySelector('[data-action="load-more"]');
    act(() => { button.click(); button.click(); });
    assert.deepEqual(cursors, [undefined, 'cursor-one']);
    assert.equal(mounted.feed.getAttribute('data-loading-more'), 'true');
    await resolveRequest(mounted, nextPage, page([note('aaaaaaaa-0000-4000-8000-000000000002', 'Second page') ]));
    assert.equal(mounted.container.querySelectorAll('li').length, 2);
  } finally {
    mounted.unmount();
  }
});

test('cross-page duplicates appear once and a context match replaces a nearby duplicate', async () => {
  const services = createServices({
    readLocationContext: () => Promise.resolve(locationA),
    loadNotesPage: (_context, _signal, cursor) => Promise.resolve(cursor
      ? page([note('aaaaaaaa-0000-4000-8000-000000000001', 'Promoted match', 'unverified_reference_label')])
      : page([], [note('aaaaaaaa-0000-4000-8000-000000000001', 'Earlier nearby', 'nearby', 'nearby')], 'cursor-one', true)),
  });
  const mounted = await mount(services.services);
  try {
    await mounted.flush();
    act(() => mounted.container.querySelector('[data-action="load-more"]').click());
    await mounted.flush();
    const entries = mounted.container.querySelectorAll('li');
    assert.equal(entries.length, 1);
    assert.equal(entries[0].getAttribute('data-group'), 'context_matches');
    assert.equal(entries[0].textContent, 'Promoted match');
  } finally {
    mounted.unmount();
  }
});

test('rapid consecutive location changes discard all earlier initial responses', async () => {
  const responses = [deferred(), deferred(), deferred()];
  let contextRead = 0;
  const contexts = [locationA, { ...locationA, label: 'Venue B' }, { ...locationA, label: 'Venue C' }];
  const services = createServices({
    readLocationContext: () => Promise.resolve(contexts[contextRead++]),
    loadNotesPage: (_context, _signal, cursor) => cursor ? Promise.resolve(page()) : responses[contextRead - 1].promise,
  });
  const mounted = await mount(services.services);
  try {
    await mounted.flush();
    act(() => services.notifyLocationChange());
    await mounted.flush();
    act(() => services.notifyLocationChange());
    await mounted.flush();
    await resolveRequest(mounted, responses[2], page([note('cccccccc-0000-4000-8000-000000000001', 'Latest location') ]));
    await resolveRequest(mounted, responses[1], page([note('bbbbbbbb-0000-4000-8000-000000000001', 'Intermediate location') ]));
    await resolveRequest(mounted, responses[0], page([note('aaaaaaaa-0000-4000-8000-000000000001', 'Oldest location') ]));
    assert.equal(mounted.feed.getAttribute('data-context'), 'Venue C');
    assert.deepEqual([...mounted.container.querySelectorAll('li')].map((item) => item.textContent), ['Latest location']);
  } finally {
    mounted.unmount();
  }
});

test('intentional cancellation does not display a load-more error', async () => {
  const abortedPage = deferred();
  const services = createServices({
    readLocationContext: () => Promise.resolve(locationA),
    loadNotesPage: (_context, _signal, cursor) => cursor
      ? abortedPage.promise
      : Promise.resolve(page([note('aaaaaaaa-0000-4000-8000-000000000001', 'Loaded note')], [], 'cursor-one', true)),
  });
  const mounted = await mount(services.services);
  try {
    await mounted.flush();
    act(() => mounted.container.querySelector('[data-action="load-more"]').click());
    act(() => services.notifyLocationChange());
    await mounted.flush();
    await rejectRequest(mounted, abortedPage, new TypeError('fetch aborted by browser'));
    assert.equal(mounted.container.querySelector('[role="alert"]'), null);
  } finally {
    mounted.unmount();
  }
});

test('a failed next page preserves notes and retries with the same cursor', async () => {
  const firstAttempt = deferred();
  let requestCount = 0;
  const cursors = [];
  const services = createServices({
    readLocationContext: () => Promise.resolve(locationA),
    loadNotesPage: (_context, _signal, cursor) => {
      cursors.push(cursor);
      if (!cursor) return Promise.resolve(page([note('aaaaaaaa-0000-4000-8000-000000000001', 'First page')], [], 'cursor-one', true));
      requestCount += 1;
      return requestCount === 1 ? firstAttempt.promise : Promise.resolve(page([note('aaaaaaaa-0000-4000-8000-000000000002', 'Recovered page') ]));
    },
  });
  const mounted = await mount(services.services);
  try {
    await mounted.flush();
    act(() => mounted.container.querySelector('[data-action="load-more"]').click());
    await rejectRequest(mounted, firstAttempt, new Error('temporary network failure'));
    assert.equal(mounted.container.querySelectorAll('li').length, 1);
    assert.ok(mounted.container.querySelector('[role="alert"]'));
    const retryButton = mounted.container.querySelector('[data-action="load-more"]');
    assert.ok(retryButton);
    assert.equal(retryButton.disabled, false);
    act(() => retryButton.click());
    await mounted.flush();
    assert.deepEqual(cursors, [undefined, 'cursor-one', 'cursor-one']);
    assert.deepEqual([...mounted.container.querySelectorAll('li')].map((item) => item.textContent), ['First page', 'Recovered page']);
    assert.equal(mounted.container.querySelector('[role="alert"]'), null);
  } finally {
    mounted.unmount();
  }
});

test('mergeDisplayNotes deduplicates each page while preserving exact and context priority', () => {
  const nearby = { ...note('aaaaaaaa-0000-4000-8000-000000000001', 'Nearby'), group: 'nearby' };
  const context = { ...note('aaaaaaaa-0000-4000-8000-000000000001', 'Context'), group: 'context_matches' };
  const exact = { ...note('bbbbbbbb-0000-4000-8000-000000000001', 'Exact', 'exact_location'), group: 'exact' };
  const merged = mergeDisplayNotes([nearby], [context, nearby, exact]);
  assert.deepEqual(merged.map(({ title, group }) => [title, group]), [
    ['Exact', 'exact'],
    ['Context', 'context_matches'],
  ]);
});
