import assert from 'node:assert/strict';
import test from 'node:test';

import {
  buildNotesUrl,
  getApiBaseUrl,
  getDisplayNotes,
  getNotesRequestContext,
  InvalidNotesResponseError,
  parsePublicNotesResponse,
  requestPublicNotes,
  NotesRequestError,
} from '../src/popup/services/publicNotes.ts';

const broadContext = { type: 'broad_area', label: 'Greater Noida, Uttar Pradesh' };
const placeContext = { type: 'place_page_unverified', label: 'GP Hotel & Banquet' };
const centerContext = { type: 'map_center', latitude: 26.8178068, longitude: 80.9071453 };
const note = {
  id: '8bc2b4ce-4277-4139-825d-c559694c8524',
  title: 'Tea stall behind the bus stop',
  body: 'The family-run stall is open early.',
  category: 'hidden_gem',
  author: { display_name: 'Mira' },
  created_at: '2026-10-20T08:30:00Z',
  helpful_vote_count: 12,
  match: { type: 'unverified_reference_label', location_label: 'GP Hotel & Banquet' },
};

function envelope(context, overrides = {}) {
  return {
    data: {
      context: { type: context.type, ...(context.label ? { label: context.label } : {}), ...(context.latitude !== undefined ? { latitude: context.latitude, longitude: context.longitude } : {}) },
      exact: [],
      context_matches: [],
      nearby: [],
      page: { next_cursor: null, has_more: false },
      ...overrides,
    },
  };
}

test('parses a valid unverified-place response and preserves context_matches separately', () => {
  const response = parsePublicNotesResponse(
    envelope(placeContext, { context_matches: [note] }),
    placeContext,
  );
  assert.equal(response.context_matches[0].match.type, 'unverified_reference_label');
  assert.deepEqual(getDisplayNotes(response).map(({ group }) => group), ['context_matches']);
});

test('parses an empty result page', () => {
  assert.deepEqual(parsePublicNotesResponse(envelope(broadContext), broadContext).context_matches, []);
});

test('keeps broad-area matches out of exact results', () => {
  const areaNote = { ...note, match: { type: 'broad_area', location_label: broadContext.label } };
  const response = parsePublicNotesResponse(envelope(broadContext, { context_matches: [areaNote] }), broadContext);
  assert.equal(response.exact.length, 0);
  assert.equal(getDisplayNotes(response)[0].group, 'context_matches');
});

test('parses nearby notes with distance and rejects presenting them as exact', () => {
  const nearbyNote = { ...note, match: { type: 'nearby', location_label: 'Bus stop near the market', distance_m: 640 } };
  const response = parsePublicNotesResponse(envelope(centerContext, { nearby: [nearbyNote] }), centerContext);
  assert.equal(getDisplayNotes(response)[0].group, 'nearby');
  assert.equal(response.nearby[0].match.distance_m, 640);
  assert.throws(
    () => parsePublicNotesResponse(envelope(centerContext, { exact: [{ ...nearbyNote, match: { ...nearbyNote.match, type: 'exact_location' } }] }), centerContext),
    InvalidNotesResponseError,
  );
});

test('rejects invalid, mismatched, and malformed API envelopes', () => {
  assert.throws(() => parsePublicNotesResponse({}, broadContext), InvalidNotesResponseError);
  assert.throws(() => parsePublicNotesResponse(envelope(placeContext), broadContext), InvalidNotesResponseError);
  assert.throws(
    () => parsePublicNotesResponse(envelope(placeContext, { context_matches: [{ ...note, helpful_vote_count: -2 }] }), placeContext),
    InvalidNotesResponseError,
  );
});

test('only sends context the extension can establish and never reuses Maps place IDs', () => {
  assert.deepEqual(getNotesRequestContext({ status: 'broad-area', sourceHostname: 'www.google.com', source: 'search-query', label: broadContext.label }), broadContext);
  assert.deepEqual(getNotesRequestContext({ status: 'place-page-unverified', sourceHostname: 'www.google.com', label: placeContext.label }), placeContext);
  assert.deepEqual(getNotesRequestContext({ status: 'map-center', sourceHostname: 'www.google.com', mapCenter: { latitude: centerContext.latitude, longitude: centerContext.longitude } }), centerContext);
  assert.equal(getNotesRequestContext({ status: 'unverified-place-reference', sourceHostname: 'www.google.com', placeId: 'ChIJsomething' }), null);
  assert.equal(getNotesRequestContext({ status: 'maps-no-location', sourceHostname: 'www.google.com' }), null);
});

test('builds exactly one API selector with bounded map-center radius and pagination', () => {
  const broadUrl = buildNotesUrl('http://127.0.0.1:8080/api/v1', broadContext, 'next-page').searchParams;
  assert.equal(broadUrl.get('context_type'), 'broad_area');
  assert.equal(broadUrl.get('label'), broadContext.label);
  assert.equal(broadUrl.get('location_id'), null);
  assert.equal(broadUrl.get('cursor'), 'next-page');

  const centerUrl = buildNotesUrl('http://127.0.0.1:8080/api/v1', centerContext).searchParams;
  assert.equal(centerUrl.get('context_type'), 'map_center');
  assert.equal(centerUrl.get('radius_m'), '5000');
  assert.equal(centerUrl.get('label'), null);
  assert.equal(centerUrl.get('location_id'), null);
});

test('development endpoint defaults to loopback and production requires configuration', () => {
  assert.equal(getApiBaseUrl(true), 'http://127.0.0.1:8080/api/v1');
  assert.throws(() => getApiBaseUrl(false));
});

test('fetches only the public notes route without credentials and validates its JSON response', async () => {
  let requestedUrl;
  let requestOptions;
  const payload = envelope(placeContext, { context_matches: [note] });
  const page = await requestPublicNotes(
    'https://api.streetmap.in/api/v1',
    placeContext,
    new globalThis.AbortController().signal,
    undefined,
    async (url, options) => {
      requestedUrl = url;
      requestOptions = options;
      return new globalThis.Response(JSON.stringify(payload), {
        status: 200,
        headers: { 'content-type': 'application/json; charset=utf-8' },
      });
    },
  );
  assert.equal(requestedUrl.pathname, '/api/v1/notes');
  assert.equal(requestedUrl.searchParams.get('context_type'), 'place_page_unverified');
  assert.equal(requestedUrl.searchParams.get('label'), 'GP Hotel & Banquet');
  assert.equal(requestOptions.credentials, 'omit');
  assert.equal(page.context_matches.length, 1);
});

test('distinguishes service failures from invalid response content', async () => {
  await assert.rejects(
    () => requestPublicNotes('https://api.streetmap.in/api/v1', broadContext, new globalThis.AbortController().signal, undefined, async () => new globalThis.Response('{}', { status: 503 })),
    NotesRequestError,
  );
  await assert.rejects(
    () => requestPublicNotes('https://api.streetmap.in/api/v1', broadContext, new globalThis.AbortController().signal, undefined, async () => new globalThis.Response('<html>gateway</html>', { headers: { 'content-type': 'text/html' } })),
    InvalidNotesResponseError,
  );
});
