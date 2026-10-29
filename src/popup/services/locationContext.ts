import { browser } from 'wxt/browser';
import {
  LOCATION_CONTEXT_REQUEST,
  resolveActiveLocationContext,
  type LocationContextMessenger,
} from '../../shared/locationContext';

const browserMessenger: LocationContextMessenger = {
  async getActiveTab() {
    const [tab] = await browser.tabs.query({ active: true, lastFocusedWindow: true });
    return tab ? { id: tab.id, url: tab.url } : undefined;
  },
  requestContext(tabId) {
    return browser.tabs.sendMessage(tabId, { type: LOCATION_CONTEXT_REQUEST });
  },
};

export function getActiveLocationContext() {
  return resolveActiveLocationContext(browserMessenger);
}
