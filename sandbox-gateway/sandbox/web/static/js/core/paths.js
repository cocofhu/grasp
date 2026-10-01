/**
 * 将相对路径解析为完整 URL（基于 document.baseURI，适配反向代理子路径）。
 * 例：baseURI 为 https://example.com/acp/，apiPath("api/events") → https://example.com/acp/api/events
 */
export function apiPath(relative) {
    try {
        return new URL(relative, document.baseURI).href;
    } catch (_) {
        return '/' + relative;
    }
}

/**
 * WebSocket URL。
 * - 默认：相对 `document.baseURI` 解析路径 `ws`（与当前页同路径前缀，便于子路径反代）。
 * - 再将协议换为 ws / wss（与页面 http/https 一致）。
 * - file:// 或无 host 时回退 ws://127.0.0.1:8765/ws。
 * - 可写 <meta name="acp-bridge-ws" content="wss://example.com/acp/ws" /> 覆盖。
 * - chatId 非空时追加 ?chat=<id>（不带则为 default 会话）。
 * @param {string} [chatId]
 */
export function wsURL(chatId) {
    return withChatQuery(baseWsURL(), chatId);
}

function baseWsURL() {
    const meta = document.querySelector('meta[name="acp-bridge-ws"]');
    const override = meta?.getAttribute('content')?.trim();
    if (override) {
        return override;
    }

    if (!location.host || location.protocol === 'file:') {
        return 'ws://127.0.0.1:8765/ws';
    }

    try {
        const u = new URL('ws', document.baseURI);
        u.protocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
        return u.href;
    } catch (_) {
        const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
        return `${proto}//${location.host}/ws`;
    }
}

/**
 * 给 URL 追加 chat 查询参数（chatId 为空时原样返回）。
 * @param {string} url
 * @param {string} [chatId]
 */
export function withChatQuery(url, chatId) {
    const id = String(chatId || '');
    if (!id) return url;
    const sep = url.includes('?') ? '&' : '?';
    return `${url}${sep}chat=${encodeURIComponent(id)}`;
}

/**
 * 带 chat 查询参数的 API 地址。
 * @param {string} relative 如 'api/models'
 * @param {string} [chatId]
 */
export function chatApiPath(relative, chatId) {
    return withChatQuery(apiPath(relative), chatId);
}
