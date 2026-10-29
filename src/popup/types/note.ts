export const NOTE_CATEGORIES = [
  'HIDDEN GEM',
  'SHORTCUT',
  'LOCAL TIP',
  'PARKING',
] as const;

export type NoteCategory = (typeof NOTE_CATEGORIES)[number];

export interface Note {
  id: string;
  category: NoteCategory;
  title: string;
  description: string;
  postedAt: string;
  helpfulVotes: number;
}
