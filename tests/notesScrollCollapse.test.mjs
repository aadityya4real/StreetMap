import assert from 'node:assert/strict';
import test from 'node:test';

import {
  SEARCH_COLLAPSE_SCROLL_THRESHOLD_PX,
  updateNotesScrollProgress,
} from '../src/popup/hooks/notesScrollCollapse.ts';

test('downward scrolling collapses only after the 24px threshold accumulates', () => {
  let progress = { lastScrollTop: 0, downwardDistance: 0 };
  for (const scrollTop of [5, 12, 23]) {
    const update = updateNotesScrollProgress(progress, scrollTop);
    assert.equal(update.shouldCollapse, false);
    progress = update.progress;
  }

  const thresholdUpdate = updateNotesScrollProgress(progress, SEARCH_COLLAPSE_SCROLL_THRESHOLD_PX);
  assert.equal(thresholdUpdate.shouldCollapse, true);
  assert.equal(thresholdUpdate.progress.lastScrollTop, SEARCH_COLLAPSE_SCROLL_THRESHOLD_PX);
});

test('upward movement resets accumulated distance and stationary positions do not collapse', () => {
  const partial = updateNotesScrollProgress({ lastScrollTop: 10, downwardDistance: 15 }, 18);
  assert.equal(partial.shouldCollapse, false);

  const upward = updateNotesScrollProgress(partial.progress, 14);
  assert.deepEqual(upward, {
    progress: { lastScrollTop: 14, downwardDistance: 0 },
    shouldCollapse: false,
  });

  const stationary = updateNotesScrollProgress(upward.progress, 14);
  assert.equal(stationary.shouldCollapse, false);
});
