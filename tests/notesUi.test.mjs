import assert from 'node:assert/strict';
import test from 'node:test';
import { Window } from 'happy-dom';
import React, { act } from 'react';
import { createRoot } from 'react-dom/client';

import NoteCard from '../src/popup/components/NoteCard.tsx';
import NotesList from '../src/popup/components/NotesList.tsx';
import SearchInput from '../src/popup/components/SearchInput.tsx';

const window = new Window();
globalThis.window = window;
globalThis.document = window.document;
globalThis.HTMLElement = window.HTMLElement;
globalThis.MouseEvent = window.MouseEvent;
Object.defineProperty(globalThis, 'navigator', { value: window.navigator, configurable: true });
globalThis.IS_REACT_ACT_ENVIRONMENT = true;
const domDocument = window.document;

const displayNote = {
  id: '8bc2b4ce-4277-4139-825d-c559694c8524',
  title: 'Tea stall behind the bus stop',
  body: 'The family-run stall is open early.',
  category: 'hidden_gem',
  author: { display_name: 'Mira' },
  created_at: '2026-10-20T08:30:00Z',
  helpful_vote_count: 12,
  match: { type: 'nearby', location_label: 'Bus stop near the market', distance_m: 640 },
  group: 'nearby',
};

function mount(element) {
  const container = domDocument.createElement('div');
  domDocument.body.append(container);
  const root = createRoot(container);
  act(() => root.render(element));
  return {
    container,
    unmount() {
      act(() => root.unmount());
      container.remove();
    },
  };
}

test('note card expands and collapses inline with location and nearby provenance', () => {
  const mounted = mount(React.createElement(NoteCard, { note: displayNote }));
  try {
    const toggle = mounted.container.querySelector('button[aria-expanded]');
    assert.ok(toggle);
    assert.equal(toggle.getAttribute('aria-expanded'), 'false');
    assert.equal(mounted.container.textContent.includes(displayNote.body), false);

    act(() => toggle.click());
    assert.equal(toggle.getAttribute('aria-expanded'), 'true');
    assert.match(mounted.container.textContent, /The family-run stall is open early/);
    assert.match(mounted.container.textContent, /Mira/);
    assert.match(mounted.container.textContent, /Bus stop near the market/);
    assert.match(mounted.container.textContent, /640 m from the map center/);

    act(() => toggle.click());
    assert.equal(toggle.getAttribute('aria-expanded'), 'false');
    assert.equal(mounted.container.textContent.includes(displayNote.body), false);
  } finally {
    mounted.unmount();
  }
});

test('exact match summaries are distinguished from nearby notes', () => {
  const exactNote = {
    ...displayNote,
    match: { type: 'exact_location', location_label: 'StreetMap verified venue' },
    group: 'exact',
  };
  const mounted = mount(React.createElement(NoteCard, { note: exactNote }));
  try {
    assert.match(mounted.container.textContent, /Exact match/);
    assert.equal(mounted.container.textContent.includes('from the map center'), false);
  } finally {
    mounted.unmount();
  }
});

test('loading, error and retry states are accessible and retry is actionable', () => {
  let retries = 0;
  const loading = mount(React.createElement(NotesList, { notes: [], query: '', status: 'loading' }));
  try {
    assert.ok(loading.container.querySelector('[role="status"]'));
    assert.match(loading.container.textContent, /Finding local notes/);
  } finally {
    loading.unmount();
  }

  const error = mount(React.createElement(NotesList, {
    notes: [], query: '', status: 'error', message: 'The notes service returned invalid data.', onRetry: () => { retries += 1; },
  }));
  try {
    assert.ok(error.container.querySelector('[role="alert"]'));
    act(() => error.container.querySelector('button').click());
    assert.equal(retries, 1);
  } finally {
    error.unmount();
  }
});

test('empty state can explain when no supported context is available', () => {
  const mounted = mount(React.createElement(NotesList, {
    notes: [], query: '', status: 'ready', message: 'Open a supported Google Maps page to find notes.',
  }));
  try {
    assert.match(mounted.container.textContent, /No local notes yet/);
    assert.match(mounted.container.textContent, /Open a supported Google Maps page/);
  } finally {
    mounted.unmount();
  }
});

test('expanded search input is labelled, controlled, and uses the native clearable search type', () => {
  const mounted = mount(React.createElement(SearchInput, {
    id: 'note-search',
    value: 'tea',
    onChange: () => {},
  }));
  try {
    const input = mounted.container.querySelector('#note-search');
    assert.ok(input);
    assert.equal(input.getAttribute('type'), 'search');
    assert.equal(input.value, 'tea');
    assert.equal(mounted.container.querySelector('label').htmlFor, 'note-search');
    assert.equal(mounted.container.querySelectorAll('.search-field svg').length, 1);
    assert.equal(mounted.container.querySelector('kbd'), null);
  } finally {
    mounted.unmount();
  }
});

test('only the notes list reports its own scroll position', () => {
  const scrollPositions = [];
  const mounted = mount(React.createElement(NotesList, {
    notes: [displayNote],
    query: '',
    onListScroll: (scrollTop) => scrollPositions.push(scrollTop),
  }));
  try {
    const list = mounted.container.querySelector('.notes-list');
    assert.ok(list);
    list.scrollTop = 28;
    act(() => list.dispatchEvent(new window.Event('scroll', { bubbles: true })));
    assert.deepEqual(scrollPositions, [28]);

    act(() => mounted.container.dispatchEvent(new window.Event('scroll', { bubbles: true })));
    assert.deepEqual(scrollPositions, [28]);
  } finally {
    mounted.unmount();
  }
});
