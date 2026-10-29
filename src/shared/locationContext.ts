export const GOOGLE_MAPS_HOSTS = [
  'google.com',
  'www.google.com',
  'maps.google.com',
  'www.google.co.in',
] as const;

export const GOOGLE_MAPS_MATCH_PATTERNS = [
  'https://google.com/maps',
  'https://google.com/maps/*',
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

export interface MapCoordinates {
  latitude: number;
  longitude: number;
}

export interface MapsPageContext {
  status: 'maps-no-location';
  sourceHostname: SupportedMapsHostname;
}

export interface BroadAreaContext {
  status: 'broad-area';
  sourceHostname: SupportedMapsHostname;
  source: 'search-query';
  label: string;
  mapCenter?: MapCoordinates;
}

export interface PlacePageContext {
  status: 'place-page-unverified';
  sourceHostname: SupportedMapsHostname;
  label: string;
  mapCenter?: MapCoordinates;
}

export interface MapCenterContext {
  status: 'map-center';
  sourceHostname: SupportedMapsHostname;
  mapCenter: MapCoordinates;
}

export interface UnverifiedPlaceReferenceContext {
  status: 'unverified-place-reference';
  sourceHostname: SupportedMapsHostname;
  placeId: string;
}

export type ContentLocationContext =
  | MapsPageContext
  | BroadAreaContext
  | PlacePageContext
  | MapCenterContext
  | UnverifiedPlaceReferenceContext;

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
const PLACE_PATH_PATTERN = /^\/maps\/place\/([^/]+)(?:\/|$)/;
const MAP_CENTER_PATH_PATTERN = /\/@([+-]?(?:\d+(?:\.\d*)?|\.\d+)),([+-]?(?:\d+(?:\.\d*)?|\.\d+))(?:,(\d+(?:\.\d+)?)z)?(?:\/|$)/;
const PLACE_ID_PATTERN = /^[a-zA-Z0-9_-]{1,256}$/;
const MAX_CONTEXT_LABEL_LENGTH = 120;

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
 * Read only public URL text. Place-route labels are unverified URL-derived
 * references; `@lat,lng` is map-center metadata, never a business location.
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

  const mapCenter = parseMapCenter(url.pathname);

  if (MAPS_SEARCH_PATHS.has(url.pathname)) {
    const queries = url.searchParams.getAll('query');
    const placeIds = url.searchParams.getAll('query_place_id');
    const apiVersions = url.searchParams.getAll('api');
    const query = queries.length === 1 ? normalizeLabel(queries[0] ?? '') : null;

    if (
      apiVersions.length === 1 &&
      apiVersions[0] === '1' &&
      query &&
      placeIds.length === 1 &&
      placeIds[0] &&
      PLACE_ID_PATTERN.test(placeIds[0])
    ) {
      return {
        status: 'unverified-place-reference',
        sourceHostname,
        placeId: placeIds[0],
      };
    }

    if (query) {
      return {
        status: 'broad-area',
        sourceHostname,
        source: 'search-query',
        label: query,
        ...(mapCenter ? { mapCenter } : {}),
      };
    }
  }

  const placeLabel = parsePlacePathLabel(url.pathname);
  if (placeLabel) {
    return {
      status: 'place-page-unverified',
      sourceHostname,
      label: placeLabel,
      ...(mapCenter ? { mapCenter } : {}),
    };
  }

  if (mapCenter) {
    return { status: 'map-center', sourceHostname, mapCenter };
  }

  return { status: 'maps-no-location', sourceHostname };
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

  if (context.status === 'broad-area') {
    const label = typeof context.label === 'string' ? normalizeLabel(context.label) : null;
    if (!label || context.source !== 'search-query') {
      return null;
    }
    const mapCenter = context.mapCenter === undefined ? undefined : parseMapCoordinates(context.mapCenter);
    if (context.mapCenter !== undefined && !mapCenter) return null;
    return {
      status: 'broad-area',
      sourceHostname: context.sourceHostname,
      source: context.source,
      label,
      ...(mapCenter ? { mapCenter } : {}),
    };
  }

  if (context.status === 'place-page-unverified') {
    const label = typeof context.label === 'string' ? normalizeLabel(context.label) : null;
    if (!label) return null;
    const mapCenter = context.mapCenter === undefined ? undefined : parseMapCoordinates(context.mapCenter);
    if (context.mapCenter !== undefined && !mapCenter) return null;
    return {
      status: 'place-page-unverified',
      sourceHostname: context.sourceHostname,
      label,
      ...(mapCenter ? { mapCenter } : {}),
    };
  }

  if (context.status === 'map-center') {
    const mapCenter = parseMapCoordinates(context.mapCenter);
    return mapCenter
      ? { status: 'map-center', sourceHostname: context.sourceHostname, mapCenter }
      : null;
  }

  if (
    context.status === 'unverified-place-reference' &&
    typeof context.placeId === 'string' &&
    PLACE_ID_PATTERN.test(context.placeId)
  ) {
    return {
      status: 'unverified-place-reference',
      sourceHostname: context.sourceHostname,
      placeId: context.placeId,
    };
  }

  return null;
}

export interface LocationContextMessenger {
  getActiveTab(): Promise<{ id?: number; url?: string } | undefined>;
  requestContext(tabId: number): Promise<unknown>;
  injectContentScript(tabId: number): Promise<void>;
}

export async function resolveActiveLocationContext(
  messenger: LocationContextMessenger,
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
  } catch (error) {
    if (!isMissingContentScriptError(error)) {
      return { status: 'error', reason: 'content-script' };
    }
    // Static content scripts are not retroactively added to an already-open tab.
    // Recover only on the verified active Maps tab, then retry the same request once.
    try {
      await messenger.injectContentScript(tab.id);
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

function parsePlacePathLabel(pathname: string): string | null {
  const match = PLACE_PATH_PATTERN.exec(pathname);
  if (!match?.[1]) return null;

  try {
    return normalizeLabel(decodeURIComponent(match[1].replaceAll('+', ' ')));
  } catch {
    return null;
  }
}

function parseMapCenter(pathname: string): MapCoordinates | null {
  const match = MAP_CENTER_PATH_PATTERN.exec(pathname);
  if (!match) return null;

  return parseMapCoordinates({ latitude: Number(match[1]), longitude: Number(match[2]) });
}

function parseMapCoordinates(value: unknown): MapCoordinates | null {
  if (!isRecord(value)) return null;
  const { latitude, longitude } = value;
  if (
    typeof latitude !== 'number' ||
    !Number.isFinite(latitude) ||
    latitude < -90 ||
    latitude > 90 ||
    typeof longitude !== 'number' ||
    !Number.isFinite(longitude) ||
    longitude < -180 ||
    longitude > 180
  ) {
    return null;
  }
  return { latitude, longitude };
}

function normalizeLabel(value: string): string | null {
  const withoutControls = Array.from(value)
    .filter((character) => {
      const codePoint = character.codePointAt(0) ?? 0;
      return codePoint > 0x1f && codePoint !== 0x7f;
    })
    .join('');
  const label = withoutControls.replace(/\s+/g, ' ').trim();
  return label.length > 0 && label.length <= MAX_CONTEXT_LABEL_LENGTH ? label : null;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function isMissingContentScriptError(error: unknown): boolean {
  if (!(error instanceof Error)) return false;
  return (
    error.message.includes('Receiving end does not exist') ||
    error.message.includes('Could not establish connection')
  );
}
