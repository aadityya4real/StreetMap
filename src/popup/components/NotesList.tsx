import type { DisplayNote } from '../types/note';
import EmptyState from './EmptyState';
import NoteCard from './NoteCard';
import NotesState from './NotesState';

type NotesStatus = 'loading' | 'error' | 'ready';

interface NotesListProps {
  notes: DisplayNote[];
  query: string;
  status?: NotesStatus;
  message?: string;
  onRetry?: () => void;
}

export default function NotesList({ notes, query, status = 'ready', message, onRetry }: NotesListProps) {
  if (status !== 'ready') return <NotesState state={status} message={message} onRetry={onRetry} />;
  if (notes.length === 0) return <EmptyState query={query} message={message} />;

  return (
    <div className="notes-list" aria-label="Community notes">
      {notes.map((note) => (
        <NoteCard key={note.id} note={note} />
      ))}
    </div>
  );
}
