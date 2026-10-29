import { useMemo } from 'react';
import type { Note } from '../types/note';

export function useNoteSearch(notes: Note[], query: string): Note[] {
  return useMemo(() => {
    const normalizedQuery = query.trim().toLocaleLowerCase();

    if (!normalizedQuery) return notes;

    return notes.filter((note) =>
      [note.category, note.title, note.description].some((value) =>
        value.toLocaleLowerCase().includes(normalizedQuery),
      ),
    );
  }, [notes, query]);
}
