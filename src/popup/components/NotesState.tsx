type NotesStateKind = 'loading' | 'error';

interface NotesStateProps {
  state: NotesStateKind;
  message?: string;
  onRetry?: () => void;
}

const stateCopy: Record<NotesStateKind, { title: string; description: string }> = {
  loading: {
    title: 'Finding local notes',
    description: 'Community knowledge will show here in a moment.',
  },
  error: {
    title: 'Notes couldn’t load',
    description: 'Please try again in a little while.',
  },
};

export default function NotesState({ state, message, onRetry }: NotesStateProps) {
  const copy = stateCopy[state];

  return (
    <div className={`notes-state notes-state-${state}`} role={state === 'error' ? 'alert' : 'status'}>
      {state === 'loading' && <span className="loading-indicator" aria-hidden="true" />}
      <h3>{copy.title}</h3>
      <p>{message ?? copy.description}</p>
      {state === 'error' && onRetry && (
        <button className="retry-button" type="button" onClick={onRetry}>Try again</button>
      )}
    </div>
  );
}
