import { getCurrentAccessToken } from '../auth/supabase';
import { getApiBaseUrl } from './publicNotes';

export interface NoteVoteState { voted: boolean; count: number }

async function requestVote(noteId: string, method: 'GET' | 'POST'): Promise<NoteVoteState> {
  const token = await getCurrentAccessToken();
  if (!token) throw new Error('Sign in to mark this note helpful.');
  const response = await fetch(new URL(`${getApiBaseUrl(import.meta.env.DEV).replace(/\/$/, '')}/notes/${encodeURIComponent(noteId)}/vote`), {
    method, headers: { Accept: 'application/json', Authorization: `Bearer ${token}` }, credentials: 'omit', cache: 'no-store',
  });
  if (!response.ok) throw new Error(response.status === 401 ? 'Your session expired. Sign in again to vote.' : 'StreetMap could not update this vote. Please retry.');
  const payload: unknown = await response.json();
  if (!payload || typeof payload !== 'object' || !('data' in payload)) throw new Error('StreetMap returned an invalid vote response.');
  const data = payload.data as { voted?: unknown; helpful_vote_count?: unknown };
  if (typeof data.voted !== 'boolean' || typeof data.helpful_vote_count !== 'number' || !Number.isSafeInteger(data.helpful_vote_count) || data.helpful_vote_count < 0) throw new Error('StreetMap returned an invalid vote response.');
  return { voted: data.voted, count: data.helpful_vote_count };
}

export const getNoteVoteState = (noteId: string) => requestVote(noteId, 'GET');
export const toggleNoteVote = (noteId: string) => requestVote(noteId, 'POST');
