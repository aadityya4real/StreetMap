export const SEARCH_COLLAPSE_SCROLL_THRESHOLD_PX = 24;

export interface NotesScrollProgress {
  lastScrollTop: number;
  downwardDistance: number;
}

export interface NotesScrollUpdate {
  progress: NotesScrollProgress;
  shouldCollapse: boolean;
}

export function updateNotesScrollProgress(
  progress: NotesScrollProgress,
  scrollTop: number,
): NotesScrollUpdate {
  const scrollDelta = scrollTop - progress.lastScrollTop;
  if (scrollDelta <= 0) {
    return {
      progress: { lastScrollTop: scrollTop, downwardDistance: 0 },
      shouldCollapse: false,
    };
  }

  const downwardDistance = progress.downwardDistance + scrollDelta;
  return {
    progress: { lastScrollTop: scrollTop, downwardDistance },
    shouldCollapse: downwardDistance >= SEARCH_COLLAPSE_SCROLL_THRESHOLD_PX,
  };
}
