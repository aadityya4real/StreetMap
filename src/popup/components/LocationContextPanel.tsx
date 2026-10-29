import type { LocationContext } from '../../shared/locationContext';

interface LocationContextPanelProps {
  context: LocationContext;
  onRefresh: () => void;
}

interface ContextCopy {
  title: string;
  description: string;
  detail?: string;
  secondaryDetail?: string;
}

function getContextCopy(context: LocationContext): ContextCopy {
  switch (context.status) {
    case 'loading':
      return {
        title: 'Checking this tab',
        description: 'Checking this tab for Google Maps context.',
      };
    case 'maps-unavailable':
      return {
        title: 'Google Maps not detected',
        description: 'Open Google Maps in this tab to explore local tips.',
      };
    case 'maps-no-location':
      return {
        title: 'Google Maps detected, but no reliable context',
        description: 'Select a place or search an area to see its available URL context.',
      };
    case 'error':
      if (context.reason === 'active-tab') {
        return {
          title: 'Unable to read location context',
          description: "StreetMap couldn't read the active tab. Try reopening the popup.",
        };
      }
      if (context.reason === 'content-script') {
        return {
          title: 'Unable to read location context',
          description: "StreetMap couldn't start its Maps reader. Reload Google Maps and try again.",
        };
      }
      return {
        title: 'Unable to read location context',
        description: "StreetMap received context it couldn't validate. Refresh and try again.",
      };
    case 'broad-area':
      return {
        title: 'Broad area/search context available',
        description:
          context.source === 'place-path'
            ? 'This is URL text for an area or place, not a verified business selection.'
            : 'This is a search query, not a confirmed selected place.',
        detail: `${context.source === 'place-path' ? 'URL area' : 'Search query'} · ${context.label}`,
        ...(context.mapCenter
          ? { secondaryDetail: `Map center · ${context.mapCenter.latitude}, ${context.mapCenter.longitude}` }
          : {}),
      };
    case 'map-center':
      return {
        title: 'Map-center coordinates available',
        description: 'These coordinates mark the map view center, not a selected business.',
        detail: `${context.mapCenter.latitude}, ${context.mapCenter.longitude}`,
      };
    case 'unverified-place-reference':
      return {
        title: 'Specific place reference available (unverified)',
        description:
          'The URL contains a place ID. StreetMap has not verified the visible selection or resolved a name.',
        detail: `Place ID · ${context.placeId}`,
      };
  }
}

export default function LocationContextPanel({ context, onRefresh }: LocationContextPanelProps) {
  const copy = getContextCopy(context);

  return (
    <section className="location-context" aria-label="Google Maps location context">
      <div className="location-context-heading">
        <span className="context-icon" aria-hidden="true">
          <svg viewBox="0 0 20 20" fill="none">
            <path
              d="M10 17.2s5.6-4.6 5.6-9.1a5.6 5.6 0 1 0-11.2 0c0 4.5 5.6 9.1 5.6 9.1Z"
              stroke="currentColor"
              strokeWidth="1.5"
            />
            <circle cx="10" cy="8" r="1.8" stroke="currentColor" strokeWidth="1.4" />
          </svg>
        </span>
        <div className="context-heading-copy">
          <p className="eyebrow">CURRENT TAB</p>
          <h2>Maps context</h2>
        </div>
        <button
          className="context-refresh"
          type="button"
          onClick={onRefresh}
          disabled={context.status === 'loading'}
          aria-label="Refresh location context"
        >
          <svg aria-hidden="true" viewBox="0 0 18 18" fill="none">
            <path
              d="M14.8 7A6 6 0 0 0 4.1 4.4L2.7 5.8M2.7 5.8V2.9M2.7 5.8h2.9M3.2 11a6 6 0 0 0 10.7 2.6l1.4-1.4m0 0v2.9m0-2.9h-2.9"
              stroke="currentColor"
              strokeWidth="1.4"
              strokeLinecap="round"
              strokeLinejoin="round"
            />
          </svg>
          Refresh
        </button>
      </div>
      <div
        className={`context-result context-result-${context.status}`}
        role={context.status === 'error' ? 'alert' : 'status'}
        aria-live={context.status === 'error' ? 'assertive' : 'polite'}
      >
        <div className="context-result-title">
          {context.status === 'loading' ? (
            <span className="context-loading-dot" aria-hidden="true" />
          ) : (
            <span className="context-status-dot" aria-hidden="true" />
          )}
          <strong>{copy.title}</strong>
        </div>
        <p>{copy.description}</p>
        {copy.detail && <code className="context-detail">{copy.detail}</code>}
        {copy.secondaryDetail && <code className="context-detail">{copy.secondaryDetail}</code>}
        {(context.status === 'broad-area' ||
          context.status === 'map-center' ||
          context.status === 'unverified-place-reference') && (
          <span className="context-source">URL signal · {context.sourceHostname}</span>
        )}
      </div>
    </section>
  );
}
