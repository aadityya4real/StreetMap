import { useCallback, useEffect, useRef, useState } from 'react';
import type { LocationContext } from '../../shared/locationContext';
import {
  getDisplayNotes,
  getNotesRequestContext,
  InvalidNotesResponseError,
  NotesConfigurationError,
  type NotesRequestContext,
} from '../services/publicNotes';
import type { DisplayNote, NoteGroup, PublicNotesPage } from '../types/note';

export type NotesState =
  | { status: 'loading' }
  | { status: 'error'; message: string }
  | {
      status: 'ready';
      notes: DisplayNote[];
      requestContext: NotesRequestContext | null;
      requestKey: string | null;
      nextCursor: string | null;
      hasMore: boolean;
    };

type ReadLocationContext = () => Promise<LocationContext>;
type LoadNotesPage = (context: NotesRequestContext, signal: AbortSignal, cursor?: string) => Promise<PublicNotesPage>;
type SubscribeToLocationChanges = (onChange: () => void) => () => void;

interface NotesFeedServices {
  readLocationContext: ReadLocationContext;
  loadNotesPage: LoadNotesPage;
  subscribeToLocationChanges: SubscribeToLocationChanges;
}

interface LoadMoreRequest {
  controller: AbortController;
  generation: number;
  cursor: string;
}

const GROUP_PRIORITY: Record<NoteGroup, number> = {
  exact: 0,
  context_matches: 1,
  nearby: 2,
};

export function mergeDisplayNotes(existing: DisplayNote[], incoming: DisplayNote[]): DisplayNote[] {
  const merged = new Map(existing.map((note) => [note.id, note]));

  for (const note of incoming) {
    const current = merged.get(note.id);
    if (!current || GROUP_PRIORITY[note.group] < GROUP_PRIORITY[current.group]) {
      merged.set(note.id, note);
    }
  }

  return [...merged.values()].sort((left, right) => GROUP_PRIORITY[left.group] - GROUP_PRIORITY[right.group]);
}

export default function useNotesFeed({
  readLocationContext,
  loadNotesPage,
  subscribeToLocationChanges,
}: NotesFeedServices) {
  const [locationContext, setLocationContext] = useState<LocationContext>({ status: 'loading' });
  const [notesState, setNotesState] = useState<NotesState>({ status: 'loading' });
  const [reloadVersion, setReloadVersion] = useState(0);
  const [isLoadingMore, setIsLoadingMore] = useState(false);
  const [loadMoreError, setLoadMoreError] = useState('');
  const generationRef = useRef(0);
  const initialControllerRef = useRef<AbortController | null>(null);
  const loadMoreRequestRef = useRef<LoadMoreRequest | null>(null);

  const reload = useCallback(() => {
    generationRef.current += 1;
    initialControllerRef.current?.abort();
    loadMoreRequestRef.current?.controller.abort();
    loadMoreRequestRef.current = null;
    setLocationContext({ status: 'loading' });
    setNotesState({ status: 'loading' });
    setIsLoadingMore(false);
    setLoadMoreError('');
    setReloadVersion((version) => version + 1);
  }, []);

  useEffect(() => subscribeToLocationChanges(reload), [reload, subscribeToLocationChanges]);

  useEffect(() => {
    const generation = generationRef.current;
    const controller = new AbortController();
    initialControllerRef.current = controller;
    let active = true;
    const isCurrent = () => active && generation === generationRef.current && !controller.signal.aborted;

    setLocationContext({ status: 'loading' });
    setNotesState({ status: 'loading' });

    async function loadInitialPage() {
      try {
        const context = await readLocationContext();
        if (!isCurrent()) return;
        setLocationContext(context);

        const requestContext = getNotesRequestContext(context);
        if (!requestContext) {
          setNotesState({
            status: 'ready',
            notes: [],
            requestContext: null,
            requestKey: null,
            nextCursor: null,
            hasMore: false,
          });
          return;
        }

        const page = await loadNotesPage(requestContext, controller.signal);
        if (!isCurrent()) return;
        setNotesState({
          status: 'ready',
          notes: mergeDisplayNotes([], getDisplayNotes(page)),
          requestContext,
          requestKey: JSON.stringify(requestContext),
          nextCursor: page.page.next_cursor,
          hasMore: page.page.has_more,
        });
      } catch (error) {
        if (!isCurrent() || isAbortError(error)) return;
        setNotesState({ status: 'error', message: getNotesErrorMessage(error) });
      }
    }

    void loadInitialPage();
    return () => {
      active = false;
      controller.abort();
      if (initialControllerRef.current === controller) initialControllerRef.current = null;
      if (loadMoreRequestRef.current?.generation === generation) {
        loadMoreRequestRef.current.controller.abort();
        loadMoreRequestRef.current = null;
      }
      if (generation === generationRef.current) generationRef.current += 1;
    };
  }, [loadNotesPage, readLocationContext, reloadVersion]);

  const loadMore = useCallback(async () => {
    if (
      notesState.status !== 'ready' ||
      !notesState.requestContext ||
      !notesState.requestKey ||
      !notesState.nextCursor ||
      isLoadingMore ||
      loadMoreRequestRef.current
    ) return;

    const cursor = notesState.nextCursor;
    const generation = generationRef.current;
    const requestKey = notesState.requestKey;
    const controller = new AbortController();
    loadMoreRequestRef.current = { controller, generation, cursor };
    setIsLoadingMore(true);
    setLoadMoreError('');

    try {
      const page = await loadNotesPage(notesState.requestContext, controller.signal, cursor);
      if (controller.signal.aborted || generation !== generationRef.current) return;
      setNotesState((current) => {
        if (generation !== generationRef.current || current.status !== 'ready' || current.requestKey !== requestKey) return current;
        return {
          ...current,
          notes: mergeDisplayNotes(current.notes, getDisplayNotes(page)),
          nextCursor: page.page.next_cursor,
          hasMore: page.page.has_more,
        };
      });
    } catch (error) {
      if (controller.signal.aborted || generation !== generationRef.current || isAbortError(error)) return;
      setLoadMoreError(getNotesErrorMessage(error));
    } finally {
      if (loadMoreRequestRef.current?.controller === controller) {
        loadMoreRequestRef.current = null;
        setIsLoadingMore(false);
      }
    }
  }, [isLoadingMore, loadNotesPage, notesState]);

  return {
    locationContext,
    notesState,
    isLoadingMore,
    loadMoreError,
    refreshLocationContext: reload,
    retryNotes: reload,
    loadMore,
  };
}

function isAbortError(error: unknown): boolean {
  return error instanceof DOMException && error.name === 'AbortError';
}

function getNotesErrorMessage(error: unknown): string {
  if (error instanceof NotesConfigurationError || error instanceof InvalidNotesResponseError) return error.message;
  return 'StreetMap could not load community notes. Check your connection or local API, then try again.';
}
