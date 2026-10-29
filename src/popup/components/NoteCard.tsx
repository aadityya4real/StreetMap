import type { Note } from '../types/note';

interface NoteCardProps {
  note: Note;
}

export default function NoteCard({ note }: NoteCardProps) {
  return (
    <article className="note-card">
      <div className="note-meta">
        <span className={`category category-${note.category.toLowerCase().replaceAll(' ', '-')}`}>
          {note.category}
        </span>
        <time className="note-time">{note.postedAt}</time>
      </div>
      <h3>{note.title}</h3>
      <p className="note-description">{note.description}</p>
      <div className="note-footer">
        <span className="sample-byline">COMMUNITY SAMPLE</span>
        <span className="helpful-count" aria-label={`${note.helpfulVotes} helpful votes`}>
          <svg aria-hidden="true" viewBox="0 0 16 16" fill="none">
            <path
              d="M5.4 7.1 7.5 3c.3-.6.8-.8 1.3-.6.5.2.7.8.5 1.3l-.6 1.7h2.2c1 0 1.6.9 1.3 1.8l-1.2 4.2c-.2.6-.7 1-1.3 1H5.4V7.1Z"
              stroke="currentColor"
              strokeWidth="1.2"
              strokeLinejoin="round"
            />
            <path d="M3.1 7h2.1v5.4H3.1z" stroke="currentColor" strokeWidth="1.2" />
          </svg>
          {note.helpfulVotes}
        </span>
      </div>
    </article>
  );
}
