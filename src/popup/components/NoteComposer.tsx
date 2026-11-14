import { useEffect, useRef, useState, type FormEvent } from 'react';
import type { LocationContext } from '../../shared/locationContext';
import {
  createNoteAttempt,
  CreateNoteApiError,
  type CreateNoteAttempt,
  type CreateNoteLocation,
  type CreateNoteRequest,
} from '../services/createNote';
import { NOTE_CATEGORIES, NOTE_CATEGORY_LABELS, type NoteCategory, type PublicNote } from '../types/note';

const TITLE_MAX_LENGTH = 120;
const BODY_MAX_LENGTH = 5000;
const CATEGORY_OPTIONS: NoteCategory[] = [...NOTE_CATEGORIES];

export interface NoteComposerActions {
  createNoteAttempt(request: CreateNoteRequest): CreateNoteAttempt;
}

interface NoteComposerProps {
  context: LocationContext;
  isAuthenticated?: boolean;
  authPanelOpen?: boolean;
  onAuthenticationRequired?: () => void;
  onClose: () => void;
  onPublished?: (note: PublicNote) => void;
  actions?: NoteComposerActions;
}

interface ComposerLocation {
  choices: ComposerLocationChoice[];
  heading: string;
  label?: string;
  details: string[];
}

interface ComposerLocationChoice {
  type: 'unverified' | 'broad' | 'coordinates';
  label: string;
  heading: string;
  requestLocation: Exclude<CreateNoteLocation, { type: 'coordinates' }> | null;
  coordinates?: { latitude: number; longitude: number };
  details: string[];
}

const defaultActions: NoteComposerActions = { createNoteAttempt };

export function getComposerLocation(context: LocationContext): ComposerLocation {
  switch (context.status) {
    case 'broad-area': {
      const center = validMapCenter(context.mapCenter);
      const details = [
        'This is a broad search label, not a confirmed selected place.',
        ...(center ? [`Map center: ${formatCoordinates(center.latitude, center.longitude)} (view center only)`] : []),
      ];
      return {
        choices: [
          {
            type: 'broad',
            label: context.label,
            heading: 'Broad area',
            requestLocation: { type: 'broad', label: context.label },
            details,
          },
          ...(center ? [createCoordinateChoice(center)] : []),
        ],
        heading: 'Area/search context',
        label: context.label,
        details,
      };
    }
    case 'place-page-unverified': {
      const center = validMapCenter(context.mapCenter);
      const details = [
        'This name comes from the Maps URL and has not been independently verified.',
        ...(center ? [`Map center: ${formatCoordinates(center.latitude, center.longitude)} (not the venue coordinates)`] : []),
      ];
      return {
        choices: [
          {
            type: 'unverified',
            label: context.label,
            heading: 'Place page detected · unverified',
            requestLocation: { type: 'unverified', label: context.label },
            details,
          },
          ...(center ? [createCoordinateChoice(center)] : []),
        ],
        heading: 'Place page detected · unverified',
        label: context.label,
        details,
      };
    }
    case 'map-center':
      if (!validMapCenter(context.mapCenter)) {
        return { choices: [], heading: 'Invalid map center', details: ['Google Maps did not provide usable coordinates.'] };
      }
      return {
        choices: [createCoordinateChoice(context.mapCenter)],
        heading: 'Map center only',
        label: formatCoordinates(context.mapCenter.latitude, context.mapCenter.longitude),
        details: ['These coordinates describe the map view, not a selected place.'],
      };
    case 'unverified-place-reference':
      return {
        choices: [],
        heading: 'Unresolved place reference',
        label: 'Maps provides a place reference without a readable name.',
        details: ['StreetMap cannot safely attach a note until a readable place or area label is available.'],
      };
    case 'maps-no-location':
      return {
        choices: [],
        heading: 'No usable location context',
        details: ['Select a place page or search for an area in Google Maps, then refresh the context.'],
      };
    case 'maps-unavailable':
      return {
        choices: [],
        heading: 'Google Maps not detected',
        details: ['Open a supported Google Maps page with a place name or area search.'],
      };
    case 'loading':
      return {
        choices: [],
        heading: 'Checking Maps context',
        details: ['Wait for the current tab context to finish loading, then reopen the composer.'],
      };
    case 'error':
      return {
        choices: [],
        heading: 'Location context unavailable',
        details: ['Refresh the Maps context and try again.'],
      };
  }
}

function createCoordinateChoice(center: { latitude: number; longitude: number }): ComposerLocationChoice {
  return {
    type: 'coordinates',
    label: formatCoordinates(center.latitude, center.longitude),
    heading: 'Map center coordinates',
    requestLocation: null,
    coordinates: center,
    details: ['These map-center coordinates are not a selected place or verified venue location. Confirm only if this point is appropriate for your note.'],
  };
}

function validMapCenter(value: { latitude: number; longitude: number } | undefined): { latitude: number; longitude: number } | null {
  if (!value || !Number.isFinite(value.latitude) || !Number.isFinite(value.longitude)) return null;
  if (value.latitude < -90 || value.latitude > 90 || value.longitude < -180 || value.longitude > 180) return null;
  return value;
}

function getConfirmedLocation(
  choice: ComposerLocationChoice,
  coordinatesConfirmed: boolean,
): CreateNoteLocation | null {
  if (choice.type !== 'coordinates') return choice.requestLocation;
  if (!coordinatesConfirmed || !choice.coordinates) return null;
  return {
    type: 'coordinates',
    latitude: choice.coordinates.latitude,
    longitude: choice.coordinates.longitude,
    user_confirmed: true,
  };
}

export function countCharacters(value: string): number {
  return Array.from(value).length;
}

export type ComposerFieldErrors = Partial<Record<'title' | 'body' | 'category', string>>;

export function validateComposerFields(title: string, body: string, category: NoteCategory): ComposerFieldErrors {
  const nextErrors: ComposerFieldErrors = {};
  const titleLength = countCharacters(title.trim());
  const bodyLength = countCharacters(body.trim());
  if (titleLength < 3) nextErrors.title = 'Enter a title with at least 3 characters.';
  else if (titleLength > TITLE_MAX_LENGTH) nextErrors.title = `Keep the title to ${TITLE_MAX_LENGTH} characters or fewer.`;
  if (bodyLength < 1) nextErrors.body = 'Add a description of the local knowledge.';
  else if (bodyLength > BODY_MAX_LENGTH) nextErrors.body = `Keep the description to ${BODY_MAX_LENGTH.toLocaleString('en-US')} characters or fewer.`;
  if (!NOTE_CATEGORIES.includes(category)) nextErrors.category = 'Choose a supported category.';
  return nextErrors;
}

export default function NoteComposer({
  context,
  isAuthenticated = false,
  authPanelOpen = false,
  onAuthenticationRequired,
  onClose,
  onPublished,
  actions = defaultActions,
}: NoteComposerProps) {
  const dialogRef = useRef<HTMLElement>(null);
  const closeButtonRef = useRef<HTMLButtonElement>(null);
  const submitLock = useRef(false);
  const attemptRef = useRef<{ signature: string; attempt: CreateNoteAttempt } | null>(null);
  const authPanelOpenRef = useRef(authPanelOpen);
  const wasAuthenticatedRef = useRef(isAuthenticated);
  authPanelOpenRef.current = authPanelOpen;
  const [title, setTitle] = useState('');
  const [body, setBody] = useState('');
  const [category, setCategory] = useState<NoteCategory>('local_tip');
  const [hasSubmitted, setHasSubmitted] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [authenticationRequired, setAuthenticationRequired] = useState(false);
  const [fieldErrors, setFieldErrors] = useState<ComposerFieldErrors>({});
  const [publishedNote, setPublishedNote] = useState<PublicNote | null>(null);
  const [location] = useState(() => getComposerLocation(context));
  const [selectedLocationType, setSelectedLocationType] = useState<ComposerLocationChoice['type'] | null>(
    () => location.choices[0]?.type ?? null,
  );
  const [coordinatesConfirmed, setCoordinatesConfirmed] = useState(false);
  const [locationError, setLocationError] = useState('');
  const selectedLocation = location.choices.find((choice) => choice.type === selectedLocationType) ?? null;

  useEffect(() => {
    const becameAuthenticated = !wasAuthenticatedRef.current && isAuthenticated;
    wasAuthenticatedRef.current = isAuthenticated;
    if (!becameAuthenticated || !authenticationRequired) return;
    setAuthenticationRequired(false);
    setError('You’re signed in. Review your draft, then select “Share note” to publish it.');
  }, [isAuthenticated, authenticationRequired]);

  useEffect(() => {
    if (authPanelOpen) return undefined;
    const previousFocus = document.activeElement;
    closeButtonRef.current?.focus();
    function handleKeyDown(event: KeyboardEvent) {
      if (event.key === 'Escape' && !submitLock.current) {
        event.preventDefault();
        onClose();
        return;
      }
      if (event.key !== 'Tab') return;
      const focusable = dialogRef.current?.querySelectorAll<HTMLElement>(
        'button:not(:disabled), input:not(:disabled), textarea:not(:disabled), select:not(:disabled)',
      );
      if (!focusable?.length) return;
      const first = focusable[0];
      const last = focusable[focusable.length - 1];
      if (event.shiftKey && document.activeElement === first) {
        event.preventDefault();
        last.focus();
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault();
        first.focus();
      }
    }
    document.addEventListener('keydown', handleKeyDown);
    return () => {
      document.removeEventListener('keydown', handleKeyDown);
      if (!authPanelOpenRef.current && previousFocus instanceof HTMLElement) previousFocus.focus();
    };
  }, [authPanelOpen, onClose]);

  function validate(): boolean {
    const nextErrors = validateComposerFields(title, body, category);
    setFieldErrors(nextErrors);
    return Object.keys(nextErrors).length === 0;
  }

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (submitLock.current || !selectedLocation || publishedNote) return;
    const requestLocation = getConfirmedLocation(selectedLocation, coordinatesConfirmed);
    if (!requestLocation) {
      setLocationError('Confirm that you want to use these map-center coordinates for this note.');
      return;
    }
    setHasSubmitted(true);
    setError('');
    setLocationError('');
    if (!validate()) return;

    const request: CreateNoteRequest = {
      location: requestLocation,
      title: title.trim(),
      body: body.trim(),
      category,
    };
    const signature = JSON.stringify(request);
    try {
      if (!attemptRef.current || attemptRef.current.signature !== signature) {
        attemptRef.current = { signature, attempt: actions.createNoteAttempt(request) };
      }
    } catch (cause) {
      setError(cause instanceof CreateNoteApiError ? cause.message : 'StreetMap could not prepare this note. Please try again.');
      return;
    }

    submitLock.current = true;
    setBusy(true);
    try {
      const result = await attemptRef.current.attempt.submit();
      setPublishedNote(result.note);
      onPublished?.(result.note);
    } catch (cause) {
      if (cause instanceof CreateNoteApiError) {
        if (cause.kind === 'authentication') {
          setAuthenticationRequired(true);
          const diagnosticReference = cause.diagnosticCode
            ? ` (Reference: ${cause.diagnosticCode})`
            : '';
          setError(
            isAuthenticated
              ? `StreetMap could not verify the current session. Sign out in the account panel, then sign in again. Your draft is still here.${diagnosticReference}`
              : `Sign in to publish this note. Your draft will stay here, and you can review it before publishing.${diagnosticReference}`,
          );
          onAuthenticationRequired?.();
        } else {
          setError(cause.message);
        }
        setFieldErrors(cause.fieldErrors ?? {});
      } else {
        setError('StreetMap could not publish this note. Please try again.');
      }
    } finally {
      submitLock.current = false;
      setBusy(false);
    }
  }

  function updateTitle(value: string) {
    setTitle(value);
    if (hasSubmitted) setFieldErrors((current) => ({ ...current, title: undefined }));
    setError('');
  }

  function updateBody(value: string) {
    setBody(value);
    if (hasSubmitted) setFieldErrors((current) => ({ ...current, body: undefined }));
    setError('');
  }

  return (
    <div className="composer-backdrop">
      <section ref={dialogRef} className="note-composer" role="dialog" aria-modal="true" aria-labelledby="composer-title">
        <header className="composer-header">
          <div>
            <p className="eyebrow">SHARE LOCAL KNOWLEDGE</p>
            <h2 id="composer-title">Share a local secret</h2>
          </div>
          <button ref={closeButtonRef} className="auth-close" type="button" onClick={onClose} disabled={busy} aria-label="Close note composer">×</button>
        </header>

        {publishedNote ? (
          <div className="composer-success" role="status" aria-live="polite">
            <span className="composer-success-mark" aria-hidden="true">✓</span>
            <h3>Note shared</h3>
            <p><strong>{publishedNote.title}</strong> is published and public.</p>
            <p>StreetMap is refreshing the notes for this Maps context.</p>
            <button className="auth-submit" type="button" onClick={onClose}>Back to Community Notes</button>
          </div>
        ) : !selectedLocation ? (
          <div className="composer-unavailable" role="status">
            <h3>{location.heading}</h3>
            {location.label && <p className="composer-location-label">{location.label}</p>}
            {location.details.map((detail) => <p key={detail}>{detail}</p>)}
            <p className="composer-unavailable-note">This context cannot be safely attached to a note yet.</p>
            <button className="auth-secondary-button" type="button" onClick={onClose}>Close</button>
          </div>
        ) : (
          <form className="composer-form" onSubmit={(event) => void handleSubmit(event)} noValidate>
            <div className="composer-scroll-area">
              <section className="composer-location" aria-label="Note location context">
                <p className="composer-location-heading">Location for this note</p>
                {location.choices.length > 1 && (
                  <>
                    <label className="auth-label" htmlFor="composer-location-type">Location type</label>
                    <select
                      id="composer-location-type"
                      className="composer-input composer-select"
                      value={selectedLocation.type}
                      disabled={busy}
                      onChange={(event) => {
                        setSelectedLocationType(event.currentTarget.value as ComposerLocationChoice['type']);
                        setCoordinatesConfirmed(false);
                        setLocationError('');
                        setError('');
                      }}
                    >
                      {location.choices.map((choice) => <option key={choice.type} value={choice.type}>{getLocationChoiceLabel(choice.type)}</option>)}
                    </select>
                  </>
                )}
                <p className="composer-location-heading composer-location-type-label">{selectedLocation.heading}</p>
                <p className="composer-location-label">{selectedLocation.label}</p>
                {selectedLocation.details.map((detail) => <p className="composer-location-detail" key={detail}>{detail}</p>)}
                {selectedLocation.type === 'coordinates' && (
                  <label className="composer-coordinate-confirmation">
                    <input
                      type="checkbox"
                      checked={coordinatesConfirmed}
                      disabled={busy}
                      onChange={(event) => {
                        setCoordinatesConfirmed(event.currentTarget.checked);
                        setLocationError('');
                      }}
                    />
                    <span>I confirm these map-center coordinates are the location I want to use for this note.</span>
                  </label>
                )}
                {locationError && <p className="composer-field-error" role="alert">{locationError}</p>}
                {selectedLocation.type !== 'coordinates' && (
                  <p className="composer-location-detail">Verified StreetMap place selection is unavailable here. Maps labels are not verified StreetMap location IDs.</p>
                )}
              </section>

              <label className="auth-label" htmlFor="composer-title-input">Title</label>
              <input
                id="composer-title-input"
                className="composer-input"
                type="text"
                autoComplete="off"
                required
                value={title}
                onInput={(event) => updateTitle(event.currentTarget.value)}
                aria-invalid={Boolean(fieldErrors.title)}
                aria-describedby={fieldErrors.title ? 'composer-title-error composer-title-count' : 'composer-title-count'}
                disabled={busy}
              />
              <div className="composer-counter-row">
                {fieldErrors.title && <span id="composer-title-error" className="composer-field-error">{fieldErrors.title}</span>}
                <span id="composer-title-count" className="composer-counter">{countCharacters(title)} / {TITLE_MAX_LENGTH}</span>
              </div>

              <label className="auth-label" htmlFor="composer-body-input">Description</label>
              <textarea
                id="composer-body-input"
                className="composer-input composer-textarea"
                rows={5}
                required
                value={body}
                onInput={(event) => updateBody(event.currentTarget.value)}
                aria-invalid={Boolean(fieldErrors.body)}
                aria-describedby={fieldErrors.body ? 'composer-body-error composer-body-count' : 'composer-body-count'}
                disabled={busy}
              />
              <div className="composer-counter-row">
                {fieldErrors.body && <span id="composer-body-error" className="composer-field-error">{fieldErrors.body}</span>}
                <span id="composer-body-count" className="composer-counter">{countCharacters(body)} / {BODY_MAX_LENGTH}</span>
              </div>

              <label className="auth-label" htmlFor="composer-category">Category</label>
              <select
                id="composer-category"
                className="composer-input composer-select"
                required
                value={category}
                onChange={(event) => {
                  setCategory(event.currentTarget.value as NoteCategory);
                  setFieldErrors((current) => ({ ...current, category: undefined }));
                  setError('');
                }}
                aria-invalid={Boolean(fieldErrors.category)}
                aria-describedby={fieldErrors.category ? 'composer-category-error' : undefined}
                disabled={busy}
              >
                {CATEGORY_OPTIONS.map((option) => <option key={option} value={option}>{NOTE_CATEGORY_LABELS[option]}</option>)}
              </select>
              {fieldErrors.category && <p id="composer-category-error" className="composer-field-error" role="alert">{fieldErrors.category}</p>}
            </div>

            {error && <p className="composer-error" role="alert">{error}</p>}
            {authenticationRequired && (
              <div className="composer-auth-required" role="status">
                <button className="auth-secondary-button" type="button" onClick={onAuthenticationRequired} disabled={busy}>
                  {isAuthenticated ? 'Open account' : 'Sign in to publish'}
                </button>
              </div>
            )}
            <div className="composer-actions">
              <button className="auth-secondary-button" type="button" onClick={onClose} disabled={busy}>Cancel</button>
              <button
                className="auth-submit"
                type="submit"
                disabled={busy || (selectedLocation.type === 'coordinates' && !coordinatesConfirmed)}
              >
                {busy ? 'Sharing note…' : 'Share note'}
              </button>
            </div>
          </form>
        )}
      </section>
    </div>
  );
}

function formatCoordinates(latitude: number, longitude: number): string {
  return `${latitude}, ${longitude}`;
}

function getLocationChoiceLabel(type: ComposerLocationChoice['type']): string {
  switch (type) {
    case 'unverified': return 'Place page name (unverified)';
    case 'broad': return 'Broad area';
    case 'coordinates': return 'Map-center coordinates';
  }
}
