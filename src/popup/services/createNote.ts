import { getCurrentAccessToken } from '../auth/supabase';
import { isNoteCategory, type NoteCategory, type NoteMatchType, type PublicNote } from '../types/note';
import { getApiBaseUrl } from './publicNotes';

const NOTE_ID_PATTERN = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
const RFC3339_UTC_PATTERN = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?Z$/;

export type CreateNoteLocation =
  | { type: 'verified'; location_id: string }
  | { type: 'unverified' | 'broad'; label: string }
  | { type: 'coordinates'; latitude: number; longitude: number; user_confirmed: true; label?: string };

export interface CreateNoteRequest {
  location: CreateNoteLocation;
  title: string;
  body: string;
  category: NoteCategory;
}

export interface CreateNoteResult {
  note: PublicNote;
  replayed: boolean;
}

export type CreateNoteFailureKind =
  | 'authentication'
  | 'configuration'
  | 'validation'
  | 'rate-limited'
  | 'conflict'
  | 'location-not-found'
  | 'account-unavailable'
  | 'network'
  | 'server'
  | 'invalid-response';

export type CreateNoteField = 'location' | 'location.type' | 'location.label' | 'location.latitude' | 'location.longitude' | 'title' | 'body' | 'category';

export class CreateNoteApiError extends Error {
  constructor(
    readonly kind: CreateNoteFailureKind,
    message: string,
    readonly fieldErrors?: Partial<Record<CreateNoteField, string>>,
    readonly retryAfterSeconds?: number,
  ) {
    super(message);
    this.name = 'CreateNoteApiError';
  }
}

export interface CreateNoteAttempt {
  submit(signal?: AbortSignal): Promise<CreateNoteResult>;
}

export interface CreateNoteDependencies {
  getApiBaseUrl: () => string;
  getAccessToken: () => Promise<string | null>;
  fetcher: typeof fetch;
  createIdempotencyKey: () => string;
}

const defaultDependencies: CreateNoteDependencies = {
  getApiBaseUrl: () => getApiBaseUrl(import.meta.env.DEV),
  getAccessToken: getCurrentAccessToken,
  fetcher: fetch,
  createIdempotencyKey: () => globalThis.crypto.randomUUID(),
};

/** A submission attempt snapshots its payload and UUID so retries remain idempotent. */
export function createNoteAttempt(
  request: CreateNoteRequest,
  dependencies: Partial<CreateNoteDependencies> = {},
): CreateNoteAttempt {
  const resolved = { ...defaultDependencies, ...dependencies };
  const apiBaseUrl = resolveApiBaseUrl(resolved.getApiBaseUrl);
  const idempotencyKey = resolved.createIdempotencyKey();
  if (!NOTE_ID_PATTERN.test(idempotencyKey)) {
    throw new CreateNoteApiError('configuration', 'StreetMap could not prepare a safe note submission. Please try again.');
  }
  const body = serializeCreateNoteRequest(request);

  return {
    async submit(signal) {
      let accessToken: string | null;
      try {
        accessToken = await resolved.getAccessToken();
      } catch {
        throw authenticationError();
      }
      if (!accessToken) throw authenticationError();

      let response: Response;
      try {
        response = await resolved.fetcher(new URL(`${apiBaseUrl.replace(/\/$/, '')}/notes`), {
          method: 'POST',
          headers: {
            Accept: 'application/json',
            Authorization: `Bearer ${accessToken}`,
            'Content-Type': 'application/json',
            'Idempotency-Key': idempotencyKey,
          },
          credentials: 'omit',
          cache: 'no-store',
          body,
          signal,
        });
      } catch {
        throw new CreateNoteApiError('network', 'StreetMap could not reach the notes service. Check your connection and retry.');
      }

      if (response.status !== 200 && response.status !== 201) {
        throw await parseCreateNoteError(response);
      }
      if (!response.headers.get('content-type')?.toLowerCase().includes('application/json')) {
        throw invalidResponseError();
      }

      let payload: unknown;
      try {
        payload = await response.json();
      } catch {
        throw invalidResponseError();
      }
      const note = parseCreatedNote(payload);
      return { note, replayed: response.status === 200 };
    },
  };
}

function resolveApiBaseUrl(getBaseUrl: () => string): string {
  try {
    return getBaseUrl();
  } catch {
    throw new CreateNoteApiError('configuration', 'Note sharing is not configured for this StreetMap build.');
  }
}

function serializeCreateNoteRequest(request: CreateNoteRequest): string {
  if (!isNoteCategory(request.category)) {
    throw new CreateNoteApiError('validation', 'Choose a supported note category.', { category: 'Choose a supported category.' });
  }

  let location: Record<string, string | number | boolean>;
  switch (request.location.type) {
    case 'verified':
      location = { type: 'verified', location_id: request.location.location_id };
      break;
    case 'unverified':
    case 'broad':
      location = { type: request.location.type, label: request.location.label };
      break;
    case 'coordinates':
      location = {
        type: 'coordinates',
        latitude: request.location.latitude,
        longitude: request.location.longitude,
        user_confirmed: request.location.user_confirmed,
        ...(request.location.label === undefined ? {} : { label: request.location.label }),
      };
      break;
    default:
      throw new CreateNoteApiError('validation', 'Review the selected location and try again.', { location: 'Review the selected location.' });
  }

  return JSON.stringify({
    location,
    title: request.title,
    body: request.body,
    category: request.category,
  });
}

function authenticationError(): CreateNoteApiError {
  return new CreateNoteApiError('authentication', 'Your StreetMap session is missing or expired. Sign in and try again.');
}

async function parseCreateNoteError(response: Response): Promise<CreateNoteApiError> {
  let errorCode: string | undefined;
  let fields: Record<string, unknown> | undefined;
  try {
    const payload: unknown = await response.json();
    if (isRecord(payload) && isRecord(payload.error)) {
      errorCode = typeof payload.error.code === 'string' ? payload.error.code : undefined;
      fields = isRecord(payload.error.fields) ? payload.error.fields : undefined;
    }
  } catch {
    // The status code remains sufficient to return a safe generic error.
  }

  if (response.status === 401 || errorCode === 'unauthorized') {
    return authenticationError();
  }
  if (response.status === 403 || errorCode === 'account_unavailable') {
    return new CreateNoteApiError('account-unavailable', 'This StreetMap account cannot share notes right now.');
  }
  if (response.status === 404 || errorCode === 'location_not_found') {
    return new CreateNoteApiError('location-not-found', 'The selected StreetMap location could not be found. Refresh the location and try again.');
  }
  if (response.status === 409 || errorCode === 'idempotency_conflict') {
    return new CreateNoteApiError('conflict', 'This submission conflicts with an earlier request. Start a new note submission to continue.');
  }
  if (response.status === 429 || errorCode === 'note_creation_rate_limited') {
    return new CreateNoteApiError(
      'rate-limited',
      'You have shared notes recently. Please wait before trying again.',
      undefined,
      parseRetryAfter(response.headers.get('retry-after')),
    );
  }
  if (response.status === 400 || errorCode === 'validation_error') {
    return new CreateNoteApiError('validation', 'Review the note details and try again.', safeFieldErrors(fields));
  }
  if (response.status >= 500) {
    return new CreateNoteApiError('server', 'StreetMap could not save this note right now. Please try again.');
  }
  return new CreateNoteApiError('server', 'StreetMap could not complete this note request. Please try again.');
}

function safeFieldErrors(fields?: Record<string, unknown>): Partial<Record<CreateNoteField, string>> | undefined {
  if (!fields) return undefined;
  const messages: Partial<Record<CreateNoteField, string>> = {};
  const safeMessages: Record<CreateNoteField, string> = {
    location: 'Review the selected location.',
    'location.type': 'Choose a supported location type.',
    'location.label': 'Location label must contain 1 to 200 characters.',
    'location.latitude': 'Latitude must be between -90 and 90.',
    'location.longitude': 'Longitude must be between -180 and 180.',
    title: 'Title must contain 3 to 120 characters.',
    body: 'Description must contain 1 to 5,000 characters.',
    category: 'Choose a supported category.',
  };
  for (const field of Object.keys(safeMessages) as CreateNoteField[]) {
    if (typeof fields[field] === 'string') messages[field] = safeMessages[field];
  }
  return Object.keys(messages).length > 0 ? messages : undefined;
}

function parseRetryAfter(value: string | null): number | undefined {
  if (!value || !/^\d{1,6}$/.test(value)) return undefined;
  const seconds = Number(value);
  return Number.isSafeInteger(seconds) && seconds > 0 ? seconds : undefined;
}

function parseCreatedNote(value: unknown): PublicNote {
  if (!isRecord(value) || !isRecord(value.data) || !isRecord(value.data.note)) throw invalidResponseError();
  const note = value.data.note;
  if (!isRecord(note.author) || !isRecord(note.match)) throw invalidResponseError();
  const matchType = note.match.type;
  const distanceM = note.match.distance_m;
  if (
    typeof note.id !== 'string' || !NOTE_ID_PATTERN.test(note.id) ||
    typeof note.title !== 'string' || note.title.length < 3 || note.title.length > 120 ||
    typeof note.body !== 'string' || note.body.length < 1 || note.body.length > 5000 ||
    !isNoteCategory(note.category) ||
    typeof note.author.display_name !== 'string' || note.author.display_name.length < 1 || note.author.display_name.length > 100 ||
    typeof note.created_at !== 'string' || !RFC3339_UTC_PATTERN.test(note.created_at) || !Number.isFinite(Date.parse(note.created_at)) ||
    typeof note.helpful_vote_count !== 'number' || !Number.isSafeInteger(note.helpful_vote_count) || note.helpful_vote_count < 0 ||
    !isNoteMatchType(matchType) ||
    typeof note.match.location_label !== 'string' || note.match.location_label.length < 1 || note.match.location_label.length > 200 ||
    (distanceM !== undefined && (typeof distanceM !== 'number' || !Number.isSafeInteger(distanceM) || distanceM < 0 || distanceM > 5000))
  ) {
    throw invalidResponseError();
  }

  return {
    id: note.id,
    title: note.title,
    body: note.body,
    category: note.category,
    author: { display_name: note.author.display_name },
    created_at: note.created_at,
    helpful_vote_count: note.helpful_vote_count,
    match: {
      type: matchType,
      location_label: note.match.location_label,
      ...(typeof distanceM === 'number' ? { distance_m: distanceM } : {}),
    },
  };
}

function isNoteMatchType(value: unknown): value is NoteMatchType {
  return value === 'exact_location' || value === 'exact_coordinate' || value === 'broad_area' ||
    value === 'unverified_reference_label' || value === 'nearby';
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function invalidResponseError(): CreateNoteApiError {
  return new CreateNoteApiError('invalid-response', 'StreetMap returned a note response the extension could not validate.');
}
