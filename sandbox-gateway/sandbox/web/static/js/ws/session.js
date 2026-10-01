import {wsURL} from '../core/paths.js';
import {ChatView} from '../ui/chat_view.js';

/**
 * 与 Agent 会话就绪（顶栏「已连接」）时显示绿色状态点
 * @param {HTMLElement|null} statusEl #status
 * @param {boolean} live
 */
export function setHeaderConnLive(statusEl, live) {
    const wrap = statusEl?.closest?.('.cc-header-status-wrap');
    if (wrap) wrap.classList.toggle('cc-conn-live', live);
}

export class BrowserSession {
    /**
     * @param {{
     *   chatId?: string,
     *   chat: ChatView,
     *   onStatus: (text: string, live: boolean) => void,
     *   getCwd: () => string,
     *   getAutoPerm: () => boolean,
     *   getLabel?: () => string,
     *   onConnected?: (connected: boolean) => void,
     *   onSessionId?: (sessionId: string) => void,
     *   onRestartAvailable?: (available: boolean) => void,
     *   onQueueState?: (m: { busy?: boolean, queue_entries?: { text?: string, opId?: string }[] }) => void,
     *   onModelUpdate?: (model: { id: string, name: string } | null, currentModel: string) => void,
     *   onPermission?: (pending: boolean) => void
     * }} opts
     */
    constructor(opts) {
        this.opts = opts;
        /** @type {WebSocket|null} */
        this.ws = null;
        /** ACP session/new 完成前禁止发 chat */
        this.panelReady = false;
        /** 超时主动 close 时避免 onclose 覆盖已写的超时文案 */
        this._suppressCloseStatus = false;
        /** close() 后不再更新状态 */
        this._closed = false;
        /** 相同错误短时间内只打一条聊天通知，避免刷屏 */
        this._lastErrSig = '';
        this._lastErrAt = 0;
        /** 服务端可能先推 connected 再收到客户端 connect，避免重复「会话就绪」 */
        this._sessionId = '';
    }

    get sessionId() {
        return this._sessionId;
    }

    /** 当前会话在 IndexedDB 中的快照键（空表示尚未就绪）。 */
    get persistKey() {
        return this._persistKey(this._sessionId);
    }

    /**
     * 本地快照键：default 会话沿用 sessionId；其它 Tab 前缀 chatId——one-shot provider
     * 在首轮前对所有会话都报同一个占位 sessionId，不加前缀会串快照。
     * @param {string} sid
     */
    _persistKey(sid) {
        if (!sid) return '';
        return this.opts.chatId ? `${this.opts.chatId}/${sid}` : sid;
    }

    _status(text, live) {
        if (this._closed) return;
        this.opts.onStatus(text, live);
    }

    _setConn(connected) {
        if (this._closed) return;
        this.opts.onConnected?.(connected);
    }

    /** WebSocket 已打开即可请求服务端重启 Agent（无需等 panelReady，便于 Agent 崩溃后自救） */
    _syncRestartBtn() {
        if (this._closed) return;
        const ok = this.ws !== null && this.ws.readyState === WebSocket.OPEN;
        this.opts.onRestartAvailable?.(ok);
    }

    connect() {
        if (this.ws) {
            try {
                this.ws.close();
            } catch (_) {
            }
        }
        this._closed = false;
        this.panelReady = false;
        this._status('正在连接 WebSocket…', false);
        this._setConn(false);

        const url = wsURL(this.opts.chatId);
        const hangMs = 20000;
        let hangTimer = setTimeout(() => {
            hangTimer = null;
            if (!this.ws || this.ws.readyState !== WebSocket.CONNECTING) return;
            this._suppressCloseStatus = true;
            try {
                this.ws.close();
            } catch (_) {
            }
            this._status(
                '无法连接 WebSocket（超时）。请用浏览器打开运行 acp-bridge 的地址（例如 http://127.0.0.1:8765 ），不要双击本地 HTML；若走反向代理需转发 WebSocket 到与页面同前缀的 /ws。当前尝试: ' +
                url,
                false
            );
            this._setConn(false);
        }, hangMs);

        const clearHang = () => {
            if (hangTimer != null) {
                clearTimeout(hangTimer);
                hangTimer = null;
            }
        };

        const ws = new WebSocket(url);
        this.ws = ws;
        ws.onopen = () => {
            clearHang();
            this._status('正在与 Agent 握手…', false);
            const msg = {
                op: 'connect',
                cwd: this.opts.getCwd(),
                fsRoot: '',
                mcpServers: null,
                autoPermission: this.opts.getAutoPerm(),
            };
            ws.send(JSON.stringify(msg));
            this._syncRestartBtn();
        };
        ws.onmessage = (ev) => {
            if (this._closed) return;
            let m;
            try {
                m = JSON.parse(ev.data);
            } catch {
                return;
            }
            if (m.op === 'connected') {
                this._lastErrSig = '';
                this._lastErrAt = 0;
                const sid = m.sessionId || '';
                const prevSid = this._sessionId;
                const dup = this.panelReady && prevSid === sid;
                this._sessionId = sid;
                this.panelReady = true;
                this._status('已连接', true);
                this._setConn(true);
                this.opts.chat.setPersistSessionId(this._persistKey(sid));
                this.opts.onSessionId?.(sid);
                if (!dup) {
                    // 新 sessionId（含重启 Agent）：先清界面与旧会话本地快照，再按后端上下文恢复
                    if (prevSid && prevSid !== sid) {
                        this.opts.chat.clearPersistedLogForSession(this._persistKey(prevSid));
                    }
                    this.opts.chat.clearConversationUi();
                    const eventLog = Array.isArray(m.eventLog) ? m.eventLog : [];
                    void (async () => {
                        if (eventLog.length > 0) {
                            this.opts.chat.replayEventLog(eventLog, m.userTimeline || []);
                        } else {
                            await this.opts.chat.restorePersistedIfSession(this._persistKey(sid));
                            this.opts.chat.applyUserTimelineFromServer(m.userTimeline || []);
                        }
                        this.opts.chat.setHistoryPaging(m.totalTurns || 0, !!m.hasMoreTurns);
                    })();
                }
                this.opts.onModelUpdate?.(m.model || null, m.currentModel || '');
                this.opts.onQueueState?.(m);
            } else if (m.op === 'error') {
                const msg = m.message || '未知错误';
                const sig = `${m.agentExited ? 'e' : 'o'}:${msg}`;
                const now = Date.now();
                const dupChat = sig === this._lastErrSig && now - this._lastErrAt < 5000;
                if (!dupChat) {
                    this._lastErrSig = sig;
                    this._lastErrAt = now;
                }
                if (m.agentExited) {
                    this._sessionId = '';
                    this.panelReady = false;
                    this._status(`Agent 已退出 · ${msg}`, false);
                    this._setConn(false);
                    if (!dupChat) this.opts.chat.appendNotice('error', msg);
                } else if (!this.panelReady) {
                    this._status(`连接失败 · ${msg}`, false);
                    if (!dupChat) this.opts.chat.appendNotice('error', msg);
                } else {
                    this._status(`请求失败 · ${msg}`, false);
                    if (!dupChat) this.opts.chat.appendNotice('error', msg);
                }
            } else if (m.op === 'event') {
                this.opts.chat.handleEvent(m.data);
            } else if (m.op === 'permission_request') {
                this.showPermission(m);
            } else if (m.op === 'queue_state') {
                this.opts.onQueueState?.(m);
            }
            this._syncRestartBtn();
        };
        ws.onclose = (ev) => {
            clearHang();
            if (this.ws !== ws) return;
            this._sessionId = '';
            this.panelReady = false;
            if (this._suppressCloseStatus) {
                this._suppressCloseStatus = false;
                this._setConn(false);
                this._syncRestartBtn();
                return;
            }
            if (ev.code === 1006) {
                this._status('WebSocket 异常断开（多为代理未支持 WebSocket Upgrade，或网络中断）', false);
            } else if (ev.code !== 1000 && ev.code !== 1001) {
                this._status(`连接已断开（code ${ev.code}${ev.reason ? ' ' + ev.reason : ''}）`, false);
            } else {
                this._status('连接已断开', false);
            }
            this._setConn(false);
            this._syncRestartBtn();
        };
        ws.onerror = () => {
            clearHang();
            if (this.ws !== ws) return;
            this.panelReady = false;
            this._status('WebSocket 错误（无法建立连接）。请确认已启动 acp-bridge 且用 http(s) 访问同一主机，尝试地址: ' + url, false);
            this._setConn(false);
            this._syncRestartBtn();
        };
    }

    /** 主动断开（关闭 Tab）；之后不再回调状态。 */
    close() {
        this._closed = true;
        this.panelReady = false;
        const ws = this.ws;
        this.ws = null;
        if (ws) {
            try {
                ws.close(1000);
            } catch (_) {
            }
        }
    }

    /** @param {{ rpcId: string, params: any }} m */
    showPermission(m) {
        const overlay = document.getElementById('perm');
        const title = document.getElementById('permTitle');
        const text = document.getElementById('permText');
        const box = document.getElementById('permOpts');
        if (!overlay || !text || !box) return;
        const label = this.opts.getLabel?.() || '';
        if (title) title.textContent = label ? `权限请求 · ${label}` : '权限请求';
        overlay.classList.add('active');
        this.opts.onPermission?.(true);
        text.textContent = JSON.stringify(m.params, null, 2);
        box.innerHTML = '';
        const opts = (m.params && m.params.options) || [];
        opts.forEach((o) => {
            const b = document.createElement('button');
            b.type = 'button';
            b.className = 'cc-btn cc-btn-block';
            const kind = (o.kind || '').toLowerCase();
            if (kind.includes('reject') || kind.includes('deny')) {
                b.classList.add('cc-perm-reject');
            } else if (kind.includes('allow') || kind.includes('approve')) {
                b.classList.add('cc-perm-approve');
            } else {
                b.classList.add('cc-btn-outline');
            }
            b.textContent = o.name || o.optionId;
            b.onclick = () => {
                this.ws?.send(
                    JSON.stringify({op: 'permission', rpcId: String(m.rpcId), optionId: o.optionId})
                );
                overlay.classList.remove('active');
                this.opts.onPermission?.(false);
            };
            box.appendChild(b);
        });
    }

    sendChat(text, images = []) {
        if (!this.ws || this.ws.readyState !== WebSocket.OPEN) {
            throw new Error('请先连接 WebSocket');
        }
        if (!this.panelReady) {
            throw new Error('请先等待 Agent 握手完成（状态为「已连接」后再发消息）');
        }
        const opId =
            typeof crypto !== 'undefined' && crypto.randomUUID
                ? crypto.randomUUID()
                : `c-${Date.now()}-${Math.random().toString(36).slice(2, 11)}`;
        const msg = {op: 'chat', text, opId};
        if (images.length > 0) msg.images = images;
        this.ws.send(JSON.stringify(msg));
    }

    cancel() {
        if (this.ws && this.ws.readyState === WebSocket.OPEN) {
            this.ws.send(JSON.stringify({op: 'cancel'}));
        }
    }

    restartAgent() {
        if (!this.ws || this.ws.readyState !== WebSocket.OPEN) {
            throw new Error('WebSocket 未连接');
        }
        this._status('正在重启…', false);
        this.ws.send(JSON.stringify({op: 'restart_agent'}));
    }

    canSendChat() {
        return (
            this.panelReady &&
            this.ws !== null &&
            this.ws.readyState === WebSocket.OPEN
        );
    }
}
