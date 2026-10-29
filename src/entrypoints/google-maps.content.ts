import { browser } from 'wxt/browser';
import { defineContentScript } from 'wxt/utils/define-content-script';
import {
  GOOGLE_MAPS_MATCH_PATTERNS,
  LOCATION_CONTEXT_RESPONSE,
  isLocationContextRequest,
  parseMapsPageUrl,
  type LocationContextResponse,
} from '../shared/locationContext';

export default defineContentScript({
  matches: [...GOOGLE_MAPS_MATCH_PATTERNS],
  runAt: 'document_start',
  main() {
    browser.runtime.onMessage.addListener((message, _sender, sendResponse) => {
      if (!isLocationContextRequest(message)) return;

      const context = parseMapsPageUrl(location.href);
      if (!context) return;

      const response: LocationContextResponse = {
        type: LOCATION_CONTEXT_RESPONSE,
        context,
      };
      sendResponse(response);
    });
  },
});
