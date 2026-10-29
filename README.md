# StreetMap

StreetMap is a Manifest V3 Chrome extension popup for community-shared local knowledge. Milestone 2 adds a small Google Maps context prototype. The current context may identify a place ID or explicit search coordinates from a supported URL; it does not verify the place shown in Google Maps. Sample notes remain fictional and generic, and are never represented as belonging to the current map.

## Requirements and commands

- Node.js 22.6 or newer (the built-in test script uses TypeScript type stripping)
- npm
- Google Chrome

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

The production unpacked extension is written to `.output/chrome-mv3`. To load it manually, open `chrome://extensions`, enable **Developer mode**, choose **Load unpacked**, and select that directory. Reload the extension from the extensions page after rebuilding.

`npm test` uses Node's built-in test runner and TypeScript type stripping, so no separate test framework is required.

## Location context flow

When the popup opens, or when **Refresh** is pressed, it reads the active tab after the user's extension action. For a supported Maps tab it sends a small typed request to the statically declared content script. The content script reads only its current `location.href`; it does not inspect the page DOM. The popup validates and normalizes the response before rendering it. A missing content script gets one short retry, then a clear error state.

The parser accepts only Google's documented Maps search URL form (`/maps/search/?api=1&query=...`) when it contains either:

- `query_place_id` alongside the required query, shown as an **unverified place reference**; or
- a query consisting only of valid latitude and longitude, shown as **search coordinates**.

Google documents `query` as supporting both specific locations and broad searches. StreetMap therefore does not treat arbitrary query text as a selected place. It also ignores map-center coordinates and undocumented `/maps/place/.../@...` routes. Missing or malformed values remain unavailable; labels and coordinates are never inferred. See Google's [Maps URLs documentation](https://developers.google.com/maps/documentation/urls/get-started).

This prototype does not resolve a place ID to a name, confirm the visible Maps selection, or detect every normal Maps UI route. Google's public URL docs define constructed links for searches, directions, map views, and Street View; they do not define the website's internal SPA routes as a place-selection API. StreetMap therefore leaves undocumented routes unconfirmed. Context is refreshed whenever the popup opens and on the Refresh button. If the URL changes while the popup remains open, press Refresh; there is no polling or stored location history.

## Permissions and privacy

- `activeTab` lets the popup read the current tab URL in response to the user's toolbar click. StreetMap does not request the broad `tabs` permission.
- The static content script matches HTTPS `/maps` pages on `www.google.com`, `maps.google.com`, and `www.google.co.in`. It runs only on those matched paths. No `<all_urls>`, geolocation, history, cookies, or backend access is requested. Chrome's site-access grant is scoped to the listed hosts; the content-script match paths limit where the script executes.
- The popup sends no full URL to the content script. The content script returns only the supported hostname and a validated place ID or coordinate pair. Nothing is persisted or sent to a service.

The `activeTab` permission is temporary and tied to user invocation, as described in Chrome's [activeTab documentation](https://developer.chrome.com/docs/extensions/develop/concepts/activeTab). WXT's [content-script entrypoints](https://wxt.dev/guide/essentials/content-scripts) generate the MV3 content-script declaration from `src/entrypoints/google-maps.content.ts`.

## Manual checks

Use the WXT development browser profile or load the production directory as an unpacked extension:

1. Open a non-Maps page, click StreetMap, and confirm **No Google Maps tab detected**.
2. Open a supported Google Maps homepage or a broad search such as `coffee shops`; confirm the UI says Maps was detected without claiming a specific place.
3. Open a documented Maps search URL with a valid `query_place_id` or coordinate-only `query`; confirm the popup labels the result as a URL reference and does not claim it verified the visible selection.
4. Change the Maps URL, reopen the popup or press **Refresh**, and confirm the displayed context updates. Switch tabs and repeat.
5. Search the demo notes and enter a query with no matches; confirm the existing filtering and empty state still work, and that notes remain labeled as samples unrelated to the map.

Pure parser, validation, permission-failure, missing-content-script, retry, and malformed-response cases are covered by `npm test`. The automated tests do not simulate Chrome's actual content-script injection or toolbar lifecycle; use the manual steps above for that browser check.

## Structure

- `src/entrypoints/popup` — popup HTML and React mount.
- `src/entrypoints/google-maps.content.ts` — static, URL-only Maps content script.
- `src/shared/locationContext.ts` — shared message contract, URL parsing, validation, and testable active-tab resolution.
- `src/popup/components` — popup UI, including the location-context panel and reusable notes states.
- `src/popup/services/locationContext.ts` — isolated Chrome tab and message APIs.
- `src/popup/data` — fictional demo notes.
- `public/icons` — local toolbar PNGs.
- `wxt.config.ts` — MV3 metadata and the `activeTab` permission.
