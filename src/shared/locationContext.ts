export const GOOGLE_MAPS_HOSTS = [
  'www.google.com',
  'maps.google.com',
  'www.google.co.in',
] as const;

export const GOOGLE_MAPS_MATCH_PATTERNS = [
  'https://www.google.com/maps',
  'https://www.google.com/maps/*',
  'https://maps.google.com/maps',
  'https://maps.google.com/maps/*',
  'https://www.google.co.in/maps',
  'https://www.google.co.in/maps/*',
] as const;

export const LOCATION_CONTEXT_REQUEST = 'streetmap:get-location-context' as const;
export const LOCATION_CONTEXT_RESPONSE = 'streetmap:location-context' as const;

export interface LocationContextRequest {
  type: typeof LOCATION_CONTEXT_REQUEST;
}

export interface MapsPageContext {
  status: 'maps-no-location';
  sourceHostname: SupportedMapsHostname;
}

export interface PlaceIdLocation {
  kind: 'place-id';
  placeId: string;
}

export interface CoordinateLocation {
  kind: 'coordinates';
  latitude: number;
  longitude: number;
}

export interface AvailableLocationContext {
  status: 'location-available';
  sourceHostname: SupportedMapsHostname;
  location: PlaceIdLocation | CoordinateLocation;
}

export type ContentLocationContext = MapsPageContext | AvailableLocationContext;

export type LocationContext =
  | { status: 'loading' }
  | { status: 'maps-unavailable' }
  | ContentLocationContext
  | {
      status: 'error';
      reason: 'active-tab' | 'content-script' | 'invalid-response';
    };

export interface LocationContextResponse {
  type: typeof LOCATION_CONTEXT_RESPONSE;
  context: ContentLocationContext;
}

export type SupportedMapsHostname = (typeof GOOGLE_MAPS_HOSTS)[number];

const MAPS_SEARCH_PATHS = new Set(['/maps/search', '/maps/search/']);
const COORDINATE_QUERY_PATTERN = /^\s*([+-]?(?:\d+(?:\.\d*)?|\.\d+))\s*,\s*([+-]?(?:\d+(?:\.\d*)?|\.\d+))\s*$/;
const PLACE_ID_PATTERN = /^[a-zA-Z0-9_-]{1,256}$/;

export function isSupportedMapsHostname(hostname: string): hostname is SupportedMapsHostname {
  return GOOGLE_MAPS_HOSTS.some((supportedHost) => hostname === supportedHost);
}

export function isGoogleMapsPageUrl(rawUrl: string): boolean {
  try {
    const url = new URL(rawUrl);
    return (
      url.protocol === 'https:' &&
      isSupportedMapsHostname(url.hostname) &&
      (url.pathname === '/maps' || url.pathname.startsWith('/maps/'))
    );
  } catch {
    return false;
  }
}

/**
 * Parse only the documented Maps search URL format. General search text and
 * undocumented Maps UI routes do not prove that a particular place is selected.
 */
export function parseMapsPageUrl(rawUrl: string): ContentLocationContext | null {
  let url: URL;

  try {
    url = new URL(rawUrl);
  } catch {
    return null;
  }

  if (!isGoogleMapsPageUrl(url.href)) return null;

  const sourceHostname = url.hostname;
  if (!isSupportedMapsHostname(sourceHostname)) return null;
  const apiVersions = url.searchParams.getAll('api');
  const queries = url.searchParams.getAll('query');
  const placeIds = url.searchParams.getAll('query_place_id');

  if (
    !MAPS_SEARCH_PATHS.has(url.pathname) ||
    apiVersions.length !== 1 ||
    apiVersions[0] !== '1' ||
    queries.length !== 1 ||
    queries[0]?.trim() === ''
  ) {
    return { status: 'maps-no-location', sourceHostname };
  }

  if (placeIds.length > 0) {
    const placeId = placeIds[0];
    if (placeIds.length === 1 && placeId && PLACE_ID_PATTERN.test(placeId)) {
      return {
        status: 'location-available',
        sourceHostname,
        location: { kind: 'place-id', placeId },
      };
    }
    return { status: 'maps-no-location', sourceHostname };
  }

  const coordinateMatch = COORDINATE_QUERY_PATTERN.exec(queries[0] ?? '');
  if (!coordinateMatch) return { status: 'maps-no-location', sourceHostname };

  const latitude = Number(coordinateMatch[1]);
  const longitude = Number(coordinateMatch[2]);
  if (
    !Number.isFinite(latitude) ||
    !Number.isFinite(longitude) ||
    latitude < -90 ||
    latitude > 90 ||
    longitude < -180 ||
    longitude > 180
  ) {
    return { status: 'maps-no-location', sourceHostname };
  }

  return {
    status: 'location-available',
    sourceHostname,
    location: { kind: 'coordinates', latitude, longitude },
  };
}

export function isLocationContextRequest(value: unknown): value is LocationContextRequest {
  return isRecord(value) && value.type === LOCATION_CONTEXT_REQUEST;
}

/** Validate and normalize a response received from the page's content script. */
export function parseLocationContextResponse(value: unknown): ContentLocationContext | null {
  if (!isRecord(value) || value.type !== LOCATION_CONTEXT_RESPONSE || !isRecord(value.context)) {
    return null;
  }

  const context = value.context;
  if (
    typeof context.sourceHostname !== 'string' ||
    !isSupportedMapsHostname(context.sourceHostname)
  ) {
    return null;
  }

  if (context.status === 'maps-no-location') {
    return { status: 'maps-no-location', sourceHostname: context.sourceHostname };
  }

  if (context.status !== 'location-available' || !isRecord(context.location)) return null;

  if (
    context.location.kind === 'place-id' &&
    typeof context.location.placeId === 'string' &&
    PLACE_ID_PATTERN.test(context.location.placeId)
  ) {
    return {
      status: 'location-available',
      sourceHostname: context.sourceHostname,
      location: { kind: 'place-id', placeId: context.location.placeId },
    };
  }

  if (
    context.location.kind === 'coordinates' &&
    typeof context.location.latitude === 'number' &&
    Number.isFinite(context.location.latitude) &&
    context.location.latitude >= -90 &&
    context.location.latitude <= 90 &&
    typeof context.location.longitude === 'number' &&
    Number.isFinite(context.location.longitude) &&
    context.location.longitude >= -180 &&
    context.location.longitude <= 180
  ) {
    return {
      status: 'location-available',
      sourceHostname: context.sourceHostname,
      location: {
        kind: 'coordinates',
        latitude: context.location.latitude,
        longitude: context.location.longitude,
      },
    };
  }

  return null;
}

export interface LocationContextMessenger {
  getActiveTab(): Promise<{ id?: number; url?: string } | undefined>;
  requestContext(tabId: number): Promise<unknown>;
}

export async function resolveActiveLocationContext(
  messenger: LocationContextMessenger,
  retryDelayMs = 120,
): Promise<LocationContext> {
  let tab: { id?: number; url?: string } | undefined;

  try {
    tab = await messenger.getActiveTab();
  } catch {
    return { status: 'error', reason: 'active-tab' };
  }

  if (tab?.id === undefined || !tab.url || !isGoogleMapsPageUrl(tab.url)) {
    return { status: 'maps-unavailable' };
  }

  const expectedContext = parseMapsPageUrl(tab.url);
  if (!expectedContext) return { status: 'maps-unavailable' };

  let response: unknown;
  try {
    response = await messenger.requestContext(tab.id);
  } catch {
    await new Promise<void>((resolve) => setTimeout(resolve, retryDelayMs));
    try {
      response = await messenger.requestContext(tab.id);
    } catch {
      return { status: 'error', reason: 'content-script' };
    }
  }

  const context = parseLocationContextResponse(response);
  if (!context || context.sourceHostname !== expectedContext.sourceHostname) {
    return { status: 'error', reason: 'invalid-response' };
  }

  return context;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}
