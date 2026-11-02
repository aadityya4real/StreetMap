import { useCallback, useEffect, useRef, useState } from 'react';
import type { LocationContext } from '../shared/locationContext';
import Header from './components/Header';
import LocationContextPanel from './components/LocationContextPanel';
import NotesList from './components/NotesList';
import SearchInput from './components/SearchInput';
import { updateNotesScrollProgress } from './hooks/notesScrollCollapse';
import useNotesFeed from './hooks/useNotesFeed';
import { useNoteSearch } from './hooks/useNoteSearch';
import { getActiveLocationContext, subscribeToActiveTabLocationChanges } from './services/locationContext';
import { fetchPublicNotes } from './services/publicNotes';

export default function App() {
  const [query, setQuery] = useState('');
  const [isSearchExpanded, setIsSearchExpanded] = useState(false);
  const [shareMessage, setShareMessage] = useState('');
  const {
    locationContext,
    notesState,
    isLoadingMore,
    loadMoreError,
    refreshLocationContext,
    retryNotes,
    loadMore,
  } = useNotesFeed({
    readLocationContext: getActiveLocationContext,
    loadNotesPage: fetchPublicNotes,
    subscribeToLocationChanges: subscribeToActiveTabLocationChanges,
  });
  const notesListRef = useRef<HTMLDivElement>(null);
  const lastNotesScrollTop = useRef(0);
  const downwardScrollDistance = useRef(0);
  const visibleNotes = useNoteSearch(notesState.status === 'ready' ? notesState.notes : [], query);

  useEffect(() => {
    lastNotesScrollTop.current = notesListRef.current?.scrollTop ?? 0;
    downwardScrollDistance.current = 0;
  }, [isSearchExpanded]);

  const handleNotesListScroll = useCallback((scrollTop: number) => {
    if (!isSearchExpanded) {
      lastNotesScrollTop.current = scrollTop;
      downwardScrollDistance.current = 0;
      return;
    }

    const update = updateNotesScrollProgress({
      lastScrollTop: lastNotesScrollTop.current,
      downwardDistance: downwardScrollDistance.current,
    }, scrollTop);
    lastNotesScrollTop.current = update.progress.lastScrollTop;
    downwardScrollDistance.current = update.progress.downwardDistance;
    if (update.shouldCollapse) {
      downwardScrollDistance.current = 0;
      setIsSearchExpanded(false);
    }
  }, [isSearchExpanded]);

  const noteEmptyMessage = notesState.status === 'ready' && !notesState.requestContext
    ? getNoContextMessage(locationContext)
    : undefined;

  return (
    <div className="popup-shell">
      <Header />
      <LocationContextPanel context={locationContext} onRefresh={refreshLocationContext} />
      <main className="popup-main" id="community-notes">
        <div className="section-heading">
          <div>
            <p className="eyebrow">KNOW THE NEIGHBORHOOD</p>
            <h1>Community Notes</h1>
          </div>
          <span className="api-badge"><span aria-hidden="true" /> PUBLIC NOTES</span>
        </div>
        <p className="notes-context-caption">Results follow the Maps context shown above.</p>
        <div className="search-collapse" hidden={!isSearchExpanded}>
          <SearchInput id="note-search" value={query} onChange={setQuery} />
        </div>

        <section className="notes-section" aria-label="Community notes">
          <div className="list-heading">
            <h2>Local finds</h2>
            <div className="list-heading-actions">
              <button
                className="search-toggle"
                type="button"
                aria-label={isSearchExpanded ? 'Hide note search' : 'Show note search'}
                aria-expanded={isSearchExpanded}
                aria-controls="note-search"
                onClick={() => setIsSearchExpanded((expanded) => !expanded)}
              >
                <svg aria-hidden="true" viewBox="0 0 20 20" fill="none">
                  <circle cx="8.8" cy="8.8" r="5.8" stroke="currentColor" strokeWidth="1.6" />
                  <path d="m13.2 13.2 4 4" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" />
                </svg>
              </button>
              <span>{notesState.status === 'ready' ? `${visibleNotes.length} NOTES` : ' '}</span>
            </div>
          </div>
          <NotesList
            notes={visibleNotes}
            query={query}
            status={notesState.status}
            message={notesState.status === 'error' ? notesState.message : noteEmptyMessage}
            onRetry={retryNotes}
            listRef={notesListRef}
            onListScroll={handleNotesListScroll}
          />
          {notesState.status === 'ready' && notesState.hasMore && (
            <div className="load-more-wrap">
              {loadMoreError && <p className="load-more-error" role="alert">{loadMoreError}</p>}
              <button className="load-more-button" type="button" onClick={() => void loadMore()} disabled={isLoadingMore}>
                {isLoadingMore ? 'Loading more…' : 'Load more notes'}
              </button>
            </div>
          )}
        </section>
      </main>
      <footer className="popup-footer">
        <button
          className="share-button"
          type="button"
          onClick={() => setShareMessage('Note sharing will be available in a future update.')}
        >
          <svg aria-hidden="true" viewBox="0 0 20 20" fill="none">
            <path d="M10 4v12M4 10h12" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" />
          </svg>
          Share a local secret
        </button>
        <p className="share-message" role="status" aria-live="polite">{shareMessage}</p>
        <div className="footer-caption">
          <span>GOOD TO KNOW, WHEREVER YOU GO.</span>
          <span className="footer-pin" aria-hidden="true">●</span>
        </div>
      </footer>
    </div>
  );
}

function getNoContextMessage(context: LocationContext): string {
  switch (context.status) {
    case 'maps-unavailable':
      return 'Open a supported Google Maps page to find notes for its available URL context.';
    case 'maps-no-location':
      return 'This Maps page has no reliable place, area, or map-center context yet.';
    case 'unverified-place-reference':
      return 'StreetMap cannot look up a Maps place reference as a verified StreetMap location.';
    case 'error':
      return 'Refresh the Maps context above to try reading this tab again.';
    default:
      return 'StreetMap needs a readable Maps location context before it can find notes.';
  }
}
