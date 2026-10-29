# StreetMap

StreetMap is a Manifest V3 Chrome extension popup for community-shared local knowledge. The popup reads public notes from the StreetMap API using only context the URL reader can safely establish: broad-area labels, unverified place-page labels, and map-center coordinates. It does not verify which place is selected in Maps. It never treats URL text or map-center coordinates as a verified venue identity or exact venue location.

## Requirements and commands

- Node.js 22.6 or newer
- npm
- Google Chrome
- For live local notes: the Go API and the documented PostGIS database/migrations (see [backend setup](backend/README.md))

```sh
npm install
npm run dev
```

WXT prepares `.output/chrome-mv3-dev` and opens its own development browser profile with the extension installed. This does not install StreetMap into your regular Chrome profile. Click the StreetMap toolbar button in that development window to open the popup.

```sh
npm run build
npm run lint
npm run typecheck
npm test
```

`npm run dev` uses `http://127.0.0.1:8080/api/v1` for the API and adds only `http://127.0.0.1:8080/*` as a development host permission. The backend must be listening on its documented default address. The popup uses extension-page `fetch`; the development host permission authorizes that specific loopback origin, so the API does not need a broad CORS policy.

Production builds do not default to localhost and do not include the development loopback host permission. They show an API-configuration state for supported Maps contexts; this milestone has no production API setting, and no production API is currently deployed.

To run the extension against the local backend, follow the database and environment setup in [`backend/README.md`](backend/README.md), apply any pending Goose migrations, and start the API on its documented `127.0.0.1:8080` address. Then run `npm run dev` from the repository root. The popup makes public read-only requests; it does not require an account or send cookies.

The production unpacked extension is written to `.output/chrome-mv3`. To load it manually, open `chrome://extensions`, enable **Developer mode**, choose **Load unpacked**, and select that directory. Reload the extension from the extensions page after rebuilding. For API-backed results while developing, use the WXT development profile (`npm run dev`) or load the development output with its local-only host permission.

`npm test` uses Node's built-in test runner with `tsx` to execute the TypeScript and React component tests. `happy-dom` supplies a lightweight DOM for the popup interaction checks.

## Location context flow

When the popup opens, or when **Refresh** is pressed, it reads the active tab after the user's extension action. For a supported Maps tab it sends a small typed request to the statically declared content script. The content script reads only its current `location.href`; it does not inspect the page DOM. The popup validates and normalizes the response before rendering it. If the static listener is absent (for example, Maps was already open when the extension was installed or reloaded), the popup injects the same packaged reader into that active Maps tab and retries once. If injection or the retry fails, it shows an error rather than treating the failure as missing context.

Once a supported context is available, the popup makes a public `GET /api/v1/notes` request. Broad-area and place-page labels use the corresponding label selector; map-center routes use only `context_type=map_center` with a 5 km radius. If a place-page URL also contains map-center coordinates, the label query is used and the coordinates remain context-panel information; the API contract accepts one selector per request. The extension does not send Maps place IDs as StreetMap location IDs and does not send URL-derived coordinates as user-confirmed exact coordinates. The API response is runtime-validated before display. Exact, context-match, and nearby arrays stay distinct. Label matches are called broad-area or unverified-reference matches; nearby notes show their tagged location and distance from the map center. Expand a note inline to see its body, author, vote count, tagged location, and match provenance. API pagination uses the server cursor through a **Load more notes** control.

Unsupported contexts such as a Maps homepage or an unresolved Maps place ID do not trigger an API request. Network/API errors and malformed responses show distinct error text with retry. A production build shows a configuration error for supported contexts instead of contacting localhost.

The parser uses only visible URL text. A `/maps/place/<label>/` route is displayed as **Place page detected**; its safely decoded label comes from the URL and is not an independently verified venue identity. If the same URL includes `@latitude,longitude,zoomz`, those numbers are shown separately as **map-center coordinates**, never as the venue's exact coordinates. A `/maps/search/?query=...` URL is broad search context, while a Maps homepage has no reliable place context. A documented `query_place_id` paired with a query is shown as a **specific place reference, unverified**; StreetMap does not infer or resolve a business name. Other Maps routes may have no reliable location context. See Google's [Maps URLs documentation](https://developers.google.com/maps/documentation/urls/get-started); its URL formats do not establish the visible website selection as verified.

This prototype does not resolve a place ID to a name, confirm the visible Maps selection, or detect every Maps route. It uses no DOM selectors, internal application state, or private endpoints. Context is refreshed whenever the popup opens and on the Refresh button. If the URL changes while the popup remains open, press Refresh; there is no polling or stored location history.

## Permissions and privacy

- `activeTab` lets the popup read the current tab URL in response to the user's toolbar click. StreetMap does not request the broad `tabs` permission.
- The static content script matches HTTPS `/maps` pages on `google.com`, `www.google.com`, `maps.google.com`, and `www.google.co.in`. `scripting` is used only to inject that same local reader into the active supported Maps tab after the static listener fails to respond; `activeTab` limits this recovery to the tab the user invoked the popup from. No `<all_urls>`, geolocation, history, cookies, or backend access is requested.
- The popup sends no full URL to the content script. The content script returns only a supported hostname and parsed context fields (area/search label, map center, or unverified place ID). The popup sends only the supported selector fields to the public API; it does not send the full Maps URL, cookies, credentials, or personal data. Fetch uses `credentials: 'omit'`.

The `activeTab` permission is temporary and tied to user invocation, as described in Chrome's [activeTab documentation](https://developer.chrome.com/docs/extensions/develop/concepts/activeTab). WXT's [content-script entrypoints](https://wxt.dev/guide/essentials/content-scripts) generate the MV3 content-script declaration from `src/entrypoints/google-maps.content.ts`.

## Manual checks

Use the WXT development browser profile or load the production directory as an unpacked extension:

1. Open a non-Maps page, click StreetMap, and confirm **Google Maps not detected**.
2. Open a supported Google Maps URL without a recognized place/search/map-center signal; confirm the UI says Maps was detected but context is unavailable.
3. Open `https://www.google.com/maps/place/GP+Hotel+%26+Banquet/@26.8178068,80.9071453,15.26z/`; confirm the URL-derived place label and separately labeled map center appear without a verified-venue or exact-coordinate claim.
4. Open a search URL with `query=coffee+shops`, then a URL with a valid `query_place_id`; confirm search text remains broad context and the ID stays unverified.
5. Change the Maps URL, reopen the popup or press **Refresh**, and confirm the displayed context updates. Switch tabs and repeat.
6. With the API available, confirm notes are loaded for a supported context, that broad/unverified label matches remain distinct from nearby map-center results, and that expanding a note reveals its author, content, tagged location, and provenance. Search for text and a no-match query; use **Load more notes** when the API has another page.
7. Stop the API or use a malformed test response to confirm the error state and **Try again** action. Open a Maps homepage and confirm no context request is sent.

Parser, API envelope validation, query construction, empty/error/retry state, inline expansion/collapse, permission-failure, missing-content-script, and malformed-context cases are covered by `npm test`. The automated tests do not simulate Chrome's actual content-script injection or toolbar lifecycle; use the manual steps above for that browser check.

## Structure

- `src/entrypoints/popup` — popup HTML and React mount.
- `src/entrypoints/google-maps.content.ts` — static, URL-only Maps content script.
- `src/shared/locationContext.ts` — shared message contract, URL parsing, validation, and testable active-tab resolution.
- `src/popup/components` — popup UI, including the location-context panel and expandable API note cards.
- `src/popup/services/locationContext.ts` — isolated Chrome tab and message APIs.
- `src/popup/services/publicNotes.ts` — context-to-query mapping, API request, and untrusted response validation.
- `public/icons` — local toolbar PNGs.
- `wxt.config.ts` — MV3 metadata, existing `activeTab` and `scripting` permissions, and the development-only local API host permission.

## Backend foundation

The Go API and PostgreSQL/PostGIS schema live in [`backend/`](backend/README.md). The public notes endpoint is read-only; this extension milestone adds no authentication, note creation, voting, or reporting.
