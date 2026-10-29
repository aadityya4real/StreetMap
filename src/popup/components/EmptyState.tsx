interface EmptyStateProps {
  query: string;
}

export default function EmptyState({ query }: EmptyStateProps) {
  return (
    <div className="empty-state" role="status">
      <span className="empty-icon" aria-hidden="true">
        <svg viewBox="0 0 32 32" fill="none">
          <circle cx="14" cy="14" r="8.5" stroke="currentColor" strokeWidth="1.6" />
          <path d="m20.5 20.5 6 6" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" />
          <path d="M10.8 14h6.4" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" />
        </svg>
      </span>
      <h3>No notes found</h3>
      <p>Nothing matches “{query}”. Try a different search.</p>
    </div>
  );
}
