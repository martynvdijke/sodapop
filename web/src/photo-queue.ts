/* Offline check-in photos live in IndexedDB, keyed by the check-in's
   idempotency_key. The localStorage queue keeps only text fields, because
   File inputs cannot be serialised. */

const DB_NAME = 'sodapop-offline'
const DB_VERSION = 1
const STORE = 'checkin-photos'
const MIN_FREE_BYTES = 200 * 1024 * 1024

type PhotoRow = { key: string; blob: Blob; name: string }

/** Keys currently being written, so garbage collection never races a save. */
const inFlight = new Set<string>()

function available(): boolean {
  return typeof indexedDB !== 'undefined'
}

function openDb(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const request = indexedDB.open(DB_NAME, DB_VERSION)
    request.onupgradeneeded = () => {
      const db = request.result
      if (!db.objectStoreNames.contains(STORE)) {
        db.createObjectStore(STORE, { keyPath: 'key' })
      }
    }
    request.onsuccess = () => resolve(request.result)
    request.onerror = () => reject(request.error)
  })
}

function withStore<T>(
  mode: IDBTransactionMode,
  run: (store: IDBObjectStore) => IDBRequest<T>,
): Promise<T> {
  return openDb().then(
    (db) =>
      new Promise<T>((resolve, reject) => {
        const transaction = db.transaction(STORE, mode)
        const request = run(transaction.objectStore(STORE))
        request.onsuccess = () => resolve(request.result)
        request.onerror = () => reject(request.error)
        transaction.oncomplete = () => db.close()
        transaction.onabort = () => {
          db.close()
          reject(transaction.error)
        }
      }),
  )
}

/** False when the browser reports less than 200 MB of free storage. */
async function hasRoom(): Promise<boolean> {
  try {
    const estimate = await navigator.storage?.estimate?.()
    const quota = estimate?.quota
    const usage = estimate?.usage ?? 0
    if (typeof quota !== 'number' || quota <= 0) return true
    return quota - usage >= MIN_FREE_BYTES
  } catch {
    return true
  }
}

/** Store the photo for later replay. Returns false when it must be skipped. */
export async function savePhoto(
  key: string,
  blob: Blob,
  name: string,
): Promise<boolean> {
  if (!available()) return false
  if (!(await hasRoom())) return false
  inFlight.add(key)
  try {
    await withStore('readwrite', (store) =>
      store.put({ key, blob, name } satisfies PhotoRow),
    )
    return true
  } catch {
    return false
  } finally {
    inFlight.delete(key)
  }
}

export async function loadPhoto(key: string): Promise<PhotoRow | null> {
  if (!available()) return null
  try {
    const row = await withStore<PhotoRow | undefined>('readonly', (store) =>
      store.get(key),
    )
    return row ?? null
  } catch {
    return null
  }
}

export async function removePhoto(key: string): Promise<void> {
  if (!available()) return
  try {
    await withStore('readwrite', (store) => store.delete(key))
  } catch {
    /* the blob is an optimisation; a failed delete is not fatal */
  }
}

async function listPhotoKeys(): Promise<string[]> {
  if (!available()) return []
  try {
    const keys = await withStore<IDBValidKey[]>('readonly', (store) =>
      store.getAllKeys(),
    )
    return keys.filter((key): key is string => typeof key === 'string')
  } catch {
    return []
  }
}

function deletePhotos(keys: string[]): Promise<void> {
  return openDb().then(
    (db) =>
      new Promise<void>((resolve, reject) => {
        const transaction = db.transaction(STORE, 'readwrite')
        const store = transaction.objectStore(STORE)
        for (const key of keys) store.delete(key)
        transaction.oncomplete = () => {
          db.close()
          resolve()
        }
        transaction.onabort = () => {
          db.close()
          reject(transaction.error)
        }
      }),
  )
}

/**
 * Delete stored photos whose idempotency key is not in `referenced`. Callers
 * pass the keys still present in the check-in queue; writes in progress are
 * always kept so a concurrent save is never collected.
 */
export async function prunePhotos(referenced: Iterable<string>): Promise<void> {
  if (!available()) return
  const keep = new Set(referenced)
  for (const key of inFlight) keep.add(key)
  const orphans = (await listPhotoKeys()).filter((key) => !keep.has(key))
  if (orphans.length === 0) return
  try {
    await deletePhotos(orphans)
  } catch {
    /* orphaned blobs are best-effort cleanup; a failed purge is not fatal */
  }
}
