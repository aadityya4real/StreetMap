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
  async injectContentScript(tabId) {
    await browser.scripting.executeScript({
      target: { tabId },
      files: ['content-scripts/google-maps.js'],
    });
  },
};

const ACTIVE_TAB_URL_POLL_MS = 750;

export function getActiveLocationContext() {
  return resolveActiveLocationContext(browserMessenger);
}

/** Refresh the popup when the active tab's URL changes, including Maps SPA navigation. */
export function subscribeToActiveTabLocationChanges(onChange: () => void): () => void {
  let activeTabIdentity: string | undefined;
  let checkInProgress = false;
  let checkPending = false;

  const checkActiveTab = async () => {
    if (checkInProgress) {
      checkPending = true;
      return;
    }
    checkInProgress = true;
    try {
      const [tab] = await browser.tabs.query({ active: true, lastFocusedWindow: true });
      if (!tab || tab.id === undefined) return;
      const identity = `${tab.id}:${tab.url ?? ''}`;
      if (activeTabIdentity !== undefined && identity !== activeTabIdentity) onChange();
      activeTabIdentity = identity;
    } catch {
      // A transient tabs API failure should not interrupt popup rendering.
    } finally {
      checkInProgress = false;
      if (checkPending) {
        checkPending = false;
        void checkActiveTab();
      }
    }
  };

  const onUpdated = (_tabId: number, changeInfo: { url?: string }) => {
    if (changeInfo.url !== undefined) void checkActiveTab();
  };
  const onActivated = () => void checkActiveTab();
  browser.tabs.onUpdated.addListener(onUpdated);
  browser.tabs.onActivated.addListener(onActivated);
  void checkActiveTab();
  const interval = globalThis.setInterval(() => void checkActiveTab(), ACTIVE_TAB_URL_POLL_MS);

  return () => {
    globalThis.clearInterval(interval);
    browser.tabs.onUpdated.removeListener(onUpdated);
    browser.tabs.onActivated.removeListener(onActivated);
  };
}
