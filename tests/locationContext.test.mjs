import assert from 'node:assert/strict';
import test from 'node:test';

import {
  LOCATION_CONTEXT_RESPONSE,
  isGoogleMapsPageUrl,
  parseLocationContextResponse,
  parseMapsPageUrl,
  resolveActiveLocationContext,
} from '../src/shared/locationContext.ts';

const mapsSearchUrl = 'https://www.google.com/maps/search/';

test('rejects non-Google Maps URLs and lookalike hosts', () => {
  assert.equal(isGoogleMapsPageUrl('https://example.com/maps'), false);
  assert.equal(isGoogleMapsPageUrl('https://www.google.com.evil.test/maps'), false);
  assert.equal(parseMapsPageUrl('https://maps.google.com.evil.test/maps'), null);
});

test('detects a supported Maps homepage without inventing a place', () => {
  assert.deepEqual(parseMapsPageUrl('https://www.google.com/maps'), {
    status: 'maps-no-location',
    sourceHostname: 'www.google.com',
  });
});

test('a general search query is not treated as a selected place', () => {
  assert.deepEqual(parseMapsPageUrl(`${mapsSearchUrl}?api=1&query=coffee+shops`), {
    status: 'maps-no-location',
    sourceHostname: 'www.google.com',
  });
});

test('recognizes the documented place-ID search format without making a label', () => {
  const context = parseMapsPageUrl(
    `${mapsSearchUrl}?api=1&query=coffee&query_place_id=ChIJN1t_tDeuEmsRUsoyG83frY4`,
  );

  assert.deepEqual(context, {
    status: 'location-available',
    sourceHostname: 'www.google.com',
    location: { kind: 'place-id', placeId: 'ChIJN1t_tDeuEmsRUsoyG83frY4' },
  });
  assert.equal('label' in (context?.location ?? {}), false);
});

test('recognizes explicit coordinate searches and validates geographic ranges', () => {
  assert.deepEqual(parseMapsPageUrl(`${mapsSearchUrl}?api=1&query=-33.8569%2C151.2152`), {
    status: 'location-available',
    sourceHostname: 'www.google.com',
    location: { kind: 'coordinates', latitude: -33.8569, longitude: 151.2152 },
  });
  assert.equal(parseMapsPageUrl(`${mapsSearchUrl}?api=1&query=91%2C0`)?.status, 'maps-no-location');
  assert.equal(parseMapsPageUrl(`${mapsSearchUrl}?api=1&query=0%2C181`)?.status, 'maps-no-location');
  assert.equal(parseMapsPageUrl(`${mapsSearchUrl}?api=1&query=1e2%2C0`)?.status, 'maps-no-location');
});

test('map centers, missing fields, duplicate fields, and undocumented routes stay unconfirmed', () => {
  assert.equal(
    parseMapsPageUrl('https://www.google.com/maps/@?api=1&map_action=map&center=10%2C20')?.status,
    'maps-no-location',
  );
  assert.equal(parseMapsPageUrl(`${mapsSearchUrl}?api=1&query_place_id=somePlaceId`)?.status, 'maps-no-location');
  assert.equal(parseMapsPageUrl(`${mapsSearchUrl}?api=1&query=1%2C2&query=3%2C4`)?.status, 'maps-no-location');
  assert.equal(parseMapsPageUrl('https://www.google.com/maps/place/Cafe/@10,20,15z')?.status, 'maps-no-location');
});

test('validates and normalizes content-script responses from untrusted input', () => {
  const response = {
    type: LOCATION_CONTEXT_RESPONSE,
    context: {
      status: 'location-available',
      sourceHostname: 'www.google.com',
      location: { kind: 'coordinates', latitude: 12.5, longitude: 77.6, label: 'Untrusted label' },
      extra: 'ignored',
    },
  };
  assert.deepEqual(parseLocationContextResponse(response), {
    status: 'location-available',
    sourceHostname: 'www.google.com',
    location: { kind: 'coordinates', latitude: 12.5, longitude: 77.6 },
  });
  assert.equal(parseLocationContextResponse({ ...response, context: { ...response.context, sourceHostname: 'evil.test' } }), null);
  assert.equal(
    parseLocationContextResponse({
      ...response,
      context: { ...response.context, location: { kind: 'coordinates', latitude: 95, longitude: 0 } },
    }),
    null,
  );
});

test('reports a non-Maps tab without messaging it', async () => {
  let messages = 0;
  const context = await resolveActiveLocationContext({
    async getActiveTab() { return { id: 7, url: 'https://example.com/' }; },
    async requestContext() { messages += 1; return null; },
  }, 0);

  assert.deepEqual(context, { status: 'maps-unavailable' });
  assert.equal(messages, 0);
});

test('handles missing tabs, tab-query failures, content-script failures, and malformed responses', async () => {
  const noTab = await resolveActiveLocationContext({
    async getActiveTab() { return undefined; },
    async requestContext() { return null; },
  }, 0);
  assert.deepEqual(noTab, { status: 'maps-unavailable' });

  const queryFailure = await resolveActiveLocationContext({
    async getActiveTab() { throw new Error('restricted page'); },
    async requestContext() { return null; },
  }, 0);
  assert.deepEqual(queryFailure, { status: 'error', reason: 'active-tab' });

  let calls = 0;
  const contentFailure = await resolveActiveLocationContext({
    async getActiveTab() { return { id: 5, url: `${mapsSearchUrl}?api=1&query=coffee` }; },
    async requestContext() { calls += 1; throw new Error('no receiving end'); },
  }, 0);
  assert.deepEqual(contentFailure, { status: 'error', reason: 'content-script' });
  assert.equal(calls, 2);

  const malformedResponse = await resolveActiveLocationContext({
    async getActiveTab() { return { id: 5, url: `${mapsSearchUrl}?api=1&query=coffee` }; },
    async requestContext() { return { type: 'unexpected' }; },
  }, 0);
  assert.deepEqual(malformedResponse, { status: 'error', reason: 'invalid-response' });
});

test('uses the content script response for a fresh SPA URL and rejects a host mismatch', async () => {
  const result = await resolveActiveLocationContext({
    async getActiveTab() { return { id: 5, url: `${mapsSearchUrl}?api=1&query=coffee` }; },
    async requestContext() {
      return {
        type: LOCATION_CONTEXT_RESPONSE,
        context: {
          status: 'location-available',
          sourceHostname: 'www.google.com',
          location: { kind: 'place-id', placeId: 'ChIJN1t_tDeuEmsRUsoyG83frY4' },
        },
      };
    },
  }, 0);
  assert.deepEqual(result, {
    status: 'location-available',
    sourceHostname: 'www.google.com',
    location: { kind: 'place-id', placeId: 'ChIJN1t_tDeuEmsRUsoyG83frY4' },
  });

  const mismatch = await resolveActiveLocationContext({
    async getActiveTab() { return { id: 5, url: `${mapsSearchUrl}?api=1&query=coffee` }; },
    async requestContext() {
      return {
        type: LOCATION_CONTEXT_RESPONSE,
        context: { status: 'maps-no-location', sourceHostname: 'www.google.co.in' },
      };
    },
  }, 0);
  assert.deepEqual(mismatch, { status: 'error', reason: 'invalid-response' });
});
