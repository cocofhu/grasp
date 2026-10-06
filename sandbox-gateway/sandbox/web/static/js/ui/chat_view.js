/**
 * 布局：CardRenderer + ConversationGroup 式卡片流；session/update 分发见
 * `conversation/session_update_dispatch.js`（可 registerSessionUpdateHandler 扩展）。
 */

import {humanizeToolTitle, normalizeSessionUpdateKind} from '../core/acp_protocol.js';
import {CardType, dispatchSessionUpdate} from '../conversation/index.js';
import {hydrateMermaid, renderMarkdown, sanitizeChatLogHtml} from '../core/md.js';
import {chatApiPath} from '../core/paths.js';
import {
    collectToolParams,
    flattenToolPayload,
    formatToolContent,
    mergeSessionUpdateEnvelope,
    pickRicherParams,
    statusIcon,
    truncateMiddle,
} from './chat_payload.js';
import {
    deleteSnapshot,
    getSnapshot,
    putSnapshot,
} from './chat_persist_db.js';
import {sanitizeImageURL} from './safe_image_url.js';

const AVATAR_SVG =
    '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" ' +
    'stroke-linecap="round" stroke-linejoin="round"><rect x="4" y="8" width="16" height="12" rx="3"/>' +
    '<path d="M12 4v4M9 13v1M15 13v1M9.5 17h5"/></svg>';

const THINKING_LIVE_LABEL = '思考中';

/** @param {number} ms */
function thoughtDoneLabel(ms) {
    const sec = Math.round(ms / 1000);
    return sec >= 1 ? `已思考 ${sec}s` : '已思考';
}

/** @param {HTMLElement|null} icon @param {string} st */
function setToolStatus(icon, st) {
    if (!icon) return;
    const done = st === 'completed' || st === 'complete' || st === 'success';
    const failed = st === 'failed' || st === 'error' || st === 'cancelled' || st === 'canceled';
    icon.className = `cc-tool-status cc-tool-status--${done ? 'ok' : failed ? 'err' : 'run'}`;
    icon.textContent = statusIcon(st);
    icon.removeAttribute('style');
}

const PERSIST_BANNER_TEXT =
    '本地聊天快照过大，无法写入本地缓存；刷新后可能无法恢复，当前会话仍可通过 eventLog 重放。';

export class ChatView {
    /**
     * @param {HTMLElement} logEl #log
     * @param {HTMLElement} scrollRoot #chatScroll
     * @param {{ chatId?: string }} [opts] chatId 用于历史分页请求（空为 default 会话）
     */
    constructor(logEl, scrollRoot, opts = {}) {
        this.logEl = logEl;
        this.scrollRoot = scrollRoot;
        this.chatId = opts.chatId || '';
        /** @type {{ kind: string|null, bodyEl: HTMLElement|null, mdBuf: string }} */
        this.stream = {kind: null, bodyEl: null, mdBuf: ''};
        /** toolCallId -> entry */
        this._toolById = new Map();
        this._anonToolSeq = 0;
        /** 无 id 的状态更新挂到最近一次工具上 */
        this._stickyToolId = null;
        /** 当前 ACP sessionId，与 Go 侧 Panel 一致；用于刷新后按会话恢复聊天记录 */
        this._persistSessionId = '';
        /** @type {ReturnType<typeof setTimeout>|null} */
        this._persistTimer = null;
        /** 每个页签只尝试从 IndexedDB 恢复一次 */
        this._restoreAttempted = false;
        /** IndexedDB 不可用或写入失败后跳过后续持久化 */
        this._persistDisabled = false;
        /** 持久化失败横幅每页签最多展示一次 */
        this._persistBannerShown = false;
        /** Markdown 内 Mermaid：流式更新时节流，避免每字符 import/run */
        /** @type {Map<HTMLElement, ReturnType<typeof setTimeout>>} */
        this._mermaidTimers = new Map();
        /** 当前助手轮的内容列（单头像下叠正文 / 工具 / 思考） */
        this._turnStack = null;
        /** 重放模式（仅重放时才插入占位符） */
        this._replaying = false;
        /** 待确认的用户消息队列（发送时入队，prompt_begin 时出队显示） */
        this._pendingUserMessages = [];
        /** 历史分页 */
        this._totalTurns = 0;
        this._hasMoreTurns = false;
        this._loadingMore = false;
        this._initScrollWatch();
    }

    _initScrollWatch() {
        this.scrollRoot.addEventListener('scroll', () => {
            if (!this._hasMoreTurns || this._loadingMore) return;
            if (this.scrollRoot.scrollTop < 80) {
                this._loadOlderTurns();
            }
        });
    }

    setHistoryPaging(totalTurns, hasMore) {
        this._totalTurns = totalTurns;
        this._hasMoreTurns = hasMore;
        this._updateLoadMoreHint();
    }

    _updateLoadMoreHint() {
        let hint = this.logEl.querySelector('.cc-load-more-hint');
        if (this._hasMoreTurns) {
            if (!hint) {
                hint = document.createElement('div');
                hint.className = 'cc-load-more-hint';
                hint.textContent = '↑ 滚动加载更早对话';
                this.logEl.prepend(hint);
            }
        } else if (hint) {
            hint.remove();
        }
    }

    async _loadOlderTurns() {
        if (this._loadingMore || !this._hasMoreTurns) return;
        this._loadingMore = true;

        let hint = this.logEl.querySelector('.cc-load-more-hint');
        if (hint) {
            hint.textContent = '加载中…';
            hint.dataset.state = 'loading';
        }

        // _nextBefore 记录下次请求的 turnIndex（初始 = totalTurns - 已显示轮数）
        if (this._nextBefore == null) {
            const displayed = this.logEl.querySelectorAll('.cc-user-row').length;
            this._nextBefore = this._totalTurns - displayed;
        }
        const beforeTurn = this._nextBefore;

        try {
            const res = await fetch(chatApiPath(`api/events?before=${beforeTurn}&limit=10`, this.chatId));
            if (!res.ok) throw new Error(await res.text());
            const data = await res.json();
            const events = Array.isArray(data.events) ? data.events : [];
            this._hasMoreTurns = !!data.hasMore;

            if (events.length > 0) {
                const prevHeight = this.scrollRoot.scrollHeight;
                this._prependEvents(events);
                const newHeight = this.scrollRoot.scrollHeight;
                this.scrollRoot.scrollTop += (newHeight - prevHeight);
            }
            this._hasMoreTurns = !!data.hasMore;
            // 更新 _nextBefore（减去本次加载的轮数）
            const loadedCount = this._countTurnsInEvents(events);
            this._nextBefore = beforeTurn - loadedCount;
            if (this._nextBefore <= 0) this._hasMoreTurns = false;
            this._updateLoadMoreHint();
        } catch (err) {
            console.warn('acp-bridge: 加载历史失败', err);
            if (hint) {
                hint.textContent = '加载失败，滚动重试';
                delete hint.dataset.state;
            }
        } finally {
            this._loadingMore = false;
        }
    }

    _countTurnsInEvents(events) {
        let count = 0;
        for (const ev of events) {
            let parsed;
            try { parsed = typeof ev === 'string' ? JSON.parse(ev) : ev; } catch { continue; }
            if (parsed && parsed.type === 'prompt_begin') count++;
        }
        return count || 1;
    }

    _prependEvents(events) {
        // 将旧事件按轮次重放并插入到顶部
        const tempContainer = document.createElement('div');
        const origLog = this.logEl;
        const origStream = this.stream;
        const origTurnStack = this._turnStack;
        const origToolById = this._toolById;
        const origStickyToolId = this._stickyToolId;
        const origPersistId = this._persistSessionId;
        const origReplaying = this._replaying;

        // 临时将 logEl 指向 temp 容器来重放
        this.logEl = tempContainer;
        this.stream = {kind: null, bodyEl: null, mdBuf: ''};
        this._toolById = new Map();
        this._stickyToolId = null;
        this._turnStack = null;
        this._persistSessionId = '';
        this._replaying = true;

        // 按轮次分组并重放
        const turns = [];
        let currentTurn = null;
        for (const ev of events) {
            let parsed;
            try {
                parsed = typeof ev === 'string' ? JSON.parse(ev) : ev;
            } catch { continue; }
            if (parsed && parsed.type === 'prompt_begin') {
                if (currentTurn) turns.push(currentTurn);
                currentTurn = {userText: '', imageURLs: [], events: [ev]};
                currentTurn.userText = String(parsed.promptText != null ? parsed.promptText : parsed.text || '').trim();
                if (Array.isArray(parsed.imageURLs) && parsed.imageURLs.length > 0) {
                    currentTurn.imageURLs = parsed.imageURLs.filter(u => typeof u === 'string' && u);
                }
            } else if (currentTurn) {
                currentTurn.events.push(ev);
            } else {
                if (!turns.length) {
                    currentTurn = {userText: '', imageURLs: [], events: []};
                }
                currentTurn?.events.push(ev);
            }
        }
        if (currentTurn) turns.push(currentTurn);

        for (const turn of turns) {
            if (turn.userText || (turn.imageURLs && turn.imageURLs.length > 0)) {
                this.appendUser(turn.userText, turn.imageURLs || []);
            }
            for (const ev of turn.events) {
                try {
                    let parsed;
                    try { parsed = typeof ev === 'string' ? JSON.parse(ev) : ev; } catch { parsed = null; }
                    if (parsed && parsed.type === 'prompt_begin') continue;
                    this.handleEvent(ev);
                } catch (e) {
                    console.warn('acp-bridge: 重放旧事件失败', e);
                }
            }
        }
        this.endStream();
        this._finishAssistantTurn();

        // 恢复状态
        this.logEl = origLog;
        this.stream = origStream;
        this._turnStack = origTurnStack;
        this._toolById = origToolById;
        this._stickyToolId = origStickyToolId;
        this._persistSessionId = origPersistId;
        this._replaying = origReplaying;

        // 将 temp 容器的内容插入到 logEl 顶部（emptyState 和 hint 之后）
        const hint = origLog.querySelector('.cc-load-more-hint');
        const insertRef = hint ? hint.nextSibling : origLog.firstChild;
        while (tempContainer.firstChild) {
            origLog.insertBefore(tempContainer.firstChild, insertRef);
        }
        this._mergeConsecutiveAssistantRows();
    }

    /** 结束当前助手轮（用户发话、系统通知、prompt_done 后下一轮再新开头像行） */
    _finishAssistantTurn() {
        this._turnStack = null;
    }

    /** @returns {HTMLElement|null} */
    _currentAssistantMain() {
        if (this._turnStack?.parentElement?.classList?.contains('cc-asst-main')) {
            return this._turnStack.parentElement;
        }
        const rows = this.logEl.querySelectorAll(':scope > .cc-asst-row');
        const last = rows[rows.length - 1];
        return last?.querySelector('.cc-asst-main') || null;
    }

    /**
     * 格式化 prompt_done.usage（按模型分桶的 TokenUsage）。
     * @param {unknown} usage
     * @returns {{ text: string, hasData: boolean }}
     */
    _formatTurnUsage(usage) {
        if (!usage || typeof usage !== 'object' || Array.isArray(usage)) {
            return {text: '暂无 token 用量', hasData: false};
        }
        const entries = Object.entries(usage);
        if (!entries.length) {
            return {text: '暂无 token 用量', hasData: false};
        }
        const blocks = [];
        let hasData = false;
        for (const [model, raw] of entries) {
            const u = raw && typeof raw === 'object' ? raw : {};
            const input = Number(u.inputTokens || 0) || 0;
            const output = Number(u.outputTokens || 0) || 0;
            const cacheRead = Number(u.cacheReadTokens || 0) || 0;
            const cacheWrite = Number(u.cacheWriteTokens || 0) || 0;
            if (input || output || cacheRead || cacheWrite) hasData = true;
            const lines = [];
            const modelLabel = String(model || '').trim();
            if (modelLabel && modelLabel.toLowerCase() !== 'default') {
                lines.push(modelLabel);
            }
            lines.push(`输入  ${input.toLocaleString()}`);
            lines.push(`输出  ${output.toLocaleString()}`);
            if (cacheRead) lines.push(`缓存读  ${cacheRead.toLocaleString()}`);
            if (cacheWrite) lines.push(`缓存写  ${cacheWrite.toLocaleString()}`);
            blocks.push(lines.join('\n'));
        }
        return {text: blocks.join('\n\n'), hasData};
    }

    /**
     * 在本轮助手气泡右下角挂 info；hover 查看 token 消耗。
     * @param {unknown} usage
     */
    _attachTurnUsage(usage) {
        this._beginAssistantTurnIfNeeded();
        const main = this._currentAssistantMain();
        if (!main) return;
        let foot = main.querySelector(':scope > .cc-asst-foot');
        if (!foot) {
            foot = document.createElement('div');
            foot.className = 'cc-asst-foot';
            main.appendChild(foot);
        }
        let btn = foot.querySelector('.cc-turn-usage');
        if (!btn) {
            btn = document.createElement('button');
            btn.type = 'button';
            btn.className = 'cc-turn-usage';
            btn.innerHTML =
                '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true">' +
                '<circle cx="12" cy="12" r="10"/>' +
                '<line x1="12" y1="16" x2="12" y2="12"/>' +
                '<line x1="12" y1="8" x2="12.01" y2="8"/>' +
                '</svg>' +
                '<span class="cc-turn-usage-tip"></span>';
            foot.appendChild(btn);
        }
        const {text, hasData} = this._formatTurnUsage(usage);
        const tip = btn.querySelector('.cc-turn-usage-tip');
        if (tip) tip.textContent = text;
        btn.title = '';
        btn.setAttribute('aria-label', hasData ? '本轮 token 用量' : '本轮未上报 token 用量');
        btn.dataset.hasData = hasData ? '1' : '0';
    }

    /** 若无进行中的助手轮，新建一行头像 + 纵向栈 */
    _beginAssistantTurnIfNeeded() {
        if (this._turnStack) return;
        const row = document.createElement('div');
        row.className = 'cc-asst-row';
        const av = document.createElement('div');
        av.className = 'cc-avatar';
        av.innerHTML = AVATAR_SVG;
        av.setAttribute('aria-hidden', 'true');
        const main = document.createElement('div');
        main.className = 'cc-asst-main';
        const sr = document.createElement('span');
        sr.className = 'cc-sr-only';
        sr.textContent = '助手本轮回复';
        const stack = document.createElement('div');
        stack.className = 'cc-asst-stack';
        main.appendChild(sr);
        main.appendChild(stack);
        row.appendChild(av);
        row.appendChild(main);
        this.logEl.appendChild(row);
        this._turnStack = stack;
        this.hideEmptyState();
    }

    /** @param {string} sessionId */
    setPersistSessionId(sessionId) {
        const s = String(sessionId || '');
        if (s !== this._persistSessionId) {
            this._restoreAttempted = false;
        }
        this._persistSessionId = s;
    }

    /** 移除某会话在 IndexedDB 中的聊天快照（重启后旧 sessionId 不再保留） */
    clearPersistedLogForSession(sessionId) {
        const sid = String(sessionId || '');
        if (!sid) return;
        void deleteSnapshot(sid);
    }

    /** 持久化失败时在 #log 顶部插入警告横幅（每页签最多一次） */
    showPersistBannerOnce() {
        if (this._persistBannerShown) return;
        this._persistBannerShown = true;
        const banner = document.createElement('div');
        banner.className = 'cc-persist-banner';
        banner.setAttribute('role', 'status');
        banner.innerHTML =
            '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true">' +
            '<path d="M10.29 3.86L1.82 18a2 2 0 001.71 3h16.94a2 2 0 001.71-3L13.71 3.86a2 2 0 00-3.42 0z"/>' +
            '<line x1="12" y1="9" x2="12" y2="13"/><line x1="12" y1="17" x2="12.01" y2="17"/>' +
            '</svg>' +
            `<span>${PERSIST_BANNER_TEXT}</span>`;
        const hint = this.logEl.querySelector('.cc-load-more-hint');
        if (hint) {
            this.logEl.insertBefore(banner, hint.nextSibling);
        } else {
            this.logEl.prepend(banner);
        }
    }

    /**
     * 清空当前聊天 DOM 与流式状态（新 backend session / 重启后 connected 且非重复握手时调用）。
     * 保留或补全 #emptyState，便于后续重放或空会话展示。
     */
    clearConversationUi() {
        for (const t of this._mermaidTimers.values()) {
            clearTimeout(t);
        }
        this._mermaidTimers.clear();
        const emptyEl = this._emptyEl();
        this.logEl.innerHTML = '';
        if (emptyEl) {
            this.logEl.appendChild(emptyEl);
        } else {
            this._ensureEmptyStateNode();
        }
        this.endStream();
        this.resetToolsForNewTurn();
        this._finishAssistantTurn();
        this._totalTurns = 0;
        this._hasMoreTurns = false;
        this._nextBefore = null;
        this._pendingUserMessages = [];
        const empty = this._emptyEl();
        if (empty) empty.hidden = false;
        this.scrollToBottom();
    }

    /**
     * WebSocket `connected` 且 sessionId 与本地保存一致时恢复 #log。
     * @returns {Promise<boolean>} 是否恢复了非空记录
     */
    async restorePersistedIfSession(sessionId) {
        const sid = String(sessionId || '');
        if (!sid || this._restoreAttempted) return false;
        this._restoreAttempted = true;
        try {
            const data = await getSnapshot(sid);
            if (!data || typeof data.html !== 'string') return false;
            if (data.sessionId != null && String(data.sessionId) !== sid) return false;
            const clean = sanitizeChatLogHtml(data.html);
            if (!clean.trim()) return false;
            this.logEl.innerHTML = clean;
            for (const av of this.logEl.querySelectorAll('.cc-avatar')) av.innerHTML = AVATAR_SVG;
            for (const det of this.logEl.querySelectorAll('.cc-thinking[data-live]')) {
                this._settleThinking(det.querySelector('.cc-thinking-body'), null);
            }
            this._flushMermaid(this.logEl);
            this._ensureEmptyStateNode();
            this._mergeConsecutiveAssistantRows();
            this.endStream();
            this.resetToolsForNewTurn();
            this._finishAssistantTurn();
            const empty = this._emptyEl();
            const hasRows = this.logEl.querySelector(
                '.cc-user-row, .cc-asst-row, .cc-tool-wrap, .cc-tool-group, .cc-notice, .cc-error-line'
            );
            if (empty) empty.hidden = !!hasRows;
            this.scrollToBottom();
            this.flushPersist();
            return !!hasRows;
        } catch (e) {
            console.warn('acp-bridge: 恢复聊天记录失败', e);
            return false;
        }
    }

    /** 净化或旧数据可能导致 #emptyState 丢失，补回以免后续逻辑报错 */
    _ensureEmptyStateNode() {
        if (this._emptyEl()) return;
        const wrap = document.createElement('div');
        wrap.className = 'cc-empty';
        wrap.innerHTML =
            `<div class="cc-empty-mark" aria-hidden="true">${AVATAR_SVG}</div><h2 class="cc-empty-title">AgentChat</h2><p class="cc-empty-desc">聊天记录已恢复</p>`;
        this.logEl.appendChild(wrap);
    }

    /**
     * 合并应同属一轮的 .cc-asst-row（同轮只显示一个助手头像）。
     * 中间若仅有「会话就绪 / 已加载命令」类 cc-notice 则跳过并继续合并；含「轮次结束」则不跨过去。
     */
    _mergeConsecutiveAssistantRows() {
        const parent = this.logEl;
        let row = parent.firstElementChild;
        while (row) {
            if (!row.classList?.contains('cc-asst-row')) {
                row = row.nextElementSibling;
                continue;
            }
            let after = row.nextElementSibling;
            while (after && this._isSkippableGapBetweenAssistantRows(after)) {
                after = after.nextElementSibling;
            }
            if (after?.classList?.contains('cc-asst-row')) {
                const stackA = row.querySelector('.cc-asst-stack');
                const stackB = after.querySelector('.cc-asst-stack');
                if (stackA && stackB) {
                    while (stackB.firstChild) {
                        stackA.appendChild(stackB.firstChild);
                    }
                    /* 保留较新一轮的 usage 角标 */
                    const mainA = row.querySelector('.cc-asst-main');
                    const footB = after.querySelector('.cc-asst-foot');
                    if (mainA && footB) {
                        mainA.querySelector(':scope > .cc-asst-foot')?.remove();
                        mainA.appendChild(footB);
                    }
                    after.remove();
                    continue;
                }
            }
            row = row.nextElementSibling;
        }
    }

    _isSkippableGapBetweenAssistantRows(el) {
        if (!el || el.classList?.contains('cc-user-row') || el.classList?.contains('cc-asst-row')) {
            return false;
        }
        if (el.classList?.contains('cc-empty')) return false;
        if (el.tagName !== 'DIV') return false;
        if (el.children.length !== 1) return false;
        const inner = el.children[0];
        if (inner.classList?.contains('cc-error-line')) return true;
        if (!inner.classList?.contains('cc-notice')) return false;
        const text = (inner.textContent || '').trim();
        return !text.includes('轮次结束');
    }

    _schedulePersist() {
        if (!this._persistSessionId) return;
        if (this._persistTimer != null) clearTimeout(this._persistTimer);
        this._persistTimer = setTimeout(() => this._flushPersist(), 200);
    }

    async _flushPersist() {
        this._persistTimer = null;
        if (!this._persistSessionId || this._persistDisabled) return;
        const sid = this._persistSessionId;
        const html = this.logEl.innerHTML;
        const ok = await putSnapshot(sid, html);
        if (!ok) {
            this._persistDisabled = true;
            this.showPersistBannerOnce();
        }
    }

    /** 刷新/关页前立即落盘（best-effort async） */
    flushPersist() {
        if (this._persistTimer != null) {
            clearTimeout(this._persistTimer);
            this._persistTimer = null;
        }
        void this._flushPersist();
    }

    /**
     * 从后端事件日志重放恢复聊天界面（替代 localStorage 恢复）。
     * 按 prompt_begin / prompt_done 边界将事件分成轮次，确保 Q→A→Q→A 交替。
     * @param {Array<object>} events  后端 eventLog（每条为 handleEvent 可处理的原始 JSON）
     * @param {Array<object>} userTimeline  后端 userTimeline（含 done/running/queued 条目）
     */
    replayEventLog(events, _userTimeline) {
        if (!Array.isArray(events) || events.length === 0) return;
        // 清空当前聊天区域
        const emptyEl = this._emptyEl();
        this.logEl.innerHTML = '';
        if (emptyEl) this.logEl.appendChild(emptyEl);
        this.endStream();
        this.resetToolsForNewTurn();
        this._finishAssistantTurn();
        this._restoreAttempted = true;
        this._replaying = true;

        // 抑制重放期间的 localStorage 写入
        const origPersistId = this._persistSessionId;
        this._persistSessionId = '';

        // 将事件按轮次分组：每个 prompt_begin 开始一个新轮次
        const turns = [];
        let currentTurn = null;
        for (const ev of events) {
            let parsed;
            try {
                parsed = typeof ev === 'string' ? JSON.parse(ev) : ev;
            } catch {
                continue;
            }
            if (parsed && parsed.type === 'prompt_begin') {
                // 新轮次开始
                currentTurn = {userText: '', imageURLs: [], events: [ev]};
                currentTurn.userText = String(
                    parsed.promptText != null ? parsed.promptText : parsed.text || ''
                ).trim();
                // 提取图片 data URL（后端在 prompt_begin 中附带）
                if (Array.isArray(parsed.imageURLs) && parsed.imageURLs.length > 0) {
                    currentTurn.imageURLs = parsed.imageURLs.filter((u) => typeof u === 'string' && u);
                }
                turns.push(currentTurn);
            } else if (currentTurn) {
                currentTurn.events.push(ev);
            } else {
                // prompt_begin 之前的事件（不应该出现，但兜底处理）
                if (!turns.length) {
                    turns.push({userText: '', events: []});
                    currentTurn = turns[0];
                }
                currentTurn?.events.push(ev);
            }
        }

        // 按轮次重放：先插入用户消息，再重放该轮次的助手事件
        for (const turn of turns) {
            // 插入用户消息（Q）—— 包含图片 URL（如有）
            if (turn.userText || (turn.imageURLs && turn.imageURLs.length > 0)) {
                this.appendUser(turn.userText, turn.imageURLs || []);
            }
            // 重放该轮次的事件（A）—— 跳过 prompt_begin（用户消息已手动插入）
            for (const ev of turn.events) {
                try {
                    let parsed;
                    try {
                        parsed = typeof ev === 'string' ? JSON.parse(ev) : ev;
                    } catch {
                        parsed = null;
                    }
                    // 跳过 prompt_begin，因为用户消息已通过 appendUser 插入
                    if (parsed && parsed.type === 'prompt_begin') continue;
                    this.handleEvent(ev);
                } catch (e) {
                    console.warn('acp-bridge: 重放事件失败', e, ev);
                }
            }
        }

        // 恢复持久化并落盘
        this._replaying = false;
        this._persistSessionId = origPersistId;
        this.endStream();
        this._finishAssistantTurn();
        this._mergeConsecutiveAssistantRows();
        this._ensureEmptyStateNode();
        const hasRows = this.logEl.querySelector(
            '.cc-user-row, .cc-asst-row, .cc-tool-wrap, .cc-tool-group, .cc-notice, .cc-error-line'
        );
        const empty = this._emptyEl();
        if (empty) empty.hidden = !!hasRows;
        this.scrollToBottom();
        this.flushPersist();
        void hydrateMermaid(this.logEl);
    }

    endStream() {
        const prevBody = this.stream.bodyEl;
        if (this.stream.kind === 'thought') this._settleThinking(prevBody, Date.now() - (this.stream.startedAt || 0));
        this.stream = {kind: null, bodyEl: null, mdBuf: ''};
        if (prevBody) this._flushMermaid(prevBody);
    }

    /**
     * @param {HTMLElement|null|undefined} body
     * @param {number|null} ms 未知时长（如从快照恢复）传 null
     */
    _settleThinking(body, ms) {
        const det = body?.closest('.cc-thinking');
        if (!det || !det.hasAttribute('data-live')) return;
        det.removeAttribute('data-live');
        const sum = det.querySelector(':scope > summary');
        if (sum) sum.textContent = ms == null ? '思考过程' : thoughtDoneLabel(ms);
    }

    /** @param {HTMLElement|null|undefined} el */
    _scheduleMermaid(el) {
        if (!el) return;
        const prev = this._mermaidTimers.get(el);
        if (prev != null) clearTimeout(prev);
        const t = setTimeout(() => {
            this._mermaidTimers.delete(el);
            void hydrateMermaid(el);
        }, 200);
        this._mermaidTimers.set(el, t);
    }

    /** @param {HTMLElement|null|undefined} el */
    _flushMermaid(el) {
        if (!el) return;
        const prev = this._mermaidTimers.get(el);
        if (prev != null) {
            clearTimeout(prev);
            this._mermaidTimers.delete(el);
        }
        void hydrateMermaid(el);
    }

    resetToolsForNewTurn() {
        this._toolById.clear();
        this._stickyToolId = null;
    }

    /** 轮次结束：折叠所有工具卡，避免与下轮混在一起 */
    _collapseAllTools() {
        for (const e of this._toolById.values()) {
            if (e && e.det) e.det.open = false;
        }
    }

    /** 新工具卡插在「当前流式助手正文」之前，避免工具永远出现在长文后面 */
    _placeNewToolWrap(wrap) {
        if (
            this.stream.kind === 'agent' &&
            this.stream.bodyEl &&
            this._turnStack &&
            this._turnStack.contains(this.stream.bodyEl)
        ) {
            this._turnStack.insertBefore(wrap, this.stream.bodyEl);
            return;
        }
        this._turnStack.appendChild(wrap);
    }

    scrollToBottom() {
        this.scrollRoot.scrollTop = this.scrollRoot.scrollHeight;
    }

    /** 本会话 #log 内的空态节点（多 Tab 时各自一份，不能按全局 id 查找） */
    _emptyEl() {
        return this.logEl.querySelector(':scope > .cc-empty');
    }

    hideEmptyState() {
        const el = this._emptyEl();
        if (el) el.hidden = true;
    }

    appendStreamAgent(chunk) {
        if (this.stream.kind === 'agent' && this.stream.bodyEl) {
            this.stream.mdBuf = (this.stream.mdBuf || '') + chunk;
            this.stream.bodyEl.innerHTML = renderMarkdown(this.stream.mdBuf);
            this._scheduleMermaid(this.stream.bodyEl);
            this.scrollToBottom();
            this._schedulePersist();
            return;
        }
        this.endStream();
        this._beginAssistantTurnIfNeeded();
        const body = document.createElement('div');
        body.className = 'cc-asst-body cc-md';
        const mdBuf = chunk;
        body.innerHTML = renderMarkdown(mdBuf);
        this._scheduleMermaid(body);
        this._turnStack.appendChild(body);
        this.stream = {kind: 'agent', bodyEl: body, mdBuf};
        this.hideEmptyState();
        this.scrollToBottom();
        this._schedulePersist();
    }

    appendStreamThought(chunk) {
        if (this.stream.kind === 'thought' && this.stream.bodyEl) {
            this.stream.mdBuf = (this.stream.mdBuf || '') + chunk;
            this.stream.bodyEl.innerHTML = renderMarkdown(this.stream.mdBuf);
            this._scheduleMermaid(this.stream.bodyEl);
            this.scrollToBottom();
            this._schedulePersist();
            return;
        }
        this.endStream();
        this._beginAssistantTurnIfNeeded();
        const det = document.createElement('details');
        det.className = 'cc-thinking';
        det.setAttribute('data-live', '');
        det.open = false;
        const sum = document.createElement('summary');
        sum.textContent = THINKING_LIVE_LABEL;
        const body = document.createElement('div');
        body.className = 'cc-thinking-body cc-md';
        const mdBuf = chunk;
        body.innerHTML = renderMarkdown(mdBuf);
        this._scheduleMermaid(body);
        det.appendChild(sum);
        det.appendChild(body);
        this._turnStack.appendChild(det);
        this.stream = {kind: 'thought', bodyEl: body, mdBuf, startedAt: Date.now()};
        this.hideEmptyState();
        this.scrollToBottom();
        this._schedulePersist();
    }

    /** @returns {string} */
    _lastUserBubbleText() {
        const rows = this.logEl.querySelectorAll('.cc-user-bubble');
        if (!rows.length) return '';
        return String(rows[rows.length - 1].textContent || '').trim();
    }

    /**
     * 从 #log 末尾向前找最后一个「会话块」：user / agent / 无。
     * agent 含助手行与工具卡（均属一轮助手侧），跳过纯 cc-notice / cc-error-line 包装。
     * @returns {'user'|'agent'|'none'}
     */
    _lastClosingBlockType() {
        const kids = [...this.logEl.children].filter(
            (el) => el.id !== 'emptyState' && !el.classList.contains('cc-empty')
        );
        for (let i = kids.length - 1; i >= 0; i--) {
            const el = kids[i];
            if (el.classList.contains('cc-user-row')) return 'user';
            if (
                el.classList.contains('cc-asst-row') ||
                el.classList.contains('cc-tool-wrap') ||
                el.classList.contains('cc-tool-group')
            ) {
                return 'agent';
            }
            if (el.children.length === 1) {
                const inner = el.firstElementChild;
                if (!(inner?.classList.contains('cc-notice') || inner?.classList.contains('cc-error-line'))) {
                    // 非可跳过的元素，继续循环尝试下一个
                }
            }
        }
        return 'none';
    }

    /** 主区末尾已是 user 时再插 user 前，先补一条最简助手块，保证 user → agent → user 卡片交替 */
    _appendMinimalAssistantPlaceholder() {
        this.endStream();
        this._beginAssistantTurnIfNeeded();
        const body = document.createElement('div');
        body.className = 'cc-asst-body cc-md cc-placeholder-reply';
        body.setAttribute('role', 'note');
        body.textContent = '（上一轮无可见助手输出）';
        this._turnStack.appendChild(body);
        this._finishAssistantTurn();
        this.hideEmptyState();
        this.scrollToBottom();
        this._schedulePersist();
    }

    _ensureAssistantBetweenUsersIfNeeded() {
        if (!this._replaying) return;
        if (this._lastClosingBlockType() !== 'user') return;
        this._appendMinimalAssistantPlaceholder();
    }

    _createUserRow(text, imageURLs = []) {
        const row = document.createElement('div');
        row.className = 'cc-user-row';
        const b = document.createElement('div');
        b.className = 'cc-user-bubble';
        /* 图片附件 */
        if (imageURLs.length > 0) {
            const imgWrap = document.createElement('div');
            imgWrap.className = 'cc-user-images';
            for (const url of imageURLs) {
                // Only the rebuilt string from sanitizeImageURL may reach img.src
                // (never assign the original url — CodeQL #1/#3 taint cut).
                const safeSrc = sanitizeImageURL(url);
                if (safeSrc === '') continue;
                const img = document.createElement('img');
                img.src = safeSrc;
                img.className = 'cc-user-img';
                img.alt = '附件图片';
                img.loading = 'lazy';
                imgWrap.appendChild(img);
            }
            b.appendChild(imgWrap);
        }
        /* 文本内容 */
        if (text) {
            const span = document.createElement('span');
            span.textContent = text;
            b.appendChild(span);
        }
        row.appendChild(b);
        return row;
    }

    /** @param {Array<{ phase?: string, content?: string, text?: string }>} entries */
    applyUserTimelineFromServer(entries) {
        if (!Array.isArray(entries) || entries.length === 0) return;
        const done = entries.filter((e) => e && e.phase === 'done');
        const running = entries.find((e) => e && e.phase === 'running');
        /* 仅 queued：勿剥主 log（避免 LS 里已有成对块被清空） */
        if (done.length === 0 && !running) return;
        /* querySelectorAll 用 :scope > 限定只匹配 logEl 直接子节点；matches() 不支持 :scope，用简单选择器 */
        const agentQSel = ':scope > .cc-asst-row, :scope > .cc-tool-wrap, :scope > .cc-tool-group';
        const agentMatch = '.cc-asst-row, .cc-tool-wrap, .cc-tool-group';
        for (const el of [...this.logEl.querySelectorAll(':scope > .cc-user-row')]) {
            el.remove();
        }
        let di = 0;
        for (const el of [...this.logEl.children]) {
            if (el.id === 'emptyState' || el.classList.contains('cc-empty')) continue;
            if (!el.matches(agentMatch)) continue;
            while (di < done.length) {
                const e = done[di];
                const t = String(e.content ?? e.text ?? '').trim();
                di++;
                if (!t) continue;
                this.logEl.insertBefore(this._createUserRow(t), el);
                break;
            }
        }
        const leftover = [];
        while (di < done.length) {
            const e = done[di++];
            const t = String(e.content ?? e.text ?? '').trim();
            if (t) leftover.push(t);
        }
        const firstAgent = this.logEl.querySelector(agentQSel);  /* agentQSel 含 :scope > 前缀，只匹配直接子节点 */
        if (firstAgent) {
            for (let i = leftover.length - 1; i >= 0; i--) {
                this.logEl.insertBefore(this._createUserRow(leftover[i]), firstAgent);
            }
        } else {
            for (const t of leftover) {
                this.logEl.appendChild(this._createUserRow(t));
            }
        }
        if (running) {
            const rt = String(running.content ?? running.text ?? '').trim();
            if (rt) {
                /* running 对应的 agent 输出是最后一组 agent 块；user 行应插在它之前，而非末尾 */
                /* agentQSel 含 :scope > 前缀，只匹配 logEl 直接子节点，不会误选嵌套的 cc-tool-wrap/cc-tool-group */
                const agentBlocks = [...this.logEl.querySelectorAll(agentQSel)];
                /* 找到尚未被 done 用户消息“认领”的最后一个 agent 块 */
                /* 往前找到这组连续 agent 块的第一个（同一轮次可能有多个顶层 agent 块） */
                /* agentMatch 不含 :scope 前缀，用于 Element.matches() */
                let insertBefore = agentBlocks.length > 0 ? agentBlocks[agentBlocks.length - 1] : null;
                if (insertBefore) {
                    let prev = insertBefore.previousElementSibling;
                    while (
                        prev &&
                        !prev.classList.contains('cc-user-row') &&
                        prev.id !== 'emptyState' &&
                        !prev.classList.contains('cc-empty') &&
                        (prev.matches(agentMatch) || this._isSkippableGapBetweenAssistantRows(prev))
                        ) {
                        insertBefore = prev;
                        prev = prev.previousElementSibling;
                    }
                    this.logEl.insertBefore(this._createUserRow(rt), insertBefore);
                } else {
                    this.logEl.appendChild(this._createUserRow(rt));
                }
                this._finishAssistantTurn();
                this.endStream();
                this.resetToolsForNewTurn();
            }
        }
        this.endStream();
        this.hideEmptyState();
        this.scrollToBottom();
        this.flushPersist();
    }

    appendUser(text, imageURLs = []) {
        this._ensureAssistantBetweenUsersIfNeeded();
        this._finishAssistantTurn();
        this.endStream();
        this.resetToolsForNewTurn();
        this.logEl.appendChild(this._createUserRow(text, imageURLs));
        this.hideEmptyState();
        this.scrollToBottom();
        this._schedulePersist();
    }

    /**
     * 用户点击发送时调用：将消息入队，等 prompt_begin 到达时再显示气泡。
     * 保证多条消息连续发送时仍然是 Q A Q A 顺序。
     */
    enqueueUserMessage(text, imageURLs = []) {
        this._pendingUserMessages.push({ text, imageURLs });
    }

    /**
     * 元信息挂在当前助手轮内部，不调用 _finishAssistantTurn（避免同轮出现多个 C 头像行）。
     * @param {'notice'|'error'} variant
     * @param {string} text
     */
    appendMetaInAssistantTurn(variant, text) {
        const msg = String(text || '').trim();
        if (!msg) return;
        this._beginAssistantTurnIfNeeded();
        const div = document.createElement('div');
        div.className =
            variant === 'error' ? 'cc-asst-meta cc-asst-meta--error' : 'cc-asst-meta';
        div.textContent = msg;
        div.setAttribute('role', 'status');
        this._turnStack.appendChild(div);
        this.hideEmptyState();
        this.scrollToBottom();
        this._schedulePersist();
    }

    appendNotice(variant, text) {
        this._finishAssistantTurn();
        this.endStream();
        const wrap = document.createElement('div');
        const el =
            variant === 'error'
                ? (() => {
                    const d = document.createElement('div');
                    d.className = 'cc-error-line';
                    d.textContent = text;
                    return d;
                })()
                : (() => {
                    const d = document.createElement('div');
                    d.className = 'cc-notice';
                    d.textContent = text;
                    return d;
                })();
        wrap.appendChild(el);
        this.logEl.appendChild(wrap);
        this.hideEmptyState();
        this.scrollToBottom();
        this._schedulePersist();
    }

    /**
     * @param {string} title
     * @param {string} bodyText
     * @param {boolean} [open]
     * @param {string} [wrapClass] 追加在 cc-tool-wrap 上，如 cc-tool-plan
     */
    appendToolCard(title, bodyText, open = true, wrapClass = '') {
        /* 同轮次内可能在助手正文后插工具卡，勿 endStream，否则下一段 agent 会新开一条 C 气泡 */
        const wrap = document.createElement('div');
        wrap.className = wrapClass ? `cc-tool-wrap ${wrapClass}` : 'cc-tool-wrap';
        const box = document.createElement('div');
        box.className = 'cc-tool';
        const det = document.createElement('details');
        if (open) det.open = true;
        const sum = document.createElement('summary');
        /* 这类卡片之后不会再收到状态更新，用中性标记而不是转圈 */
        const icon = document.createElement('span');
        icon.className = 'cc-tool-status cc-tool-status--info';
        icon.textContent = '◆';
        const t = document.createElement('span');
        t.className = 'cc-tool-title';
        t.textContent = humanizeToolTitle(title);
        sum.appendChild(icon);
        sum.appendChild(t);
        const body = document.createElement('div');
        body.className = 'cc-tool-body cc-md';
        body.innerHTML = renderMarkdown(bodyText || '');
        det.appendChild(sum);
        det.appendChild(body);
        box.appendChild(det);
        wrap.appendChild(box);
        this._beginAssistantTurnIfNeeded();
        this._turnStack.appendChild(wrap);
        this._flushMermaid(body);
        this.hideEmptyState();
        this.scrollToBottom();
        this._schedulePersist();
    }

    _resolveToolId(upd) {
        let id =
            upd.toolCallId ||
            upd.toolCall?.toolCallId ||
            upd.tool?.toolCallId ||
            upd.call?.toolCallId ||
            upd.id ||
            upd.callId;
        if (id) {
            this._stickyToolId = String(id);
            return String(id);
        }
        if (this._stickyToolId) return this._stickyToolId;
        id = `anon-${++this._anonToolSeq}`;
        this._stickyToolId = id;
        return id;
    }

    _renderToolBody(entry) {
        const chain = (entry.statusChain || []).join(' → ');
        const extra = entry.extraBody || '';
        entry.bodyEl.textContent = '';
        entry.bodyEl.className = 'cc-tool-body';
        if (chain) {
            const prog = document.createElement('div');
            prog.className = 'cc-tool-progress';
            prog.textContent = `进度：${chain}`;
            entry.bodyEl.appendChild(prog);
        }
        /* ToolCallCard：extras.args → <details><summary>Arguments</summary><pre> + truncateMiddle */
        if (entry.paramsRaw != null) {
            const argStr = String(formatToolContent(entry.paramsRaw)).trim();
            if (argStr) {
                const det = document.createElement('details');
                det.className = 'cc-tool-args-details';
                const sum = document.createElement('summary');
                sum.className = 'cc-tool-args-summary';
                sum.textContent = 'Arguments';
                const pre = document.createElement('pre');
                pre.className = 'cc-tool-args-pre';
                pre.textContent = truncateMiddle(argStr, 1000, 1000).text;
                det.appendChild(sum);
                det.appendChild(pre);
                det.open = false;
                entry.bodyEl.appendChild(det);
            }
        }
        /* ToolResultCard；ACP 流式输出默认展开 Result，避免多一层点击 */
        if (extra) {
            const rd = document.createElement('details');
            rd.className = 'cc-tool-result-details';
            rd.open = true;
            const rs = document.createElement('summary');
            rs.className = 'cc-tool-result-summary';
            rs.textContent = 'Result';
            const md = document.createElement('div');
            md.className = 'cc-md cc-tool-result-md';
            md.innerHTML = renderMarkdown(extra);
            rd.appendChild(rs);
            rd.appendChild(md);
            entry.bodyEl.appendChild(rd);
            this._flushMermaid(md);
        }
    }

    /**
     * 同一 toolCallId（或粘性 id）合并为一张卡；状态只追加到「进度」链，不各起一条通知。
     */
    _mergeToolCall(rawUpd) {
        const upd = flattenToolPayload(rawUpd);
        const id = this._resolveToolId(upd);
        let entry = this._toolById.get(id);
        if (!entry) {
            const group = document.createElement('div');
            group.className = 'cc-tool-group';
            group.setAttribute('data-card-type', CardType.ToolCall);
            const wrap = document.createElement('div');
            wrap.className = 'cc-tool-wrap';
            const box = document.createElement('div');
            box.className = 'cc-tool';
            const det = document.createElement('details');
            det.open = false;
            const sum = document.createElement('summary');
            const icon = document.createElement('span');
            const titleSpan = document.createElement('span');
            titleSpan.className = 'cc-tool-title';
            sum.appendChild(icon);
            sum.appendChild(titleSpan);
            const body = document.createElement('div');
            body.className = 'cc-tool-body';
            det.appendChild(sum);
            det.appendChild(body);
            box.appendChild(det);
            wrap.appendChild(box);
            group.appendChild(wrap);
            this._beginAssistantTurnIfNeeded();
            this._placeNewToolWrap(group);
            entry = {
                wrap,
                det,
                icon,
                titleSpan,
                bodyEl: body,
                statusChain: [],
                extraBody: '',
                /** @type {unknown|null} 工具入参（与 content 流式输出分开存） */
                paramsRaw: null,
            };
            this._toolById.set(id, entry);
            this.hideEmptyState();
        }

        const title =
            upd.title ||
            upd.name ||
            upd.toolName ||
            (upd.kind && String(upd.kind)) ||
            entry.titleSpan.textContent ||
            '工具调用';
        if (title) entry.titleSpan.textContent = humanizeToolTitle(title);

        const st = String(upd.status || upd.state || 'pending').toLowerCase().replace(/-/g, '_');
        const last = entry.statusChain[entry.statusChain.length - 1];
        if (st && st !== last) {
            entry.statusChain.push(st);
        }

        setToolStatus(entry.icon, st);

        /*
         * ACP 工具事件分两类：
         *   tool_call        → pending/in_progress，rawInput 可能含参数
         *   tool_call_update → completed，rawOutput.content 含结果
         * 只在非 update 事件中提取参数，避免把结果误认为参数。
         */
        const isResultEvent = /update/i.test(upd.sessionUpdate || rawUpd.sessionUpdate || '');
        if (!isResultEvent) {
            let paramsSnap = collectToolParams(upd);
            if (paramsSnap == null && rawUpd !== upd) {
                paramsSnap = collectToolParams(rawUpd);
            }
            if (paramsSnap != null) {
                entry.paramsRaw = pickRicherParams(entry.paramsRaw, paramsSnap);
            }
        }

        const chunk = formatToolContent(upd.content);
        if (chunk) {
            /* 结果事件的 content 直接追加到 extraBody（Result 区域） */
            if (isResultEvent) {
                entry.extraBody = entry.extraBody ? `${entry.extraBody}\n${chunk}` : chunk;
            } else {
                const paramsStr = entry.paramsRaw != null ? formatToolContent(entry.paramsRaw) : '';
                /* 避免 arguments 既进了 params 又在 content 里整段重复 */
                if (!paramsStr || chunk.trim() !== paramsStr.trim()) {
                    entry.extraBody = entry.extraBody ? `${entry.extraBody}\n${chunk}` : chunk;
                }
            }
        }

        this._renderToolBody(entry);

        if (st === 'completed' || st === 'complete' || st === 'failed' || st === 'error') {
            entry.det.open = false;
            this._stickyToolId = null;
        }

        this.scrollToBottom();
        this._schedulePersist();
    }

    handleEvent(data) {
        let u;
        try {
            u = typeof data === 'string' ? JSON.parse(data) : data;
        } catch {
            this.appendNotice('notice', String(data));
            return;
        }
        if (u.type !== 'session_update') {
            if (u.type === 'prompt_begin') {
                /*
                 * 与后端每轮 session/prompt 对齐。
                 * 有用户文本时始终结束上一助手轮并显示用户气泡。
                 * 实时模式：从待处理队列中取出消息（含图片），保证 Q A Q A 顺序。
                 * 若无用户文本（Agent 自动继续 / 工具调用后续），保持同一轮次。
                 */
                this.endStream();
                this.resetToolsForNewTurn();
                const full = String(u.promptText != null ? u.promptText : u.text || '').trim();
                if (full) {
                    this._finishAssistantTurn();
                    const pending = this._pendingUserMessages.shift();
                    const imgs = pending ? pending.imageURLs : [];
                    this.appendUser(pending ? pending.text : full, imgs);
                }
                this.flushPersist();
                return;
            }
            if (u.type === 'prompt_done') {
                this.endStream();
                this._collapseAllTools();
                this._attachTurnUsage(u.usage);
                const srNorm = String(u.stopReason || '')
                    .toLowerCase()
                    .replace(/-/g, '_');
                /* end_turn / 用户 Stop 为预期结束，不必刷屏 */
                if (
                    srNorm &&
                    srNorm !== 'end_turn' &&
                    srNorm !== 'cancelled' &&
                    srNorm !== 'canceled'
                ) {
                    this.appendNotice('notice', `轮次结束 · ${u.stopReason || ''}`);
                }
                /*
                 * 不在此处调用 _finishAssistantTurn()：
                 * Agent 可能在 prompt_done 后紧接 prompt_begin（无用户消息）继续输出，
                 * 提前结束轮次会导致后续内容出现在新的 C 头像行下。
                 * 轮次会在下一个 appendUser / 有用户消息的 prompt_begin 时自然关闭。
                 */
                this.flushPersist();
            } else if (u.type === 'fs_write') {
                this.appendToolCard('write_file', u.path || '', false);
            } else if (u.type === 'terminal_create') {
                this.appendToolCard(
                    'terminal',
                    `${u.command || ''} ${(u.args || []).join(' ')}`.trim(),
                    false
                );
            } else if (u.type === 'error_text') {
                /* 错误正文（模型不存在 / 鉴权失败等）走兜底分支只会留下一张空卡片，必须原样展示。 */
                this.appendMetaInAssistantTurn('error', String(u.text || '') || '事件 · error_text');
            } else {
                this.appendMetaInAssistantTurn('notice', `事件 · ${u.type || 'unknown'}`);
            }
            return;
        }

        let upd;
        try {
            upd = typeof u.update === 'string' ? JSON.parse(u.update) : u.update;
        } catch {
            upd = u.update;
        }
        if (!upd || typeof upd !== 'object') return;
        const merged = mergeSessionUpdateEnvelope(upd);
        const flat = flattenToolPayload(merged);
        const kind = normalizeSessionUpdateKind(merged);
        console.debug('acp-bridge: session_update kind=', kind, 'merged=', merged);
        dispatchSessionUpdate({chatView: this, kind, flat, merged});
    }
}
