import { useMemo } from 'react';
import type { DisplayNote } from '../types/note';

export function useNoteSearch(notes: DisplayNote[], query: string): DisplayNote[] {
  return useMemo(() => {
    const normalizedQuery = query.trim().toLocaleLowerCase();

    if (!normalizedQuery) return notes;

    return notes.filter((note) =>
      [note.category, note.title, note.body, note.author.display_name, note.match.location_label].some((value) =>
        value.toLocaleLowerCase().includes(normalizedQuery),
      ),
    );
  }, [notes, query]);
}
