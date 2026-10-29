import assert from 'node:assert/strict';
import test from 'node:test';

import {
  GOOGLE_MAPS_MATCH_PATTERNS,
  LOCATION_CONTEXT_RESPONSE,
  isGoogleMapsPageUrl,
  parseLocationContextResponse,
  parseMapsPageUrl,
  resolveActiveLocationContext,
} from '../src/shared/locationContext.ts';

const mapsSearchUrl = 'https://www.google.com/maps/search/';
const greaterNoidaUrl =
  'https://www.google.com/maps/place/Greater+Noida,+Uttar+Pradesh/@28.4983349,77.4334236,12z/data=!3m1!4b1';

function response(context) {
  return { type: LOCATION_CONTEXT_RESPONSE, context };
}

test('supports the exact www and apex Google Maps hosts in code and manifest matches', () => {
  assert.equal(isGoogleMapsPageUrl(greaterNoidaUrl), true);
  assert.equal(isGoogleMapsPageUrl(greaterNoidaUrl.replace('www.google.com', 'google.com')), true);
  assert.ok(GOOGLE_MAPS_MATCH_PATTERNS.includes('https://google.com/maps/*'));
  assert.ok(GOOGLE_MAPS_MATCH_PATTERNS.includes('https://www.google.com/maps/*'));
  assert.equal(isGoogleMapsPageUrl('https://www.google.com.evil.test/maps'), false);
  assert.equal(isGoogleMapsPageUrl('https://example.com/maps'), false);
});

test('GP Hotel place page decodes plus signs and encoded ampersands, with a separate map center', () => {
  const venueUrl =
    'https://www.google.com/maps/place/GP+Hotel+%26+Banquet/@26.8178068,80.9071453,15.26z/data=!4m2!3m1!1s0x0';
  const context = parseMapsPageUrl(venueUrl);
  assert.deepEqual(context, {
    status: 'place-page-unverified',
    sourceHostname: 'www.google.com',
    label: 'GP Hotel & Banquet',
    mapCenter: { latitude: 26.8178068, longitude: 80.9071453 },
  });
  assert.equal(context?.status === 'place-page-unverified' && 'coordinates' in context, false);
});

test('place-page labels decode percent-encoded spaces and remain unverified', () => {
  assert.deepEqual(
    parseMapsPageUrl('https://www.google.com/maps/place/Cafe%20with%20a%20View/'),
    {
      status: 'place-page-unverified',
      sourceHostname: 'www.google.com',
      label: 'Cafe with a View',
    },
  );
});

test('a place route without coordinates remains unverified and invents no point', () => {
  assert.deepEqual(
    parseMapsPageUrl('https://www.google.com/maps/place/Greater+Noida,+Uttar+Pradesh/'),
    {
      status: 'place-page-unverified',
      sourceHostname: 'www.google.com',
      label: 'Greater Noida, Uttar Pradesh',
    },
  );
});

test('map-center-only URL is separate from a selected place', () => {
  assert.deepEqual(
    parseMapsPageUrl('https://google.com/maps/@28.4983349,77.4334236,12z/data=!3m1!1e3'),
    {
      status: 'map-center',
      sourceHostname: 'google.com',
      mapCenter: { latitude: 28.4983349, longitude: 77.4334236 },
    },
  );
});

test('a general Maps search is broad search context, not a confirmed selection', () => {
  assert.deepEqual(parseMapsPageUrl(`${mapsSearchUrl}?api=1&query=coffee+shops`), {
    status: 'broad-area',
    sourceHostname: 'www.google.com',
    source: 'search-query',
    label: 'coffee shops',
  });
});

test('Maps homepage has no reliable place context', () => {
  assert.deepEqual(parseMapsPageUrl('https://www.google.com/maps'), {
    status: 'maps-no-location',
    sourceHostname: 'www.google.com',
  });
});

test('coordinate search text is not confused with a map-center coordinate', () => {
  assert.deepEqual(parseMapsPageUrl(`${mapsSearchUrl}?api=1&query=28.5%2C77.4`), {
    status: 'broad-area',
    sourceHostname: 'www.google.com',
    source: 'search-query',
    label: '28.5,77.4',
  });
});

test('documented query_place_id is a specific but unverified reference', () => {
  assert.deepEqual(
    parseMapsPageUrl(
      `${mapsSearchUrl}?api=1&query=coffee&query_place_id=ChIJN1t_tDeuEmsRUsoyG83frY4`,
    ),
    {
      status: 'unverified-place-reference',
      sourceHostname: 'www.google.com',
      placeId: 'ChIJN1t_tDeuEmsRUsoyG83frY4',
    },
  );
});

test('invalid map-center coordinates are omitted from place context and rejected by themselves', () => {
  const placeWithInvalidCenter = parseMapsPageUrl(
    'https://www.google.com/maps/place/GP+Hotel/@91,0,15z/',
  );
  assert.deepEqual(placeWithInvalidCenter, {
    status: 'place-page-unverified',
    sourceHostname: 'www.google.com',
    label: 'GP Hotel',
  });
  assert.equal(parseMapsPageUrl('https://www.google.com/maps/@91,0,12z')?.status, 'maps-no-location');
});

test('unsupported routes and missing query fields stay without reliable context', () => {
  assert.equal(parseMapsPageUrl('https://www.google.com/maps/dir/Origin/Target')?.status, 'maps-no-location');
  assert.equal(parseMapsPageUrl(`${mapsSearchUrl}?api=1&query_place_id=somePlaceId`)?.status, 'maps-no-location');
});

test('malformed percent encoding does not become a place label; valid center remains separate', () => {
  assert.deepEqual(
    parseMapsPageUrl('https://www.google.com/maps/place/GP+Hotel+%2G/@26.8,80.9,15z/'),
    {
      status: 'map-center',
      sourceHostname: 'www.google.com',
      mapCenter: { latitude: 26.8, longitude: 80.9 },
    },
  );
  assert.equal(parseMapsPageUrl('https://[invalid-url'), null);
});

test('validates and normalizes content-script context from untrusted input', () => {
  const valid = parseLocationContextResponse(
    response({
      status: 'broad-area',
      sourceHostname: 'www.google.com',
      source: 'search-query',
      label: ' Greater   Noida, Uttar Pradesh ',
      mapCenter: { latitude: 28.5, longitude: 77.4, extra: 'ignored' },
      extra: 'ignored',
    }),
  );
  assert.deepEqual(valid, {
    status: 'broad-area',
    sourceHostname: 'www.google.com',
    source: 'search-query',
    label: 'Greater Noida, Uttar Pradesh',
    mapCenter: { latitude: 28.5, longitude: 77.4 },
  });
  assert.equal(
    parseLocationContextResponse(
      response({
        status: 'map-center',
        sourceHostname: 'www.google.com',
        mapCenter: { latitude: 95, longitude: 0 },
      }),
    ),
    null,
  );
  assert.equal(
    parseLocationContextResponse(
      response({ status: 'broad-area', sourceHostname: 'www.google.com', source: 'search-query', label: '<script>' }),
    )?.status,
    'broad-area',
  );
  assert.equal(
    parseLocationContextResponse(
      response({ status: 'map-center', sourceHostname: 'evil.test', mapCenter: { latitude: 0, longitude: 0 } }),
    ),
    null,
  );
});

test('non-Maps and missing-tab cases avoid messaging or injection', async () => {
  let messages = 0;
  let injections = 0;
  const messenger = {
    async getActiveTab() { return { id: 7, url: 'https://example.com/' }; },
    async requestContext() { messages += 1; return null; },
    async injectContentScript() { injections += 1; },
  };
  assert.deepEqual(await resolveActiveLocationContext(messenger), { status: 'maps-unavailable' });
  assert.equal(messages, 0);
  assert.equal(injections, 0);

  assert.deepEqual(
    await resolveActiveLocationContext({
      async getActiveTab() { return undefined; },
      async requestContext() { messages += 1; return null; },
      async injectContentScript() { injections += 1; },
    }),
    { status: 'maps-unavailable' },
  );
});

test('recovers an already-open Maps tab by injecting the packaged script once', async () => {
  let requests = 0;
  let injections = 0;
  const context = {
    status: 'place-page-unverified',
    sourceHostname: 'www.google.com',
    label: 'Greater Noida, Uttar Pradesh',
    mapCenter: { latitude: 28.4983349, longitude: 77.4334236 },
  };
  const result = await resolveActiveLocationContext({
    async getActiveTab() { return { id: 5, url: greaterNoidaUrl }; },
    async requestContext() {
      requests += 1;
      if (requests === 1) throw new Error('Could not establish connection. Receiving end does not exist.');
      return response(context);
    },
    async injectContentScript(tabId) {
      assert.equal(tabId, 5);
      injections += 1;
    },
  });

  assert.deepEqual(result, context);
  assert.equal(requests, 2);
  assert.equal(injections, 1);
});

test('keeps a real error when active-tab lookup or recovery injection fails', async () => {
  const queryFailure = await resolveActiveLocationContext({
    async getActiveTab() { throw new Error('tab lookup failed'); },
    async requestContext() { return null; },
    async injectContentScript() {},
  });
  assert.deepEqual(queryFailure, { status: 'error', reason: 'active-tab' });

  let requests = 0;
  const injectionFailure = await resolveActiveLocationContext({
    async getActiveTab() { return { id: 5, url: greaterNoidaUrl }; },
    async requestContext() { requests += 1; throw new Error('Receiving end does not exist'); },
    async injectContentScript() { throw new Error('injection denied'); },
  });
  assert.deepEqual(injectionFailure, { status: 'error', reason: 'content-script' });
  assert.equal(requests, 1);
});

test('does not inject after unrelated messaging failures', async () => {
  let injections = 0;
  const result = await resolveActiveLocationContext({
    async getActiveTab() { return { id: 5, url: greaterNoidaUrl }; },
    async requestContext() { throw new Error('The message channel closed unexpectedly'); },
    async injectContentScript() { injections += 1; },
  });
  assert.deepEqual(result, { status: 'error', reason: 'content-script' });
  assert.equal(injections, 0);
});

test('rejects malformed responses and hostname mismatches instead of masking errors', async () => {
  const malformed = await resolveActiveLocationContext({
    async getActiveTab() { return { id: 5, url: greaterNoidaUrl }; },
    async requestContext() { return { type: 'unexpected' }; },
    async injectContentScript() { assert.fail('Should not inject after a response was received'); },
  });
  assert.deepEqual(malformed, { status: 'error', reason: 'invalid-response' });

  const mismatch = await resolveActiveLocationContext({
    async getActiveTab() { return { id: 5, url: greaterNoidaUrl }; },
    async requestContext() {
      return response({ status: 'maps-no-location', sourceHostname: 'maps.google.com' });
    },
    async injectContentScript() { assert.fail('Should not inject after a response was received'); },
  });
  assert.deepEqual(mismatch, { status: 'error', reason: 'invalid-response' });
});
