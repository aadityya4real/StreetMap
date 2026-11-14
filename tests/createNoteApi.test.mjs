import assert from 'node:assert/strict';
import test from 'node:test';

import {
  CreateNoteApiError,
  createNoteAttempt,
} from '../src/popup/services/createNote.ts';

const idempotencyKey = '8bc2b4ce-4277-4139-825d-c559694c8524';
const noteId = '8bc2b4ce-4277-4139-825d-c559694c8525';
const accessToken = 'synthetic-access-token-for-tests';
const request = {
  location: { type: 'unverified', label: 'Maps URL place label' },
  title: 'A useful local secret',
  body: 'Use the marked public path by the garden.',
  category: 'hidden_gem',
};

const createdNote = {
  id: noteId,
  title: request.title,
  body: request.body,
  category: request.category,
  author: { display_name: 'StreetMap member' },
  created_at: '2026-11-10T08:30:00Z',
  helpful_vote_count: 0,
  match: { type: 'unverified_reference_label', location_label: 'Maps URL place label' },
};

function successResponse(status = 201, note = createdNote) {
  return new globalThis.Response(JSON.stringify({ data: { note } }), {
    status,
    headers: { 'content-type': 'application/json; charset=utf-8' },
  });
}

function failureResponse(status, error, headers = {}) {
  return new globalThis.Response(JSON.stringify({ error }), {
    status,
    headers: { 'content-type': 'application/json; charset=utf-8', ...headers },
  });
}

function dependencies(overrides = {}) {
  return {
    getApiBaseUrl: () => 'http://127.0.0.1:8080/api/v1',
    getAccessToken: async () => accessToken,
    fetcher: async () => successResponse(),
    createIdempotencyKey: () => idempotencyKey,
    ...overrides,
  };
}

test('sends the exact authenticated backend request contract', async () => {
  let requestedUrl;
  let requestOptions;
  const attempt = createNoteAttempt(request, dependencies({
    fetcher: async (url, options) => {
      requestedUrl = url;
      requestOptions = options;
      return successResponse();
    },
  }));

  const result = await attempt.submit();

  assert.equal(requestedUrl.href, 'http://127.0.0.1:8080/api/v1/notes');
  assert.equal(requestOptions.method, 'POST');
  assert.equal(requestOptions.headers.Authorization, `Bearer ${accessToken}`);
  assert.equal(requestOptions.headers['Content-Type'], 'application/json');
  assert.equal(requestOptions.headers.Accept, 'application/json');
  assert.equal(requestOptions.headers['Idempotency-Key'], idempotencyKey);
  assert.equal(requestOptions.credentials, 'omit');
  assert.deepEqual(JSON.parse(requestOptions.body), {
    location: { type: 'unverified', label: 'Maps URL place label' },
    title: 'A useful local secret',
    body: 'Use the marked public path by the garden.',
    category: 'hidden_gem',
  });
  assert.equal(requestOptions.body.includes('author'), false);
  assert.equal(requestOptions.body.includes('user_id'), false);
  assert.equal(requestOptions.body.includes('status'), false);
  assert.equal(result.note.id, noteId);
  assert.equal(result.replayed, false);
});

test('default browser fetch wrapper invokes fetch without a dependency-object receiver', async () => {
  const originalFetch = globalThis.fetch;
  let receivedUrl;
  globalThis.fetch = async function (input, options) {
    assert.equal(this, globalThis, 'the global fetch function must receive its browser-global receiver');
    receivedUrl = new globalThis.URL(input);
    assert.equal(options.method, 'POST');
    return successResponse();
  };

  try {
    const attempt = createNoteAttempt(request, {
      getApiBaseUrl: () => 'http://127.0.0.1:8080/api/v1',
      getAccessToken: async () => accessToken,
      createIdempotencyKey: () => idempotencyKey,
    });
    await attempt.submit();
    assert.equal(receivedUrl.href, 'http://127.0.0.1:8080/api/v1/notes');
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test('returns a clean authentication failure without making a request when no token exists', async () => {
  let requestCount = 0;
  const attempt = createNoteAttempt(request, dependencies({
    getAccessToken: async () => null,
    fetcher: async () => {
      requestCount++;
      return successResponse();
    },
  }));

  await assert.rejects(attempt.submit(), (error) => {
    assert.ok(error instanceof CreateNoteApiError);
    assert.equal(error.kind, 'authentication');
    assert.match(error.message, /session is missing or expired/i);
    assert.equal(error.message.includes(accessToken), false);
    return true;
  });
  assert.equal(requestCount, 0);
});

test('does not disguise a Supabase session refresh error as a missing login or API failure', async () => {
  let requestCount = 0;
  const attempt = createNoteAttempt(request, dependencies({
    getAccessToken: async () => { throw new Error(`synthetic provider error ${accessToken}`); },
    fetcher: async () => {
      requestCount++;
      return successResponse();
    },
  }));

  await assert.rejects(attempt.submit(), (error) => {
    assert.equal(error.kind, 'authentication-provider-unavailable');
    assert.equal(error.diagnosticCode, 'SUPABASE_SESSION_REFRESH_FAILED');
    assert.match(error.message, /could not refresh your session with Supabase/i);
    assert.equal(error.message.includes(accessToken), false);
    return true;
  });
  assert.equal(requestCount, 0);
});

test('maps an expired-token response to a clean sign-in-again error', async () => {
  let requestCount = 0;
  const attempt = createNoteAttempt(request, dependencies({
    fetcher: async () => {
      requestCount++;
      return failureResponse(401, { code: 'unauthorized', message: 'token details must not be exposed' }, {
        'Access-Control-Allow-Origin': 'chrome-extension://ifepahgidkeebpgeackolcjlcobgoaid',
      });
    },
  }));
  await assert.rejects(attempt.submit(), (error) => {
    assert.equal(error.kind, 'authentication');
    assert.equal(error.diagnosticCode, 'HTTP_401_UNAUTHORIZED');
    assert.match(error.message, /API rejected the StreetMap session/i);
    assert.equal(error.message.includes('token details'), false);
    return true;
  });
  assert.equal(requestCount, 1, 'a readable HTTP 401 must not trigger the fetch-rejection health diagnostic');
});

test('serializes only the selected location contract for all four backend location types', async (t) => {
  const cases = [
    {
      name: 'verified location',
      location: { type: 'verified', location_id: '00000000-0000-4000-8000-000000000001' },
      expected: { type: 'verified', location_id: '00000000-0000-4000-8000-000000000001' },
    },
    {
      name: 'unverified place label',
      location: { type: 'unverified', label: 'URL-derived place name' },
      expected: { type: 'unverified', label: 'URL-derived place name' },
    },
    {
      name: 'broad context',
      location: { type: 'broad', label: 'A neighbourhood' },
      expected: { type: 'broad', label: 'A neighbourhood' },
    },
    {
      name: 'user-confirmed coordinates',
      location: { type: 'coordinates', latitude: 28.5, longitude: 77.2, user_confirmed: true },
      expected: { type: 'coordinates', latitude: 28.5, longitude: 77.2, user_confirmed: true },
    },
  ];
  for (const item of cases) {
    await t.test(item.name, async () => {
      let body;
      const attempt = createNoteAttempt({ ...request, location: item.location }, dependencies({
        fetcher: async (_url, options) => {
          body = JSON.parse(options.body);
          return successResponse();
        },
      }));
      await attempt.submit();
      assert.deepEqual(body.location, item.expected);
      assert.deepEqual(Object.keys(body).sort(), ['body', 'category', 'location', 'title']);
    });
  }
});

test('validates location identifiers and requires confirmed valid coordinates before any request', () => {
  assert.throws(
    () => createNoteAttempt({ ...request, location: { type: 'verified', location_id: 'ChIJ-not-a-StreetMap-UUID' } }, dependencies()),
    (error) => error instanceof CreateNoteApiError && error.kind === 'validation' && Boolean(error.fieldErrors.location),
    'verified IDs must have the backend UUID shape; Google Place IDs are not accepted',
  );

  for (const location of [
    { type: 'coordinates', latitude: 28.5, longitude: 77.2 },
    { type: 'coordinates', latitude: 28.5, longitude: 77.2, user_confirmed: false },
    { type: 'coordinates', latitude: 91, longitude: 77.2, user_confirmed: true },
    { type: 'coordinates', latitude: 28.5, longitude: 181, user_confirmed: true },
    { type: 'broad', label: '' },
    { type: 'unverified', label: 'x'.repeat(201) },
    undefined,
  ]) {
    assert.throws(
      () => createNoteAttempt({ ...request, location }, dependencies()),
      (error) => error instanceof CreateNoteApiError && error.kind === 'validation',
    );
  }
});

test('converts backend validation fields to safe actionable messages', async () => {
  const attempt = createNoteAttempt(request, dependencies({
    fetcher: async () => failureResponse(400, {
      code: 'validation_error',
      message: 'sensitive response text must not be shown',
      fields: { title: 'raw backend details', ignored_field: 'do not expose' },
    }),
  }));

  await assert.rejects(attempt.submit(), (error) => {
    assert.equal(error.kind, 'validation');
    assert.equal(error.fieldErrors.title, 'Title must contain 3 to 120 characters.');
    assert.equal('ignored_field' in (error.fieldErrors ?? {}), false);
    assert.equal(error.message.includes('sensitive response text'), false);
    assert.equal(error.message.includes('raw backend details'), false);
    return true;
  });
});

test('classifies rate limits and idempotency conflicts without exposing response text', async (t) => {
  await t.test('rate limit', async () => {
    const attempt = createNoteAttempt(request, dependencies({
      fetcher: async () => failureResponse(429, { code: 'note_creation_rate_limited', message: 'private details' }, { 'retry-after': '120' }),
    }));
    await assert.rejects(attempt.submit(), (error) => {
      assert.equal(error.kind, 'rate-limited');
      assert.equal(error.retryAfterSeconds, 120);
      assert.equal(error.message.includes('private details'), false);
      return true;
    });
  });

  await t.test('idempotency conflict', async () => {
    const attempt = createNoteAttempt(request, dependencies({
      fetcher: async () => failureResponse(409, { code: 'idempotency_conflict', message: 'private details' }),
    }));
    await assert.rejects(attempt.submit(), (error) => {
      assert.equal(error.kind, 'conflict');
      assert.equal(error.message.includes('private details'), false);
      return true;
    });
  });
});

test('does not misclassify unavailable backend authentication as an expired user session', async () => {
  const attempt = createNoteAttempt(request, dependencies({
    fetcher: async () => failureResponse(503, { code: 'authentication_unavailable', message: 'internal detail' }),
  }));
  await assert.rejects(attempt.submit(), (error) => {
    assert.equal(error.kind, 'authentication-unavailable');
    assert.equal(error.diagnosticCode, 'HTTP_503_AUTHENTICATION_UNAVAILABLE');
    assert.match(error.message, /Supabase JWT verification settings/i);
    assert.equal(error.message.includes('internal detail'), false);
    return true;
  });
});

test('classifies an unreachable API separately from a rejected note request when health responds', async (t) => {
  await t.test('health check cannot reach API', async () => {
    const seenRequests = [];
    const attempt = createNoteAttempt(request, dependencies({
      fetcher: async (url, options) => {
        seenRequests.push({ url: url.href, options });
        throw new TypeError(`synthetic network failure containing ${accessToken}`);
      },
    }));

    await assert.rejects(attempt.submit(), (error) => {
      assert.equal(error.kind, 'network');
      assert.equal(error.diagnosticCode, 'API_UNREACHABLE_OR_PERMISSION');
      assert.match(error.message, /health endpoint/i);
      assert.equal(error.message.includes(accessToken), false);
      return true;
    });
    assert.deepEqual(seenRequests.map(({ url }) => url), [
      'http://127.0.0.1:8080/api/v1/notes',
      'http://127.0.0.1:8080/api/v1/health',
    ]);
    const healthOptions = seenRequests[1].options;
    assert.equal(healthOptions.method, 'GET');
    assert.deepEqual(healthOptions.headers, { Accept: 'application/json' });
    assert.equal(healthOptions.headers.Authorization, undefined);
    assert.equal(healthOptions.credentials, 'omit');
    assert.equal(healthOptions.cache, 'no-store');
  });

  await t.test('API health responds but note request is rejected', async () => {
    const seenRequests = [];
    const attempt = createNoteAttempt(request, dependencies({
      fetcher: async (url, options) => {
        seenRequests.push({ url: url.href, options });
        if (url.pathname.endsWith('/notes')) {
          throw new TypeError('synthetic browser rejection');
        }
        return new globalThis.Response('', { status: 200 });
      },
    }));

    await assert.rejects(attempt.submit(), (error) => {
      assert.equal(error.kind, 'network');
      assert.equal(error.diagnosticCode, 'NOTE_REQUEST_REJECTED_API_RESPONDS');
      assert.match(error.message, /API is responding/i);
      assert.match(error.message, /development CORS policy/i);
      assert.doesNotMatch(error.message, /host permission/i);
      return true;
    });
    assert.equal(seenRequests.length, 2);
    assert.equal(seenRequests[1].options.headers.Authorization, undefined);
  });

  await t.test('retains the original POST fetch error as a non-displayed cause', async () => {
    const originalFetchError = new TypeError(`synthetic browser failure ${accessToken}`);
    const attempt = createNoteAttempt(request, dependencies({
      fetcher: async (url) => {
        if (url.pathname.endsWith('/notes')) throw originalFetchError;
        return new globalThis.Response('', { status: 200 });
      },
    }));

    await assert.rejects(attempt.submit(), (error) => {
      assert.equal(error.cause, originalFetchError);
      assert.equal(error.message.includes(accessToken), false);
      assert.equal(JSON.stringify(error).includes(accessToken), false);
      assert.equal(error.diagnosticCode, 'NOTE_REQUEST_REJECTED_API_RESPONDS');
      return true;
    });
  });

  await t.test('API health responds with a failure status', async () => {
    const attempt = createNoteAttempt(request, dependencies({
      fetcher: async (url) => url.pathname.endsWith('/notes')
        ? Promise.reject(new TypeError('synthetic browser rejection'))
        : new globalThis.Response('', { status: 503 }),
    }));

    await assert.rejects(attempt.submit(), (error) => {
      assert.equal(error.kind, 'network');
      assert.equal(error.diagnosticCode, 'API_HEALTH_CHECK_FAILED');
      assert.match(error.message, /HTTP 503/);
      return true;
    });
  });
});

test('classifies safe HTTP error responses without exposing response bodies', async (t) => {
  const cases = [
    { status: 403, payload: { code: 'account_unavailable', message: 'private detail' }, kind: 'account-unavailable', diagnostic: 'HTTP_403_ACCOUNT_UNAVAILABLE' },
    { status: 500, payload: { code: 'internal_error', message: 'private detail' }, kind: 'server', diagnostic: 'HTTP_SERVER_ERROR' },
    { status: 418, payload: { code: 'unknown', message: 'private detail' }, kind: 'server', diagnostic: 'HTTP_REQUEST_REJECTED' },
  ];
  for (const item of cases) {
    await t.test(`HTTP ${item.status}`, async () => {
      const attempt = createNoteAttempt(request, dependencies({
        fetcher: async () => failureResponse(item.status, item.payload),
      }));
      await assert.rejects(attempt.submit(), (error) => {
        assert.equal(error.kind, item.kind);
        assert.equal(error.diagnosticCode, item.diagnostic);
        assert.equal(error.message.includes('private detail'), false);
        return true;
      });
    });
  }
});

test('retries the same request with the same idempotency key and gives a new attempt a new key', async () => {
  const keys = [idempotencyKey, '8bc2b4ce-4277-4139-825d-c559694c8526'];
  let fetchCount = 0;
  const requestKeys = [];
  const deps = dependencies({
    createIdempotencyKey: () => keys.shift(),
    fetcher: async (_url, options) => {
      requestKeys.push(options.headers['Idempotency-Key']);
      fetchCount++;
      return fetchCount === 1
        ? failureResponse(503, { code: 'internal_error', message: 'internal diagnostic' })
        : successResponse(200);
    },
  });

  const firstAttempt = createNoteAttempt(request, deps);
  await assert.rejects(firstAttempt.submit(), (error) => error.kind === 'server');
  const replay = await firstAttempt.submit();
  assert.equal(replay.replayed, true);

  const secondAttempt = createNoteAttempt(request, deps);
  await secondAttempt.submit();
  assert.deepEqual(requestKeys, [idempotencyKey, idempotencyKey, '8bc2b4ce-4277-4139-825d-c559694c8526']);
});

test('rejects a malformed successful response without leaking its body', async () => {
  const attempt = createNoteAttempt(request, dependencies({
    fetcher: async () => new globalThis.Response(JSON.stringify({ data: { note: { access_token: accessToken } } }), {
      status: 201,
      headers: { 'content-type': 'application/json' },
    }),
  }));
  await assert.rejects(attempt.submit(), (error) => {
    assert.equal(error.kind, 'invalid-response');
    assert.equal(error.message.includes(accessToken), false);
    return true;
  });
});
