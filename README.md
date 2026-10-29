# StreetMap

A small Manifest V3 Chrome extension popup foundation for StreetMap. This milestone contains fictional, clearly labeled sample notes only. It does not identify the active Google Maps location, contact a service, or submit notes.

## Requirements

- Node.js 22 or newer
- npm
- Google Chrome

## Install and develop

```sh
npm install
npm run dev
```

WXT starts a development build at `.output/chrome-mv3-dev` and opens a separate browser profile with the extension installed. Click the StreetMap toolbar icon in that window to open the popup. WXT rebuilds the development extension when files change.

## Build and checks

```sh
npm run build
npm run lint
npm run typecheck
```

The production, unpacked extension is written to `.output/chrome-mv3`. To load it, open `chrome://extensions`, enable **Developer mode**, choose **Load unpacked**, and select the `.output/chrome-mv3` directory.

There is no separate preview server for a Chrome extension popup. Use `npm run dev` to preview it through Chrome, or load the production output to inspect the release build.

## Project structure

- `src/entrypoints/popup` — WXT popup document and React mount point.
- `src/popup/components` — focused popup UI components, including reusable loading and error states.
- `src/popup/data` — separate, fictional demo note content.
- `src/popup/hooks` — search filtering logic.
- `src/popup/types` — the typed note model and category values.
- `public/icons` — local PNG toolbar icons at 16, 32, 48, and 128 px.
- `wxt.config.ts` — MV3 metadata and Vite React setup.

WXT is a maintained Vite-based web extension framework. It handles the Chrome extension entrypoint and generates the production Manifest V3 file and bundled paths in `.output/chrome-mv3`, avoiding custom manifest-copy and bundle wiring. The source manifest is configured in `wxt.config.ts`.

The extension currently requests no permissions, host access, background worker, or content scripts. Google Maps integration and backend features are intentionally out of scope.
