# StreetMap

StreetMap is a Manifest V3 Chrome extension popup for community-shared local knowledge. Milestone 2 adds a small Google Maps URL-context prototype. It can distinguish broad area/search text, map-center coordinates, and an unverified place reference. It does not verify which place is selected in Maps. Sample notes remain fictional and generic, and are never represented as belonging to the current map.

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

When the popup opens, or when **Refresh** is pressed, it reads the active tab after the user's extension action. For a supported Maps tab it sends a small typed request to the statically declared content script. The content script reads only its current `location.href`; it does not inspect the page DOM. The popup validates and normalizes the response before rendering it. If the static listener is absent (for example, Maps was already open when the extension was installed or reloaded), the popup injects the same packaged reader into that active Maps tab and retries once. If injection or the retry fails, it shows an error rather than treating the failure as missing context.

The parser uses only visible URL text. A `/maps/place/<label>/` route is displayed as **broad area/search context**; for example, `/maps/place/Greater+Noida,+Uttar+Pradesh/` yields the broad label “Greater Noida, Uttar Pradesh.” If the same URL includes `@latitude,longitude,zoomz`, those numbers are shown separately as **map-center coordinates**, never as the selected business's coordinates. A `/maps/search/?query=...` URL is broad search context. A documented `query_place_id` paired with a query is shown as a **specific place reference, unverified**; StreetMap does not infer or resolve a business name. Other Maps routes may have no reliable location context. See Google's [Maps URLs documentation](https://developers.google.com/maps/documentation/urls/get-started); its URL formats do not establish the visible website selection as verified.

This prototype does not resolve a place ID to a name, confirm the visible Maps selection, or detect every Maps route. It uses no DOM selectors, internal application state, or private endpoints. Context is refreshed whenever the popup opens and on the Refresh button. If the URL changes while the popup remains open, press Refresh; there is no polling or stored location history.

## Permissions and privacy

- `activeTab` lets the popup read the current tab URL in response to the user's toolbar click. StreetMap does not request the broad `tabs` permission.
- The static content script matches HTTPS `/maps` pages on `google.com`, `www.google.com`, `maps.google.com`, and `www.google.co.in`. `scripting` is used only to inject that same local reader into the active supported Maps tab after the static listener fails to respond; `activeTab` limits this recovery to the tab the user invoked the popup from. No `<all_urls>`, geolocation, history, cookies, or backend access is requested.
- The popup sends no full URL to the content script. The content script returns only a supported hostname and the parsed context fields (area/search label, map center, or unverified place ID). Nothing is persisted or sent to a service.

The `activeTab` permission is temporary and tied to user invocation, as described in Chrome's [activeTab documentation](https://developer.chrome.com/docs/extensions/develop/concepts/activeTab). WXT's [content-script entrypoints](https://wxt.dev/guide/essentials/content-scripts) generate the MV3 content-script declaration from `src/entrypoints/google-maps.content.ts`.

## Manual checks

Use the WXT development browser profile or load the production directory as an unpacked extension:

1. Open a non-Maps page, click StreetMap, and confirm **Google Maps not detected**.
2. Open a supported Google Maps URL without a recognized place/search/map-center signal; confirm the UI says Maps was detected but context is unavailable.
3. Open `https://www.google.com/maps/place/Greater+Noida,+Uttar+Pradesh/@28.4983349,77.4334236,12z/`; confirm the broad area and separately labeled map center appear, without a selected-business claim.
4. Open a search URL with `query=coffee+shops`, then a URL with a valid `query_place_id`; confirm search text remains broad context and the ID stays unverified.
5. Change the Maps URL, reopen the popup or press **Refresh**, and confirm the displayed context updates. Switch tabs and repeat.
6. Search the demo notes and enter a query with no matches; confirm the existing filtering and empty state still work, and that notes remain labeled as samples unrelated to the map.

Pure parser, validation, permission-failure, missing-content-script, retry, and malformed-response cases are covered by `npm test`. The automated tests do not simulate Chrome's actual content-script injection or toolbar lifecycle; use the manual steps above for that browser check.

## Structure

- `src/entrypoints/popup` — popup HTML and React mount.
- `src/entrypoints/google-maps.content.ts` — static, URL-only Maps content script.
- `src/shared/locationContext.ts` — shared message contract, URL parsing, validation, and testable active-tab resolution.
- `src/popup/components` — popup UI, including the location-context panel and reusable notes states.
- `src/popup/services/locationContext.ts` — isolated Chrome tab and message APIs.
- `src/popup/data` — fictional demo notes.
- `public/icons` — local toolbar PNGs.
- `wxt.config.ts` — MV3 metadata and the narrowly scoped `activeTab` and `scripting` permissions.
