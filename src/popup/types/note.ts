export const NOTE_CATEGORIES = [
  'hidden_gem',
  'shortcut',
  'local_tip',
  'parking',
  'warning',
  'road_condition',
  'safety',
  'other',
] as const;

export type NoteCategory = (typeof NOTE_CATEGORIES)[number];

export const NOTE_CATEGORY_LABELS: Record<NoteCategory, string> = {
  hidden_gem: 'Hidden Gem',
  shortcut: 'Shortcut',
  local_tip: 'Local Tip',
  parking: 'Parking',
  warning: 'Warning',
  road_condition: 'Road Condition',
  safety: 'Safety',
  other: 'Other',
};

export function isNoteCategory(value: unknown): value is NoteCategory {
  return typeof value === 'string' && NOTE_CATEGORIES.includes(value as NoteCategory);
}

export type NoteMatchType =
  | 'exact_location'
  | 'exact_coordinate'
  | 'broad_area'
  | 'unverified_reference_label'
  | 'nearby';

export interface PublicNote {
  id: string;
  title: string;
  body: string;
  category: NoteCategory;
  author: { display_name: string };
  created_at: string;
  helpful_vote_count: number;
  match: {
    type: NoteMatchType;
    location_label: string;
    distance_m?: number;
  };
}

export type NoteGroup = 'exact' | 'context_matches' | 'nearby';

export interface DisplayNote extends PublicNote {
  group: NoteGroup;
}

export interface PublicNotesPage {
  context: {
    type: 'verified_place' | 'broad_area' | 'place_page_unverified' | 'map_center' | 'coordinate';
    location_id?: string;
    label?: string;
    latitude?: number;
    longitude?: number;
  };
  exact: PublicNote[];
  context_matches: PublicNote[];
  nearby: PublicNote[];
  page: { next_cursor: string | null; has_more: boolean };
}
