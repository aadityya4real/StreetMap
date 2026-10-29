import { useCallback, useEffect, useRef, useState } from 'react';
import type { LocationContext } from '../shared/locationContext';
import Header from './components/Header';
import LocationContextPanel from './components/LocationContextPanel';
import NotesList from './components/NotesList';
import SearchInput from './components/SearchInput';
import { useNoteSearch } from './hooks/useNoteSearch';
import { getActiveLocationContext } from './services/locationContext';
import {
  fetchPublicNotes,
  getDisplayNotes,
  getNotesRequestContext,
  InvalidNotesResponseError,
  NotesConfigurationError,
  type NotesRequestContext,
} from './services/publicNotes';
import type { DisplayNote, PublicNotesPage } from './types/note';

type NotesState =
  | { status: 'loading' }
  | { status: 'error'; message: string }
  | {
      status: 'ready';
      notes: DisplayNote[];
      requestContext: NotesRequestContext | null;
      nextCursor: string | null;
      hasMore: boolean;
    };

export default function App() {
  const [query, setQuery] = useState('');
  const [shareMessage, setShareMessage] = useState('');
  const [locationContext, setLocationContext] = useState<LocationContext>({ status: 'loading' });
  const [notesState, setNotesState] = useState<NotesState>({ status: 'loading' });
  const [refreshCount, setRefreshCount] = useState(0);
  const [isLoadingMore, setIsLoadingMore] = useState(false);
  const [loadMoreError, setLoadMoreError] = useState('');
  const loadMoreController = useRef<AbortController | null>(null);
  const visibleNotes = useNoteSearch(notesState.status === 'ready' ? notesState.notes : [], query);

  const refreshLocationContext = useCallback(() => {
    setLocationContext({ status: 'loading' });
    setNotesState({ status: 'loading' });
    loadMoreController.current?.abort();
    setIsLoadingMore(false);
    setLoadMoreError('');
    setRefreshCount((count) => count + 1);
  }, []);

  useEffect(() => {
    const controller = new AbortController();
    let isCurrent = true;

    async function loadNotes() {
      setLocationContext({ status: 'loading' });
      setNotesState({ status: 'loading' });
      try {
        const context = await getActiveLocationContext();
        if (!isCurrent) return;
        setLocationContext(context);

        const requestContext = getNotesRequestContext(context);
        if (!requestContext) {
          setNotesState({
            status: 'ready',
            notes: [],
            requestContext: null,
            nextCursor: null,
            hasMore: false,
          });
          return;
        }

        const page = await fetchPublicNotes(requestContext, controller.signal);
        if (!isCurrent) return;
        setNotesState(toReadyState(page, requestContext));
      } catch (error) {
        if (!isCurrent || isAbortError(error)) return;
        setNotesState({ status: 'error', message: getNotesErrorMessage(error) });
      }
    }

    void loadNotes();
    return () => {
      isCurrent = false;
      controller.abort();
      loadMoreController.current?.abort();
      loadMoreController.current = null;
    };
  }, [refreshCount]);

  const retryNotes = useCallback(() => {
    setRefreshCount((count) => count + 1);
  }, []);

  const loadMore = useCallback(async () => {
    if (notesState.status !== 'ready' || !notesState.requestContext || !notesState.nextCursor || isLoadingMore) return;
    const controller = new AbortController();
    loadMoreController.current = controller;
    setIsLoadingMore(true);
    setLoadMoreError('');
    try {
      const page = await fetchPublicNotes(notesState.requestContext, controller.signal, notesState.nextCursor);
      setNotesState((current) => current.status === 'ready'
        ? {
            ...current,
            notes: [...current.notes, ...getDisplayNotes(page)],
            nextCursor: page.page.next_cursor,
            hasMore: page.page.has_more,
          }
        : current);
    } catch (error) {
      if (!isAbortError(error)) setLoadMoreError(getNotesErrorMessage(error));
    } finally {
      if (loadMoreController.current === controller) {
        loadMoreController.current = null;
        setIsLoadingMore(false);
      }
    }
  }, [isLoadingMore, notesState]);

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
        <SearchInput value={query} onChange={setQuery} />

        <section className="notes-section" aria-label="Community notes">
          <div className="list-heading">
            <h2>Local finds</h2>
            <span>{notesState.status === 'ready' ? `${visibleNotes.length} NOTES` : ' '}</span>
          </div>
          <NotesList
            notes={visibleNotes}
            query={query}
            status={notesState.status}
            message={notesState.status === 'error' ? notesState.message : noteEmptyMessage}
            onRetry={retryNotes}
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

function toReadyState(page: PublicNotesPage, requestContext: NotesRequestContext): NotesState {
  return {
    status: 'ready',
    notes: getDisplayNotes(page),
    requestContext,
    nextCursor: page.page.next_cursor,
    hasMore: page.page.has_more,
  };
}

function isAbortError(error: unknown): boolean {
  return error instanceof DOMException && error.name === 'AbortError';
}

function getNotesErrorMessage(error: unknown): string {
  if (error instanceof NotesConfigurationError) return error.message;
  if (error instanceof InvalidNotesResponseError) return error.message;
  return 'StreetMap could not load community notes. Check your connection or local API, then try again.';
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
