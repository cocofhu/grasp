/**
 * Protocol allowlist for user-supplied image URLs assigned to img.src
 * (CodeQL #1 XSS / #3 open redirect).
 *
 * Returns a freshly built safe string (never the original tainted input):
 * - data: rebuilt from canonical MIME literal + charset-copied payload
 * - http(s): URL.href after protocol check
 * or empty string when the input must not be assigned to img.src.
 */

/** Canonical MIME literals only — keys after normalizing image/jpg → image/jpeg. */
const CANONICAL_DATA_MIME = {
    'image/png': 'image/png',
    'image/jpeg': 'image/jpeg',
    'image/gif': 'image/gif',
    'image/webp': 'image/webp',
};

/**
 * Copy payload through an allowlist char-by-char so the result is not the
 * original tainted string (CodeQL taint barrier for img.src assignment).
 * @param {string} payload
 * @param {boolean} isBase64
 * @returns {string|null}
 */
function copySafeDataPayload(payload, isBase64) {
    let out = '';
    for (let i = 0; i < payload.length; i++) {
        const c = payload.charCodeAt(i);
        if (isBase64) {
            // A-Z a-z 0-9 + / =
            if (
                (c >= 65 && c <= 90) ||
                (c >= 97 && c <= 122) ||
                (c >= 48 && c <= 57) ||
                c === 43 ||
                c === 47 ||
                c === 61
            ) {
                out += String.fromCharCode(c);
            } else {
                return null;
            }
            continue;
        }
        // Non-base64 data: percent-encoding and common image-payload bytes.
        if (
            (c >= 65 && c <= 90) ||
            (c >= 97 && c <= 122) ||
            (c >= 48 && c <= 57) ||
            c === 37 || // %
            c === 43 || // +
            c === 47 || // /
            c === 61 || // =
            c === 46 || // .
            c === 95 || // _
            c === 45 // -
        ) {
            out += String.fromCharCode(c);
        } else {
            return null;
        }
    }
    return out;
}

/**
 * @param {unknown} url
 * @returns {string} Safe URL for img.src, or "" if rejected.
 */
export function sanitizeImageURL(url) {
    if (typeof url !== 'string') return '';
    const t = url.trim();
    if (!t) return '';

    if (/^data:/i.test(t)) {
        const m = /^data:(image\/[a-z0-9.+-]+)(;base64)?,(.*)$/is.exec(t);
        if (!m) return '';
        let mimeKey = m[1].toLowerCase();
        if (mimeKey === 'image/jpg') mimeKey = 'image/jpeg';
        const mime = CANONICAL_DATA_MIME[mimeKey];
        if (!mime) return '';
        const isBase64 = Boolean(m[2]);
        const payload = copySafeDataPayload(m[3] ?? '', isBase64);
        if (payload === null) return '';
        // Rebuild from literal MIME + copied payload — never return original t.
        return 'data:' + mime + (isBase64 ? ';base64' : '') + ',' + payload;
    }

    try {
        const u = new URL(t);
        if (u.protocol !== 'http:' && u.protocol !== 'https:') return '';
        return u.href;
    } catch {
        return '';
    }
}
