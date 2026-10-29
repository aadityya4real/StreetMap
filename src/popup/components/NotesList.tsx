import type { Note } from '../types/note';
import EmptyState from './EmptyState';
import NoteCard from './NoteCard';
import NotesState from './NotesState';

type NotesStatus = 'loading' | 'error' | 'ready';

interface NotesListProps {
  notes: Note[];
  query: string;
  status?: NotesStatus;
}

export default function NotesList({ notes, query, status = 'ready' }: NotesListProps) {
  if (status !== 'ready') return <NotesState state={status} />;
  if (notes.length === 0) return <EmptyState query={query} />;

  return (
    <div className="notes-list" aria-label="Community notes">
      {notes.map((note) => (
        <NoteCard key={note.id} note={note} />
      ))}
    </div>
  );
}
