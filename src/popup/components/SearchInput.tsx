interface SearchInputProps {
  value: string;
  onChange: (value: string) => void;
}

export default function SearchInput({ value, onChange }: SearchInputProps) {
  return (
    <label className="search-field">
      <svg aria-hidden="true" viewBox="0 0 20 20" fill="none">
        <circle cx="8.8" cy="8.8" r="5.8" stroke="currentColor" strokeWidth="1.6" />
        <path d="m13.2 13.2 4 4" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" />
      </svg>
      <span className="visually-hidden">Search community notes</span>
      <input
        type="search"
        value={value}
        onChange={(event) => onChange(event.currentTarget.value)}
        placeholder="Search notes, tips, places..."
        autoComplete="off"
      />
      <kbd aria-hidden="true">⌕</kbd>
    </label>
  );
}
