import { useCallback, useEffect, useState } from 'react';
import Header from './components/Header';
import LocationContextPanel from './components/LocationContextPanel';
import NotesList from './components/NotesList';
import SearchInput from './components/SearchInput';
import { demoNotes } from './data/demoNotes';
import { useNoteSearch } from './hooks/useNoteSearch';
import { getActiveLocationContext } from './services/locationContext';
import type { LocationContext } from '../shared/locationContext';

export default function App() {
  const [query, setQuery] = useState('');
  const [shareMessage, setShareMessage] = useState('');
  const [locationContext, setLocationContext] = useState<LocationContext>({ status: 'loading' });
  const visibleNotes = useNoteSearch(demoNotes, query);

  const refreshLocationContext = useCallback(async () => {
    setLocationContext({ status: 'loading' });
    setLocationContext(await getActiveLocationContext());
  }, []);

  useEffect(() => {
    void refreshLocationContext();
  }, [refreshLocationContext]);

  return (
    <div className="popup-shell">
      <Header />
      <LocationContextPanel context={locationContext} onRefresh={() => void refreshLocationContext()} />
      <main className="popup-main" id="community-notes">
        <div className="section-heading">
          <div>
            <p className="eyebrow">KNOW THE NEIGHBORHOOD</p>
            <h1>Community Notes</h1>
          </div>
          <span className="demo-badge"><span aria-hidden="true" /> DEMO</span>
        </div>
        <p className="demo-disclaimer">Sample notes for preview only · Not tied to your location</p>
        <SearchInput value={query} onChange={setQuery} />

        <section className="notes-section" aria-label="Sample community notes">
          <div className="list-heading">
            <h2>Local finds</h2>
            <span>{visibleNotes.length} NOTES</span>
          </div>
          <NotesList notes={visibleNotes} query={query} />
        </section>
      </main>
      <footer className="popup-footer">
        <button
          className="share-button"
          type="button"
          onClick={() => setShareMessage('Note sharing will be available in a future update.')}
        >
          <svg aria-hidden="true" viewBox="0 0 20 20" fill="none">
            <path d="M10 4v12M4 10h12" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" />
          </svg>
          Share a local secret
        </button>
        <p className="share-message" role="status" aria-live="polite">{shareMessage}</p>
        <div className="footer-caption">
          <span>GOOD TO KNOW, WHEREVER YOU GO.</span>
          <span className="footer-pin" aria-hidden="true">●</span>
        </div>
      </footer>
    </div>
  );
}
