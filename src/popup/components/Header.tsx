function PinMark() {
  return (
    <svg aria-hidden="true" className="brand-mark" viewBox="0 0 32 32" fill="none">
      <path
        d="M16 28s10-8.2 10-16.2a10 10 0 1 0-20 0C6 19.8 16 28 16 28Z"
        fill="currentColor"
      />
      <circle cx="16" cy="11.5" r="3.4" fill="#202321" />
    </svg>
  );
}

interface HeaderProps {
  accountLabel: string;
  onAccountClick: () => void;
}

export default function Header({ accountLabel, onAccountClick }: HeaderProps) {
  return (
    <header className="app-header">
      <div className="brand">
        <PinMark />
        <span>StreetMap</span>
      </div>
      <div className="header-actions">
        <span className="header-tag">LOCAL KNOWLEDGE</span>
        <button className="account-button" type="button" onClick={onAccountClick}>{accountLabel}</button>
      </div>
    </header>
  );
}
