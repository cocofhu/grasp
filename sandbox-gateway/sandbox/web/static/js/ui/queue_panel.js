const MAX_PREVIEW = 200;

/**
 * 是否有进行中的轮次或尚未消费的排队项（与面板是否展开的判断一致，供顶栏停止按钮等复用）。
 * @param {Record<string, unknown>|null|undefined} m queue_state / connected 内嵌的队列字段
 * @returns {boolean}
 */
export function queueStateHasPendingWork(m) {
    if (!m || typeof m !== 'object') return false;
    const entries = Array.isArray(m.queue_entries) ? m.queue_entries : [];
    if (entries.length > 0) return true;
    if (m.busy === true) return true;
    const run = m.running && typeof m.running === 'object' ? m.running : null;
    if (run == null) return false;
    const runningText = String(run.content ?? run.text ?? '').trim();
    const runningOp = String(run.id ?? run.opId ?? '').trim();
    return !!(runningText || runningOp);
}

/**
 * 待发送队列面板；含服务端 `running`（当前正 session/prompt 的一条），避免「只有排队、没有进行中」时面板空白。
 * @param {HTMLElement|null} panelEl <details id="queuePanel">
 * @returns {(m: { busy?: boolean, queue_entries?: { id?: string, action?: string, content?: string, text?: string, opId?: string }[], running?: { id?: string, action?: string, content?: string, text?: string, opId?: string }, queue_length?: number, queue_capacity?: number }) => void}
 */
export function createQueuePanelUpdater(panelEl) {
    if (!panelEl) return () => {
    };
    const bodyEl = panelEl.querySelector('.cc-queue-body');
    const sumTextEl = panelEl.querySelector('.cc-queue-summary-text');
    return function onQueueState(m) {
        const entries = Array.isArray(m.queue_entries) ? m.queue_entries : [];
        const rows = entries.map((e) => ({
            text: String(e?.content ?? e?.text ?? ''),
            opId: String(e?.id ?? e?.opId ?? ''),
            action: (String(e?.action ?? 'chat').trim() || 'chat'),
            imageCount: Number(e?.imageCount ?? 0),
        }));
        const n = rows.length;
        const cap =
            typeof m.queue_capacity === 'number' && m.queue_capacity > 0
                ? m.queue_capacity
                : null;

        const busy = m.busy === true;
        const run = m.running && typeof m.running === 'object' ? m.running : null;
        const runningText =
            run != null
                ? String(run.content ?? run.text ?? '').trim()
                : '';
        const runningOp =
            run != null
                ? String(run.id ?? run.opId ?? '').trim()
                : '';
        const hasRunning = !!(runningText || runningOp);
        const hasWork = queueStateHasPendingWork(m);
        const runningAction =
            run != null
                ? (String(run.action ?? 'chat').trim() || 'chat')
                : 'chat';
        const runningImageCount =
            run != null ? Number(run.imageCount ?? 0) : 0;

        if (!hasWork) {
            panelEl.hidden = true;
            return;
        }
        panelEl.hidden = false;
        panelEl.dataset.busy = String(busy || hasRunning);
        panelEl.open = true;
        if (sumTextEl) {
            const parts = [];
            if (hasRunning) parts.push('1 条进行中');
            else if (busy) parts.push('处理中…');
            if (n > 0) parts.push(cap != null ? `${n}/${cap} 条排队` : `${n} 条排队`);
            sumTextEl.textContent = parts.join(' · ');
        }
        if (!bodyEl) return;
        bodyEl.textContent = '';

        /** 创建一行队列行 DOM（复用逻辑，避免重复代码） */
        function createQueueRow(className, ringClass, mainText, mainTitle) {
            const line = document.createElement('div');
            line.className = className;
            const ring = document.createElement('span');
            ring.className = ringClass;
            ring.setAttribute('aria-hidden', 'true');
            const main = document.createElement('div');
            main.className = 'cc-queue-row-main';
            main.textContent = mainText;
            if (mainTitle) main.title = mainTitle;
            line.appendChild(ring);
            line.appendChild(main);
            return {line, ring, main};
        }

        if (busy && !hasRunning) {
            const {line} = createQueueRow(
                'cc-queue-row cc-queue-row--running',
                'cc-queue-ring cc-queue-ring--spin',
                '正在处理…'
            );
            bodyEl.appendChild(line);
        }

        if (hasRunning) {
            const oneLine = runningText.replace(/\s+/g, ' ').trim();
            const short =
                oneLine.length > MAX_PREVIEW ? `${oneLine.slice(0, MAX_PREVIEW)}…` : oneLine;
            const prefix = runningImageCount > 0 ? `${runningImageCount} 张图片 · ` : '';
            const {line, main} = createQueueRow(
                'cc-queue-row cc-queue-row--running',
                'cc-queue-ring cc-queue-ring--spin',
                short ? `正在回复：${prefix}${short}` : (prefix ? `正在回复：${prefix.trim()}` : '正在回复…'),
                runningText || runningOp
            );
            const tag = document.createElement('span');
            tag.className = 'cc-queue-row-tag';
            tag.textContent = runningOp
                ? `${runningAction} · ${runningOp.slice(0, 8)}`
                : runningAction;
            line.appendChild(tag);
            bodyEl.appendChild(line);
        }

        rows.forEach((row, i) => {
            const text = typeof row === 'string' ? String(row ?? '') : String(row.text ?? '');
            const opId = typeof row === 'string' ? '' : row.opId || '';
            const act =
                typeof row === 'string'
                    ? 'chat'
                    : String(row.action ?? 'chat').trim() || 'chat';
            const imgCnt = typeof row === 'string' ? 0 : Number(row.imageCount ?? 0);
            const line = document.createElement('div');
            line.className = 'cc-queue-row';
            const ring = document.createElement('span');
            ring.className = 'cc-queue-ring';
            ring.setAttribute('aria-hidden', 'true');
            const idx = document.createElement('span');
            idx.className = 'cc-queue-idx';
            idx.textContent = String(i + 1);
            ring.appendChild(idx);
            const main = document.createElement('div');
            main.className = 'cc-queue-row-main';
            const oneLine = text.replace(/\s+/g, ' ').trim();
            const short =
                oneLine.length > MAX_PREVIEW ? `${oneLine.slice(0, MAX_PREVIEW)}…` : oneLine;
            const imgPrefix = imgCnt > 0 ? `${imgCnt} 张图片 · ` : '';
            main.textContent = imgPrefix + (short || (imgCnt > 0 ? '' : '（空）'));
            main.title = text;
            const tag = document.createElement('span');
            tag.className = 'cc-queue-row-tag';
            tag.textContent = opId ? `${act} · ${opId.slice(0, 8)}` : act;
            line.appendChild(ring);
            line.appendChild(main);
            line.appendChild(tag);
            bodyEl.appendChild(line);
        });
    };
}
