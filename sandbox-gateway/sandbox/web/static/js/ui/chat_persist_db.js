/**
 * IndexedDB 聊天 HTML 快照持久化（库 acp-bridge-chat，键 sessionId，v1 结构）。
 * 打开/写入失败返回 null/false 并 console.warn，不抛错阻塞 UI。
 */

const DB_NAME = 'acp-bridge-chat';
const DB_VERSION = 1;
const STORE_NAME = 'snapshots';

/** @type {IDBDatabase|null} */
let _db = null;
/** @type {Promise<IDBDatabase|null>|null} */
let _openPromise = null;

/**
 * @returns {Promise<IDBDatabase|null>}
 */
export async function openDb() {
    if (_db) return _db;
    if (_openPromise) return _openPromise;

    _openPromise = new Promise((resolve) => {
        if (typeof indexedDB === 'undefined') {
            console.warn('acp-bridge: IndexedDB unavailable, skip local persist');
            resolve(null);
            return;
        }
        let req;
        try {
            req = indexedDB.open(DB_NAME, DB_VERSION);
        } catch (e) {
            console.warn('acp-bridge: IndexedDB open failed', e);
            resolve(null);
            return;
        }
        req.onerror = () => {
            console.warn('acp-bridge: IndexedDB open failed', req.error);
            resolve(null);
        };
        req.onupgradeneeded = () => {
            const db = req.result;
            if (!db.objectStoreNames.contains(STORE_NAME)) {
                db.createObjectStore(STORE_NAME, {keyPath: 'sessionId'});
            }
        };
        req.onsuccess = () => {
            _db = req.result;
            _db.onversionchange = () => {
                try {
                    _db?.close();
                } catch {
                    /* ignore */
                }
                _db = null;
                _openPromise = null;
            };
            resolve(_db);
        };
    });

    return _openPromise;
}

/**
 * @param {unknown} raw
 * @returns {{ v: number, sessionId: string, html: string, ts: number }|null}
 */
function parseSnapshot(raw) {
    if (!raw || typeof raw !== 'object') return null;
    const data = /** @type {{ v?: unknown, sessionId?: unknown, html?: unknown, ts?: unknown }} */ (raw);
    if (data.v !== 1) return null;
    if (typeof data.html !== 'string') return null;
    const sessionId = data.sessionId != null ? String(data.sessionId) : '';
    if (!sessionId) return null;
    const ts = typeof data.ts === 'number' && Number.isFinite(data.ts) ? data.ts : 0;
    return {v: 1, sessionId, html: data.html, ts};
}

/**
 * @param {string} sessionId
 * @param {string} html
 * @param {number} [ts]
 * @returns {Promise<boolean>}
 */
export async function putSnapshot(sessionId, html, ts = Date.now()) {
    const sid = String(sessionId || '');
    if (!sid) return false;
    const db = await openDb();
    if (!db) return false;
    const snapshot = {v: 1, sessionId: sid, html, ts};
    return new Promise((resolve) => {
        const tx = db.transaction(STORE_NAME, 'readwrite');
        tx.onerror = () => {
            console.warn('acp-bridge: IndexedDB put failed', tx.error);
            resolve(false);
        };
        tx.oncomplete = () => resolve(true);
        tx.objectStore(STORE_NAME).put(snapshot);
    });
}

/**
 * @param {string} sessionId
 * @returns {Promise<{ v: number, sessionId: string, html: string, ts: number }|null>}
 */
export async function getSnapshot(sessionId) {
    const sid = String(sessionId || '');
    if (!sid) return null;
    const db = await openDb();
    if (!db) return null;
    return new Promise((resolve) => {
        const tx = db.transaction(STORE_NAME, 'readonly');
        tx.onerror = () => {
            console.warn('acp-bridge: IndexedDB get failed', tx.error);
            resolve(null);
        };
        const req = tx.objectStore(STORE_NAME).get(sid);
        req.onerror = () => {
            console.warn('acp-bridge: IndexedDB get failed', req.error);
            resolve(null);
        };
        req.onsuccess = () => resolve(parseSnapshot(req.result));
    });
}

/**
 * @param {string} sessionId
 * @returns {Promise<boolean>}
 */
export async function deleteSnapshot(sessionId) {
    const sid = String(sessionId || '');
    if (!sid) return false;
    const db = await openDb();
    if (!db) return false;
    return new Promise((resolve) => {
        const tx = db.transaction(STORE_NAME, 'readwrite');
        tx.onerror = () => {
            console.warn('acp-bridge: IndexedDB delete failed', tx.error);
            resolve(false);
        };
        tx.oncomplete = () => resolve(true);
        tx.objectStore(STORE_NAME).delete(sid);
    });
}

/**
 * 删除不在 keepSessionIds 中的所有快照（清理已关闭 Tab / 已重启会话的旧记录）。
 * @param {string|Iterable<string>} keepSessionIds
 * @returns {Promise<void>}
 */
export async function deleteOtherSnapshots(keepSessionIds) {
    const keep = new Set(
        typeof keepSessionIds === 'string' ? [keepSessionIds] : Array.from(keepSessionIds || [], String)
    );
    const db = await openDb();
    if (!db) return;
    await new Promise((resolve) => {
        const tx = db.transaction(STORE_NAME, 'readwrite');
        tx.onerror = () => {
            console.warn('acp-bridge: IndexedDB enumerate delete failed', tx.error);
            resolve(undefined);
        };
        tx.oncomplete = () => resolve(undefined);
        const store = tx.objectStore(STORE_NAME);
        const req = store.openCursor();
        req.onerror = () => {
            console.warn('acp-bridge: IndexedDB enumerate failed', req.error);
            resolve(undefined);
        };
        req.onsuccess = () => {
            const cursor = req.result;
            if (!cursor) return;
            const key = String(cursor.key || '');
            if (key && !keep.has(key)) {
                cursor.delete();
            }
            cursor.continue();
        };
    });
}
