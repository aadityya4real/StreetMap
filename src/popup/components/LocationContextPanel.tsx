import type { LocationContext } from '../../shared/locationContext';

interface LocationContextPanelProps {
  context: LocationContext;
  onRefresh: () => void;
}

interface ContextCopy {
  title: string;
  description: string;
  detail?: string;
}

function getContextCopy(context: LocationContext): ContextCopy {
  switch (context.status) {
    case 'loading':
      return {
        title: 'Checking this tab',
        description: 'Looking for a supported Google Maps location reference.',
      };
    case 'maps-unavailable':
      return {
        title: 'No Google Maps tab detected',
        description: 'Open Google Maps in this tab to explore local tips.',
      };
    case 'maps-no-location':
      return {
        title: 'Google Maps detected',
        description: 'Select a place on Google Maps to see its available context.',
      };
    case 'error':
      return {
        title: 'Unable to read location context',
        description: "StreetMap couldn't identify this location. Try selecting the place again.",
      };
    case 'location-available':
      if (context.location.kind === 'place-id') {
        return {
          title: 'Place reference found',
          description:
            'This Maps link includes a place ID. StreetMap cannot verify the visible selection or resolve a place name yet.',
          detail: `Place ID · ${context.location.placeId}`,
        };
      }
      return {
        title: 'Search coordinates found',
        description:
          'These coordinates come from a documented Maps search URL; they do not confirm the visible place selection.',
        detail: `${context.location.latitude}, ${context.location.longitude}`,
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
        {context.status === 'location-available' && (
          <span className="context-source">URL signal · {context.sourceHostname}</span>
        )}
      </div>
    </section>
  );
}
