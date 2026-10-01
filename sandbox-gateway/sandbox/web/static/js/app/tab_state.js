/**
 * 会话 Tab 的纯逻辑（不依赖 DOM），便于 node:test 单测。
 */

export const DEFAULT_CHAT_ID = 'default';
export const ACTIVE_TAB_STORAGE_KEY = 'acp-bridge-active-chat';

/**
 * Tab 显示名：有标题用标题；default 为「默认」；其余按位置编号。
 * @param {{ id: string, title?: string }} info
 * @param {number} index 在 Tab 条中的位置（0 起）
 */
export function tabLabel(info, index) {
    const t = String(info?.title || '').trim();
    if (t) return t;
    if (info?.id === DEFAULT_CHAT_ID) return '默认';
    return `会话 ${index + 1}`;
}

/**
 * 对比本地 Tab 与服务端会话列表，得出需新增 / 移除的会话（服务端为准，保持服务端顺序）。
 * @param {string[]} localIds
 * @param {{ id: string }[]} serverChats
 * @returns {{ added: { id: string }[], removed: string[] }}
 */
export function reconcileChats(localIds, serverChats) {
    const local = new Set(localIds);
    const server = new Set((serverChats || []).map((c) => c.id));
    return {
        added: (serverChats || []).filter((c) => c && c.id && !local.has(c.id)),
        removed: localIds.filter((id) => !server.has(id)),
    };
}

/**
 * 选出要激活的 Tab：优先上次记住的，否则 default，否则第一个。
 * @param {string|null|undefined} savedId
 * @param {string[]} ids
 */
export function pickActiveId(savedId, ids) {
    if (savedId && ids.includes(savedId)) return savedId;
    if (ids.includes(DEFAULT_CHAT_ID)) return DEFAULT_CHAT_ID;
    return ids[0] || '';
}

/**
 * 关闭 closingId 后应激活的 Tab：优先右侧相邻，其次左侧。
 * @param {string[]} ids 关闭前的顺序
 * @param {string} closingId
 */
export function neighborAfterClose(ids, closingId) {
    const i = ids.indexOf(closingId);
    if (i < 0) return ids[0] || '';
    return ids[i + 1] || ids[i - 1] || '';
}

/**
 * 模型显示名：'auto' / 空为 Auto；在目录中找得到用目录名，否则用 id。
 * @param {string} modelId
 * @param {{ id: string, name?: string }[]} models
 */
export function modelDisplayName(modelId, models) {
    const id = String(modelId || '');
    if (!id || id === 'auto') return 'Auto';
    const found = (models || []).find((m) => m.id === id);
    return (found && found.name) || id;
}

/**
 * 模型弹窗列表：首项「默认（…）」对应空串（跟随 -model / ACP_BRIDGE_MODEL，均未设即 Auto）。
 * @param {{ id: string, name?: string, isDefault?: boolean }[]} models
 * @param {string} defaultModel
 * @returns {{ id: string, name: string, isDefault?: boolean, followDefault?: boolean }[]}
 */
export function modelPickerItems(models, defaultModel) {
    const def = modelDisplayName(defaultModel, models);
    return [
        {id: '', name: `默认（${def}）`, followDefault: true},
        ...(models || []).map((m) => ({...m, name: m.name || m.id})),
    ];
}
