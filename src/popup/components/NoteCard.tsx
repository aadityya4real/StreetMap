import { useState } from 'react';
import type { DisplayNote } from '../types/note';

interface NoteCardProps {
  note: DisplayNote;
}

export default function NoteCard({ note }: NoteCardProps) {
  const [expanded, setExpanded] = useState(false);
  const detailsId = `note-details-${note.id}`;
  const matchLabel = note.group === 'nearby'
    ? 'Nearby note'
    : note.group === 'context_matches'
      ? note.match.type === 'broad_area' ? 'Broad-area match' : 'Unverified reference match'
      : 'Exact match';

  return (
    <article className={`note-card${expanded ? ' note-card-expanded' : ''}`}>
      <button
        className="note-summary"
        type="button"
        aria-expanded={expanded}
        aria-controls={detailsId}
        onClick={() => setExpanded((isExpanded) => !isExpanded)}
      >
        <span className="note-meta">
          <span className={`category category-${note.category.replaceAll('_', '-')}`}>
            {note.category.replaceAll('_', ' ').toUpperCase()}
          </span>
          <time className="note-time" dateTime={note.created_at}>{formatCreatedAt(note.created_at)}</time>
        </span>
        <span className="note-title" role="heading" aria-level={3}>{note.title}</span>
        <span className="note-summary-footer">
          <span className="match-label">{matchLabel}</span>
          <span className="helpful-count" aria-label={`${note.helpful_vote_count} helpful votes`}>
            <svg aria-hidden="true" viewBox="0 0 16 16" fill="none">
              <path
                d="M5.4 7.1 7.5 3c.3-.6.8-.8 1.3-.6.5.2.7.8.5 1.3l-.6 1.7h2.2c1 0 1.6.9 1.3 1.8l-1.2 4.2c-.2.6-.7 1-1.3 1H5.4V7.1Z"
                stroke="currentColor"
                strokeWidth="1.2"
                strokeLinejoin="round"
              />
              <path d="M3.1 7h2.1v5.4H3.1z" stroke="currentColor" strokeWidth="1.2" />
            </svg>
            {note.helpful_vote_count}
          </span>
        </span>
      </button>
      {expanded && (
        <div className="note-details" id={detailsId}>
          <p className="note-description">{note.body}</p>
          <dl className="note-context-details">
            <div><dt>Shared by</dt><dd>{note.author.display_name}</dd></div>
            <div><dt>Tagged location</dt><dd>{note.match.location_label}</dd></div>
            <div>
              <dt>Match</dt>
              <dd>
                {matchLabel}
                {note.group === 'nearby' && note.match.distance_m !== undefined
                  ? ` · ${formatDistance(note.match.distance_m)} from the map center`
                  : ''}
              </dd>
            </div>
          </dl>
        </div>
      )}
    </article>
  );
}

function formatCreatedAt(value: string): string {
  return new Intl.DateTimeFormat(undefined, { month: 'short', day: 'numeric', year: 'numeric' }).format(new Date(value));
}

function formatDistance(distanceM: number): string {
  return distanceM >= 1000 ? `${(distanceM / 1000).toFixed(1)} km` : `${distanceM} m`;
}
