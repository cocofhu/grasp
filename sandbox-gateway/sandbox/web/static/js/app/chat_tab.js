import {ChatView} from '../ui/chat_view.js';
import {createQueuePanelUpdater, queueStateHasPendingWork} from '../ui/queue_panel.js';
import {BrowserSession} from '../ws/session.js';
import {DEFAULT_CHAT_ID, tabLabel} from './tab_state.js';

/**
 * 一个会话 Tab：自己的聊天面板、ChatView、WebSocket 与输入草稿。
 * 顶栏状态 / 模型 / 停止按钮由 main.js 按当前激活 Tab 渲染，Tab 只通过 onChange 通知变化。
 */
export class ChatTab {
    /**
     * @param {{
     *   info: { id: string, title?: string, model?: string },
     *   index: number,
     *   template: HTMLTemplateElement,
     *   panesEl: HTMLElement,
     *   onChange: (tab: ChatTab) => void,
     *   onSessionId?: (tab: ChatTab) => void,
     * }} opts
     */
    constructor(opts) {
        this.id = opts.info.id;
        this.title = opts.info.title || '';
        /** 本 Tab 显式选择的模型；空表示跟随默认 */
        this.selectedModel = opts.info.model || '';
        /** 连接后服务端报告的实际模型（'auto' 表示由 CLI 自选） */
        this.currentModel = '';
        this.index = opts.index;
        this._onChange = opts.onChange;

        this.statusText = '正在加载…';
        this.statusLive = false;
        this.sessionLive = false;
        this.restartAvailable = false;
        this.permissionPending = false;
        /** 正在 POST /api/model 切换模型 */
        this.switchingModel = false;
        /** 最近一次 connected / queue_state 中的队列字段 */
        this.lastQueue = null;

        /** 输入框草稿与待发送附件（切换 Tab 时由 main.js 换入换出） */
        this.draft = '';
        /** @type {{ file: File, dataURL?: string }[]} */
        this.pendingFiles = [];

        const frag = opts.template.content.cloneNode(true);
        this.pane = /** @type {HTMLElement} */ (frag.querySelector('.cc-chat-pane'));
        this.pane.id = `chatPane-${this.id}`;
        this.pane.hidden = true;
        this.logEl = /** @type {HTMLElement} */ (this.pane.querySelector('.cc-log'));
        const queuePanel = this.pane.querySelector('.cc-queue-panel');
        opts.panesEl.appendChild(this.pane);

        this.chat = new ChatView(this.logEl, this.pane, {chatId: this.wireChatId});
        const updateQueuePanel = createQueuePanelUpdater(/** @type {HTMLElement} */ (queuePanel));

        this.session = new BrowserSession({
            chatId: this.wireChatId,
            chat: this.chat,
            getCwd: () => '',
            getAutoPerm: () => true,
            getLabel: () => this.label,
            onStatus: (text, live) => {
                this.statusText = text;
                this.statusLive = live;
                this._changed();
            },
            onConnected: (connected) => {
                this.sessionLive = connected;
                if (!connected) this.lastQueue = null;
                this._changed();
            },
            onSessionId: () => opts.onSessionId?.(this),
            onRestartAvailable: (available) => {
                this.restartAvailable = available;
                this._changed();
            },
            onQueueState: (m) => {
                updateQueuePanel(m);
                this.lastQueue = m && typeof m === 'object' ? m : null;
                this._changed();
            },
            onModelUpdate: (_model, currentModel) => {
                if (currentModel) {
                    this.currentModel = currentModel;
                    this._changed();
                }
            },
            onPermission: (pending) => {
                this.permissionPending = pending;
                this._changed();
            },
        });
    }

    /** WS / API 上的 chat 参数；default 会话不带参数，与平台调用保持一致。 */
    get wireChatId() {
        return this.id === DEFAULT_CHAT_ID ? '' : this.id;
    }

    get label() {
        return tabLabel({id: this.id, title: this.title}, this.index);
    }

    get busy() {
        return this.sessionLive && queueStateHasPendingWork(this.lastQueue);
    }

    /** IndexedDB 快照键（含 chatId 前缀），未连上时为空 */
    get persistKey() {
        return this.session.persistKey;
    }

    _changed() {
        this._onChange(this);
    }

    connect() {
        try {
            this.session.connect();
        } catch (e) {
            console.error(e);
            this.statusText = '脚本异常，无法连接：' + (e && e.message ? e.message : String(e));
            this.statusLive = false;
            this._changed();
        }
    }

    /** @param {boolean} active */
    setActive(active) {
        this.pane.hidden = !active;
        if (active) this.chat.scrollToBottom();
    }

    flushPersist() {
        try {
            this.chat.flushPersist();
        } catch (_) {
        }
    }

    /** 关闭 Tab：断开 WS、删除本地快照并移除面板。 */
    destroy() {
        const key = this.session.persistKey;
        this.session.close();
        this.chat.setPersistSessionId('');
        if (key) this.chat.clearPersistedLogForSession(key);
        this.pane.remove();
    }
}
