/**
 * Grasp 控制台以 iframe 内嵌 AgentChat（同源 /sandbox-bridge/:id/）时的握手：
 * 主题由宿主决定（#theme= 首屏 + grasp-embed:theme 后续切换），页面就绪后回报宿主，
 * 宿主据此撤掉加载遮罩，而不是等 iframe load（load 要等全部子资源）。
 */

export const THEME_MESSAGE = 'grasp-embed:theme';
export const READY_MESSAGE = 'grasp:agentchat-ready';

/** @param {unknown} v @returns {'light'|'dark'|null} */
function asTheme(v) {
    return v === 'light' || v === 'dark' ? v : null;
}

/** @param {string} hash @returns {'light'|'dark'|null} */
export function parseThemeHash(hash) {
    return asTheme(new URLSearchParams(String(hash || '').replace(/^#/, '')).get('theme'));
}

/** @param {unknown} data @returns {'light'|'dark'|null} */
export function parseThemeMessage(data) {
    if (!data || typeof data !== 'object') return null;
    const m = /** @type {{type?: unknown, theme?: unknown}} */ (data);
    return m.type === THEME_MESSAGE ? asTheme(m.theme) : null;
}

/** @param {Window} win */
export function isEmbedded(win) {
    try {
        return win.parent !== win;
    } catch (_) {
        return true;
    }
}

/**
 * 只认同源父窗口发来的主题；内嵌时主题跟宿主走，不写入本页 localStorage。
 * @param {Window} win
 * @param {(theme: 'light'|'dark') => void} apply
 */
export function bindEmbedTheme(win, apply) {
    win.addEventListener('message', (e) => {
        if (e.source !== win.parent || e.origin !== win.location.origin) return;
        const theme = parseThemeMessage(e.data);
        if (theme) apply(theme);
    });
}

/** @param {Window} win */
export function announceReady(win) {
    if (!isEmbedded(win)) return;
    try {
        win.parent.postMessage({type: READY_MESSAGE}, win.location.origin);
    } catch (_) {
        /* 父窗口跨源时 postMessage 按 targetOrigin 静默丢弃，这里只防御异常环境 */
    }
}
