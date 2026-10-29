import type { LocationContext } from '../../shared/locationContext';
import { NOTE_CATEGORIES, type NoteCategory, type NoteGroup, type PublicNote, type PublicNotesPage } from '../types/note';

const DEFAULT_DEVELOPMENT_API_BASE_URL = 'http://127.0.0.1:8080/api/v1';
const NOTES_PAGE_SIZE = 50;
const MAX_NOTE_DISTANCE_M = 5000;
const NOTE_ID_PATTERN = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
const RFC3339_UTC_PATTERN = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?Z$/;

export class NotesConfigurationError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'NotesConfigurationError';
  }
}

export class InvalidNotesResponseError extends Error {
  constructor() {
    super('The notes service returned data StreetMap could not validate.');
    this.name = 'InvalidNotesResponseError';
  }
}

export class NotesRequestError extends Error {
  constructor() {
    super('StreetMap could not load community notes. Check your connection and try again.');
    this.name = 'NotesRequestError';
  }
}

export type NotesRequestContext =
  | { type: 'broad_area'; label: string }
  | { type: 'place_page_unverified'; label: string }
  | { type: 'map_center'; latitude: number; longitude: number };

/** Use only context selectors the current Maps URL reader can establish safely. */
export function getNotesRequestContext(context: LocationContext): NotesRequestContext | null {
  switch (context.status) {
    case 'broad-area':
      return { type: 'broad_area', label: context.label };
    case 'place-page-unverified':
      return { type: 'place_page_unverified', label: context.label };
    case 'map-center':
      return {
        type: 'map_center',
        latitude: context.mapCenter.latitude,
        longitude: context.mapCenter.longitude,
      };
    default:
      // Maps place IDs are not StreetMap IDs, and map-center coordinates are
      // not user-confirmed coordinate selections.
      return null;
  }
}

export function getApiBaseUrl(isDevelopment: boolean): string {
  if (!isDevelopment) {
    throw new NotesConfigurationError('The StreetMap API is not configured for this build.');
  }
  return DEFAULT_DEVELOPMENT_API_BASE_URL;
}

export function buildNotesUrl(
  apiBaseUrl: string,
  context: NotesRequestContext,
  cursor?: string,
): URL {
  const url = new URL(`${apiBaseUrl.replace(/\/$/, '')}/notes`);
  url.searchParams.set('limit', String(NOTES_PAGE_SIZE));

  if (context.type === 'broad_area' || context.type === 'place_page_unverified') {
    url.searchParams.set('context_type', context.type);
    url.searchParams.set('label', context.label);
  } else {
    url.searchParams.set('context_type', context.type);
    url.searchParams.set('latitude', String(context.latitude));
    url.searchParams.set('longitude', String(context.longitude));
    url.searchParams.set('radius_m', String(MAX_NOTE_DISTANCE_M));
  }
  if (cursor) url.searchParams.set('cursor', cursor);
  return url;
}

export async function fetchPublicNotes(
  requestContext: NotesRequestContext,
  signal: AbortSignal,
  cursor?: string,
): Promise<PublicNotesPage> {
  const baseUrl = getApiBaseUrl(import.meta.env.DEV);
  return requestPublicNotes(baseUrl, requestContext, signal, cursor);
}

export async function requestPublicNotes(
  apiBaseUrl: string,
  requestContext: NotesRequestContext,
  signal: AbortSignal,
  cursor?: string,
  fetcher: typeof fetch = fetch,
): Promise<PublicNotesPage> {
  let response: Response;
  try {
    response = await fetcher(buildNotesUrl(apiBaseUrl, requestContext, cursor), {
      method: 'GET',
      headers: { Accept: 'application/json' },
      credentials: 'omit',
      cache: 'no-store',
      signal,
    });
  } catch (error) {
    if (error instanceof DOMException && error.name === 'AbortError') throw error;
    throw new NotesRequestError();
  }

  if (!response.ok) throw new NotesRequestError();
  if (!response.headers.get('content-type')?.toLowerCase().includes('application/json')) {
    throw new InvalidNotesResponseError();
  }

  let payload: unknown;
  try {
    payload = await response.json();
  } catch {
    throw new InvalidNotesResponseError();
  }
  return parsePublicNotesResponse(payload, requestContext);
}

export function parsePublicNotesResponse(
  value: unknown,
  expectedContext: NotesRequestContext,
): PublicNotesPage {
  if (!isRecord(value) || !isRecord(value.data)) throw new InvalidNotesResponseError();
  const data = value.data;
  if (
    !isRecord(data.context) ||
    !Array.isArray(data.exact) ||
    !Array.isArray(data.context_matches) ||
    !Array.isArray(data.nearby) ||
    !isRecord(data.page)
  ) {
    throw new InvalidNotesResponseError();
  }

  const parsedContext = parseResponseContext(data.context, expectedContext);
  const seenIds = new Set<string>();
  const exact = parseNoteGroup(data.exact, 'exact', expectedContext, seenIds);
  const contextMatches = parseNoteGroup(data.context_matches, 'context_matches', expectedContext, seenIds);
  const nearby = parseNoteGroup(data.nearby, 'nearby', expectedContext, seenIds);
  const { next_cursor: nextCursor, has_more: hasMore } = data.page;
  if (
    !(nextCursor === null || (typeof nextCursor === 'string' && nextCursor.length > 0 && nextCursor.length <= 1024)) ||
    typeof hasMore !== 'boolean' ||
    (hasMore && !nextCursor)
  ) {
    throw new InvalidNotesResponseError();
  }

  return {
    context: parsedContext,
    exact,
    context_matches: contextMatches,
    nearby,
    page: { next_cursor: nextCursor, has_more: hasMore },
  };
}

export function getDisplayNotes(page: PublicNotesPage) {
  return [
    ...page.exact.map((note) => ({ ...note, group: 'exact' as const })),
    ...page.context_matches.map((note) => ({ ...note, group: 'context_matches' as const })),
    ...page.nearby.map((note) => ({ ...note, group: 'nearby' as const })),
  ];
}

function parseResponseContext(
  value: Record<string, unknown>,
  expected: NotesRequestContext,
): PublicNotesPage['context'] {
  if (value.type !== expected.type) throw new InvalidNotesResponseError();
  if (expected.type === 'broad_area' || expected.type === 'place_page_unverified') {
    if (value.label !== expected.label) throw new InvalidNotesResponseError();
    return { type: expected.type, label: expected.label };
  }
  if (value.latitude !== expected.latitude || value.longitude !== expected.longitude) {
    throw new InvalidNotesResponseError();
  }
  if (value.label !== undefined && typeof value.label !== 'string') throw new InvalidNotesResponseError();
  return {
    type: 'map_center',
    latitude: expected.latitude,
    longitude: expected.longitude,
    ...(typeof value.label === 'string' ? { label: value.label } : {}),
  };
}

function parseNoteGroup(
  value: unknown[],
  group: NoteGroup,
  expectedContext: NotesRequestContext,
  seenIds: Set<string>,
): PublicNote[] {
  return value.map((entry) => {
    const note = parseNote(entry, group, expectedContext);
    if (seenIds.has(note.id)) throw new InvalidNotesResponseError();
    seenIds.add(note.id);
    return note;
  });
}

function parseNote(value: unknown, group: NoteGroup, expectedContext: NotesRequestContext): PublicNote {
  if (!isRecord(value) || !isRecord(value.author) || !isRecord(value.match)) {
    throw new InvalidNotesResponseError();
  }
  const { id, title, body, category, created_at: createdAt, helpful_vote_count: helpfulVoteCount } = value;
  const { type: matchType, location_label: locationLabel, distance_m: distanceM } = value.match;
  if (
    typeof id !== 'string' || !NOTE_ID_PATTERN.test(id) ||
    typeof title !== 'string' || title.length < 3 || title.length > 120 ||
    typeof body !== 'string' || body.length < 1 || body.length > 5000 ||
    typeof category !== 'string' || !NOTE_CATEGORIES.includes(category as NoteCategory) ||
    typeof value.author.display_name !== 'string' || value.author.display_name.length < 1 || value.author.display_name.length > 100 ||
    typeof createdAt !== 'string' || !RFC3339_UTC_PATTERN.test(createdAt) || !Number.isFinite(Date.parse(createdAt)) ||
    typeof helpfulVoteCount !== 'number' || !Number.isSafeInteger(helpfulVoteCount) || helpfulVoteCount < 0 ||
    typeof matchType !== 'string' || typeof locationLabel !== 'string' || locationLabel.length < 1 || locationLabel.length > 200
  ) {
    throw new InvalidNotesResponseError();
  }

  // The extension currently issues only broad, unverified-label, and map-center
  // queries. None of those selectors can produce exact results by contract.
  if (group === 'exact') {
    throw new InvalidNotesResponseError();
  }
  if (
    group === 'context_matches' &&
    (expectedContext.type === 'broad_area' ? matchType !== 'broad_area' :
      expectedContext.type === 'place_page_unverified' ? matchType !== 'unverified_reference_label' : true)
  ) {
    throw new InvalidNotesResponseError();
  }
  if (group === 'nearby' && (expectedContext.type !== 'map_center' || matchType !== 'nearby')) {
    throw new InvalidNotesResponseError();
  }
  if (group === 'nearby') {
    if (typeof distanceM !== 'number' || !Number.isSafeInteger(distanceM) || distanceM < 0 || distanceM > MAX_NOTE_DISTANCE_M) {
      throw new InvalidNotesResponseError();
    }
  } else if (distanceM !== undefined) {
    throw new InvalidNotesResponseError();
  }

  return {
    id,
    title,
    body,
    category: category as NoteCategory,
    author: { display_name: value.author.display_name },
    created_at: createdAt,
    helpful_vote_count: helpfulVoteCount,
    match: {
      type: matchType as PublicNote['match']['type'],
      location_label: locationLabel,
      ...(typeof distanceM === 'number' ? { distance_m: distanceM } : {}),
    },
  };
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}
