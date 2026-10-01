import {apiPath} from '../core/paths.js';
import {deleteOtherSnapshots} from '../ui/chat_persist_db.js';
import {ChatTab} from './chat_tab.js';
import {
    ACTIVE_TAB_STORAGE_KEY,
    DEFAULT_CHAT_ID,
    neighborAfterClose,
    pickActiveId,
    reconcileChats,
} from './tab_state.js';

/** 其它浏览器窗口新建 / 关闭的 Tab 隔多久同步一次 */
const SYNC_INTERVAL_MS = 15000;

/**
 * 管理会话 Tab：从 /api/chats 加载、新建 / 关闭 / 重命名 / 切换，并渲染 Tab 条。
 * 后台 Tab 的 WebSocket 保持连接，Tab 条上实时显示忙碌与待授权状态。
 */
export class TabManager {
    /**
     * @param {{
     *   tabListEl: HTMLElement,
     *   newTabBtn: HTMLButtonElement|null,
     *   panesEl: HTMLElement,
     *   template: HTMLTemplateElement,
     *   onActiveChange: (tab: ChatTab, prev: ChatTab|null) => void,
     *   onTabChange: (tab: ChatTab) => void,
     *   getModelName: (tab: ChatTab) => string,
     * }} opts
     */
    constructor(opts) {
        this.opts = opts;
        /** @type {ChatTab[]} */
        this.tabs = [];
        /** @type {ChatTab|null} */
        this.active = null;
        this.maxChats = 0;
        this.defaultModel = '';
        this._syncTimer = null;
        this._busyOp = false;
        // 每次本地增删改 Tab 自增；sync 拿到的列表若早于这些操作则丢弃，避免误删刚建的 Tab。
        this._opGen = 0;

        opts.newTabBtn?.addEventListener('click', () => {
            void this.createTab();
        });
    }

    /** 首次加载：拉会话列表、建 Tab、恢复上次激活的 Tab，并连接全部 Tab。 */
    async init() {
        const list = await this._fetchChats();
        const chats = list?.chats?.length ? list.chats : [{id: DEFAULT_CHAT_ID}];
        for (const info of chats) this._addTab(info);
        let saved = null;
        try {
            saved = localStorage.getItem(ACTIVE_TAB_STORAGE_KEY);
        } catch (_) {
        }
        this.activate(pickActiveId(saved, this.tabs.map((t) => t.id)));
        for (const t of this.tabs) t.connect();
        this._syncTimer = setInterval(() => void this.sync(), SYNC_INTERVAL_MS);
        document.addEventListener('visibilitychange', () => {
            if (document.visibilityState === 'visible') void this.sync();
        });
    }

    /** 与服务端列表对齐（其它窗口新建 / 关闭的 Tab）。 */
    async sync() {
        if (this._busyOp) return;
        const gen = this._opGen;
        const list = await this._fetchChats();
        if (!list?.chats || this._busyOp || gen !== this._opGen) return;
        const {added, removed} = reconcileChats(this.tabs.map((t) => t.id), list.chats);
        for (const id of removed) this._removeTab(id);
        for (const info of added) this._addTab(info).connect();
        for (const info of list.chats) {
            const t = this.get(info.id);
            if (!t) continue;
            t.title = info.title || '';
            if (!t.switchingModel) t.selectedModel = info.model || '';
        }
        if (added.length || removed.length) this._renumber();
        this.render();
    }

    /** @returns {Promise<{ chats: { id: string, title?: string, model?: string }[], max?: number, defaultModel?: string }|null>} */
    async _fetchChats() {
        try {
            const res = await fetch(apiPath('api/chats'), {credentials: 'include', headers: {Accept: 'application/json'}});
            if (!res.ok) {
                console.warn('acp-bridge: /api/chats 响应异常', res.status);
                return null;
            }
            const data = await res.json();
            this.maxChats = Number(data.max) || 0;
            this.defaultModel = String(data.defaultModel || '');
            return data;
        } catch (e) {
            console.warn('acp-bridge: 加载会话列表失败', e);
            return null;
        }
    }

    /** @param {string} id */
    get(id) {
        return this.tabs.find((t) => t.id === id) || null;
    }

    /** @param {{ id: string, title?: string, model?: string }} info */
    _addTab(info) {
        const tab = new ChatTab({
            info,
            index: this.tabs.length,
            template: this.opts.template,
            panesEl: this.opts.panesEl,
            onChange: (t) => {
                this.render();
                this.opts.onTabChange(t);
            },
            onSessionId: () => this._pruneSnapshots(),
        });
        this.tabs.push(tab);
        return tab;
    }

    /** @param {string} id */
    _removeTab(id) {
        const tab = this.get(id);
        if (!tab) return;
        const next = this.active === tab ? neighborAfterClose(this.tabs.map((t) => t.id), id) : '';
        tab.destroy();
        this.tabs = this.tabs.filter((t) => t !== tab);
        if (next) this.activate(next);
    }

    _renumber() {
        this.tabs.forEach((t, i) => {
            t.index = i;
        });
    }

    /** 全部 Tab 都拿到 sessionId 后，删掉不属于任何 Tab 的本地快照。 */
    _pruneSnapshots() {
        const ids = this.tabs.map((t) => t.persistKey);
        if (ids.some((s) => !s)) return;
        void deleteOtherSnapshots(ids);
    }

    /** @param {string} id */
    activate(id) {
        const tab = this.get(id);
        if (!tab) return;
        const prev = this.active;
        this.active = tab;
        for (const t of this.tabs) t.setActive(t === tab);
        try {
            localStorage.setItem(ACTIVE_TAB_STORAGE_KEY, tab.id);
        } catch (_) {
        }
        this.render();
        if (prev !== tab) this.opts.onActiveChange(tab, prev);
    }

    async createTab() {
        if (this._busyOp) return;
        this._busyOp = true;
        this._opGen++;
        try {
            const res = await fetch(apiPath('api/chats'), {
                method: 'POST',
                credentials: 'include',
                headers: {'Content-Type': 'application/json', Accept: 'application/json'},
                body: JSON.stringify({}),
            });
            if (res.status === 409) {
                alert(`会话数已达上限（${this.maxChats || '?'} 个），请先关闭不用的 Tab。`);
                return;
            }
            if (!res.ok) throw new Error(await res.text());
            const info = await res.json();
            const tab = this._addTab(info);
            this.activate(tab.id);
            tab.connect();
        } catch (e) {
            console.error('acp-bridge: 新建会话失败', e);
            alert('新建会话失败：' + (e.message || String(e)));
        } finally {
            this._busyOp = false;
            this.render();
        }
    }

    /** @param {string} id */
    async closeTab(id) {
        const tab = this.get(id);
        if (!tab || id === DEFAULT_CHAT_ID || this._busyOp) return;
        if (tab.busy && !confirm(`「${tab.label}」正在运行，关闭将结束该会话的 Agent。确定关闭？`)) return;
        this._busyOp = true;
        this._opGen++;
        try {
            const res = await fetch(apiPath(`api/chats/${encodeURIComponent(id)}`), {
                method: 'DELETE',
                credentials: 'include',
                headers: {Accept: 'application/json'},
            });
            if (!res.ok && res.status !== 404) throw new Error(await res.text());
            this._removeTab(id);
            this._renumber();
            this._pruneSnapshots();
        } catch (e) {
            console.error('acp-bridge: 关闭会话失败', e);
            alert('关闭会话失败：' + (e.message || String(e)));
        } finally {
            this._busyOp = false;
            this.render();
        }
    }

    /** @param {string} id */
    async renameTab(id) {
        const tab = this.get(id);
        if (!tab) return;
        const next = prompt('会话名称（留空恢复默认）', tab.title || tab.label);
        if (next == null) return;
        try {
            const res = await fetch(apiPath(`api/chats/${encodeURIComponent(id)}`), {
                method: 'PATCH',
                credentials: 'include',
                headers: {'Content-Type': 'application/json', Accept: 'application/json'},
                body: JSON.stringify({title: next}),
            });
            if (!res.ok) throw new Error(await res.text());
            const info = await res.json();
            this._opGen++;
            tab.title = info.title || '';
            this.render();
            this.opts.onTabChange(tab);
        } catch (e) {
            alert('重命名失败：' + (e.message || String(e)));
        }
    }

    flushAll() {
        for (const t of this.tabs) t.flushPersist();
    }

    /**
     * 增量更新 Tab 条：每个 Tab 的元素只创建一次（重建会打断双击重命名等手势）。
     */
    render() {
        const list = this.opts.tabListEl;
        if (!this._tabEls) this._tabEls = new Map();
        const els = this._tabEls;
        for (const [id, el] of els) {
            if (!this.get(id)) {
                el.remove();
                els.delete(id);
            }
        }
        this.tabs.forEach((tab, i) => {
            let el = els.get(tab.id);
            if (!el) {
                el = this._createTabEl(tab.id);
                els.set(tab.id, el);
            }
            if (list.children[i] !== el) list.insertBefore(el, list.children[i] || null);

            const active = tab === this.active;
            el.setAttribute('aria-selected', active ? 'true' : 'false');
            el.setAttribute('aria-controls', tab.pane.id);
            el.tabIndex = active ? 0 : -1;
            el.classList.toggle('cc-tab--live', tab.sessionLive);
            el.classList.toggle('cc-tab--busy', tab.busy);
            el.classList.toggle('cc-tab--perm', tab.permissionPending);
            el.title = `${tab.label} · ${tab.statusText}（双击重命名）`;
            el.querySelector('.cc-tab-title').textContent = tab.label;
            const model = el.querySelector('.cc-tab-model');
            model.hidden = !tab.selectedModel;
            model.textContent = tab.selectedModel ? this.opts.getModelName(tab) : '';
            const close = el.querySelector('.cc-tab-close');
            if (close) close.setAttribute('aria-label', `关闭 ${tab.label}`);
        });
        const btn = this.opts.newTabBtn;
        if (btn) btn.disabled = this._busyOp || (this.maxChats > 0 && this.tabs.length >= this.maxChats);
    }

    /** @param {string} id */
    _createTabEl(id) {
        const el = document.createElement('div');
        el.className = 'cc-tab';
        el.setAttribute('role', 'tab');
        el.dataset.chatId = id;

        const dot = document.createElement('span');
        dot.className = 'cc-tab-dot';
        dot.setAttribute('aria-hidden', 'true');
        el.appendChild(dot);

        const title = document.createElement('span');
        title.className = 'cc-tab-title';
        el.appendChild(title);

        const model = document.createElement('span');
        model.className = 'cc-tab-model';
        el.appendChild(model);

        if (id !== DEFAULT_CHAT_ID) {
            const close = document.createElement('button');
            close.type = 'button';
            close.className = 'cc-tab-close';
            close.textContent = '×';
            close.title = '关闭会话';
            close.addEventListener('click', (e) => {
                e.stopPropagation();
                void this.closeTab(id);
            });
            close.addEventListener('dblclick', (e) => e.stopPropagation());
            el.appendChild(close);
        }

        el.addEventListener('click', () => this.activate(id));
        el.addEventListener('dblclick', () => void this.renameTab(id));
        el.addEventListener('auxclick', (e) => {
            if (e.button === 1) {
                e.preventDefault();
                void this.closeTab(id);
            }
        });
        el.addEventListener('keydown', (e) => {
            if (e.key !== 'ArrowRight' && e.key !== 'ArrowLeft') return;
            const i = this.tabs.findIndex((t) => t.id === id);
            if (i < 0) return;
            const j = (i + (e.key === 'ArrowRight' ? 1 : -1) + this.tabs.length) % this.tabs.length;
            const next = this.tabs[j];
            this.activate(next.id);
            this._tabEls.get(next.id)?.focus();
        });
        return el;
    }
}
