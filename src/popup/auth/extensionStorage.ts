export interface ExtensionLocalStorage {
  get(key: string): Promise<Record<string, unknown>>;
  set(items: Record<string, unknown>): Promise<void>;
  remove(key: string): Promise<void>;
}

export interface AuthStorage {
  getItem(key: string): Promise<string | null>;
  setItem(key: string, value: string): Promise<void>;
  removeItem(key: string): Promise<void>;
}

export interface ExtensionStorageAccess {
  setAccessLevel(options: { accessLevel: 'TRUSTED_CONTEXTS' }): Promise<void>;
}

export async function restrictAuthStorageToTrustedContexts(storage: ExtensionStorageAccess): Promise<void> {
  await storage.setAccessLevel({ accessLevel: 'TRUSTED_CONTEXTS' });
}

export function createAuthStorage(storage: ExtensionLocalStorage): AuthStorage {
  return {
    async getItem(key) {
      const values = await storage.get(key);
      const value = values[key];
      return typeof value === 'string' ? value : null;
    },
    async setItem(key, value) {
      await storage.set({ [key]: value });
    },
    async removeItem(key) {
      await storage.remove(key);
    },
  };
}
