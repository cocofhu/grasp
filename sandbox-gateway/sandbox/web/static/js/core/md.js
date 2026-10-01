/**
 * Markdown → 安全 HTML（DOMPurify）。marked / DOMPurify 随包自带：入口模块的静态依赖
 * 一旦走外网，CDN 不通时整个页面都起不来。
 * ```mermaid 围栏经 marked 转为 `<pre class="mermaid">`，再由 hydrateMermaid 异步绘制成 SVG；
 * Mermaid 体积大，仍按需从 esm.sh 加载，加载失败时保留源码块。
 */
import {marked} from '../third_party/marked-15.0.6.esm.js';
import DOMPurify from '../third_party/dompurify-3.2.4.esm.js';

function escapeHtml(s) {
    return String(s)
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;');
}

marked.setOptions({
    gfm: true,
    breaks: true,
});

marked.use({
    renderer: {
        /** @param {{ text?: string, lang?: string, escaped?: boolean }} token */
        code(token) {
            const lang = String(token.lang || '')
                .trim()
                .toLowerCase()
                .replace(/^language-/, '');
            if (lang !== 'mermaid') {
                return false;
            }
            const raw = String(token.text ?? '');
            const body = token.escaped ? raw : escapeHtml(raw);
            /* 供 lightbox 用源码重绘：克隆 SVG 会与页内副本 id 冲突，Mermaid 注入的 #id 样式只命中第一个，放大层会「空白」 */
            const srcAttr = encodeURIComponent(raw);
            return `<div class="cc-mermaid" data-cc-mermaid-src="${srcAttr}"><pre class="mermaid">${body}</pre></div>`;
        },
    },
});

DOMPurify.addHook('afterSanitizeAttributes', (node) => {
    if (node.tagName === 'A' && node.hasAttribute('href')) {
        node.setAttribute('target', '_blank');
        node.setAttribute('rel', 'noopener noreferrer');
    }
});

/**
 * @param {string} src
 * @returns {string}
 */
export function renderMarkdown(src) {
    if (src == null || !String(src).trim()) return '';
    try {
        const html = marked.parse(String(src));
        return DOMPurify.sanitize(html, {
            USE_PROFILES: {html: true},
            ADD_ATTR: ['data-cc-mermaid-src'],
        });
    } catch (e) {
        console.warn('renderMarkdown', e);
        return `<p>${escapeHtml(String(src))}</p>`;
    }
}

/** @type {Promise<typeof import('https://esm.sh/mermaid@11.4.1')>|null} */
let mermaidModPromise = null;
/**
 * 当前已应用的 init 指纹（明暗 + 配置版本）。改 flowchart/security 等选项时须 bump CFG。
 */
let mermaidThemeApplied = '';

const MERMAID_CFG_FINGERPRINT = 'loose-svglabels-v1';

async function getMermaid() {
    if (!mermaidModPromise) {
        mermaidModPromise = import('https://esm.sh/mermaid@11.4.1').catch((e) => {
            /* 网络恢复后下一张图还能再试 */
            mermaidModPromise = null;
            throw e;
        });
    }
    const mod = await mermaidModPromise;
    return mod.default;
}

async function ensureMermaidTheme(mermaid) {
    const mode =
        document.documentElement.getAttribute('data-theme') === 'light' ? 'light' : 'dark';
    const stamp = `${mode}|${MERMAID_CFG_FINGERPRINT}`;
    if (stamp === mermaidThemeApplied) return;
    mermaid.initialize({
        startOnLoad: false,
        /* strict + iframe 与聊天区并发 render 易踩同一套 body 临时节点；foreignObject 标签在弹层里也常丢字 */
        securityLevel: 'loose',
        theme: 'base',
        themeVariables: buildMermaidThemeVariables(),
        suppressErrorRendering: true,
        flowchart: {
            curve: 'basis',
            padding: 12,
            /* SVG 文字节点，嵌入/放大时比 htmlLabels 稳定 */
            htmlLabels: false,
        },
    });
    mermaidThemeApplied = stamp;
}

/**
 * 从页面 CSS 变量生成 Mermaid themeVariables。
 * 注意：flowchart 节点填充色来自 primaryColor，不能用 accent 整块铺满，否则高对比刺眼。
 */
function buildMermaidThemeVariables() {
    const s = getComputedStyle(document.documentElement);
    const v = (name, fb) => (s.getPropertyValue(name).trim() || fb);
    const ff =
        s.getPropertyValue('font-family').trim() ||
        'Inter, "PingFang SC", "Hiragino Sans GB", "Microsoft YaHei", system-ui, sans-serif';
    const light = document.documentElement.getAttribute('data-theme') === 'light';

    const accent = v('--accent', light ? '#0969da' : '#58a6ff');
    const border = v('--border-color', light ? '#d0d7de' : '#30363d');
    const text = v('--text-primary', light ? '#1f2328' : '#e6edf3');
    const textMuted = v('--text-secondary', light ? '#656d76' : '#8b949e');

    const base = {
        fontFamily: ff,
        border1: border,
        border2: accent,
        textColor: text,
        titleColor: text,
        nodeTextColor: text,
        nodeBorder: border,
        signalColor: accent,
        loopTextColor: textMuted,
        noteTextColor: text,
        noteBorderColor: border,
        cScale0: accent,
        cScale1: v('--success', light ? '#1a7f37' : '#3fb950'),
        cScale2: v('--warning', light ? '#9a6700' : '#d29922'),
        cScale3: v('--danger', light ? '#cf222e' : '#f85149'),
        /* 圆角节点，略柔和 */
        primaryBorderRadius: light ? 6 : 10,
    };

    if (light) {
        return {
            ...base,
            darkMode: false,
            background: v('--bg-primary', '#ffffff'),
            mainBkg: v('--code-bg', '#f6f8fa'),
            secondBkg: v('--bg-secondary', '#f6f8fa'),
            tertiaryColor: v('--bg-tertiary', '#eaeef2'),
            primaryColor: '#ffffff',
            primaryTextColor: text,
            primaryBorderColor: border,
            secondaryColor: v('--bg-secondary', '#f6f8fa'),
            secondaryTextColor: text,
            secondaryBorderColor: border,
            tertiaryTextColor: textMuted,
            lineColor: textMuted,
            clusterBkg: v('--bg-secondary', '#f6f8fa'),
            clusterBorder: border,
            edgeLabelBackground: v('--bg-tertiary', '#eaeef2'),
            actorBkg: v('--code-bg', '#f6f8fa'),
            actorBorder: border,
            actorTextColor: text,
            labelBackground: v('--bg-tertiary', '#eaeef2'),
            labelTextColor: text,
            noteBkgColor: v('--code-bg', '#f6f8fa'),
        };
    }

    /* 深色：画布略浅于页面底、节点用层级灰 + 浅色字；连线中性灰，避免「死黑 + 死白」 */
    const canvas = v('--bg-secondary', '#161b22');
    const nodeA = v('--bg-tertiary', '#21262d');
    const nodeB = v('--code-bg', '#1c2128');
    const lineSoft = v('--text-tertiary', '#6e6e78');

    return {
        ...base,
        darkMode: true,
        background: canvas,
        mainBkg: nodeB,
        secondBkg: v('--bg-primary', '#0d1117'),
        tertiaryColor: nodeA,
        primaryColor: nodeA,
        primaryTextColor: text,
        primaryBorderColor: border,
        secondaryColor: nodeB,
        secondaryTextColor: text,
        secondaryBorderColor: v('--border-strong', '#36363e'),
        tertiaryTextColor: textMuted,
        lineColor: lineSoft,
        clusterBkg: v('--bg-primary', '#0d1117'),
        clusterBorder: border,
        edgeLabelBackground: nodeA,
        actorBkg: nodeA,
        actorBorder: border,
        actorTextColor: text,
        labelBackground: nodeA,
        labelTextColor: text,
        noteBkgColor: nodeB,
    };
}

/**
 * 为已渲染的 `.cc-mermaid`（内含 svg）加上可点击放大提示与键盘可达性。
 * @param {ParentNode|null|undefined} root
 */
export function markMermaidInteractive(root) {
    if (!root || typeof root.querySelectorAll !== 'function') return;
    for (const el of root.querySelectorAll('.cc-mermaid')) {
        if (!el.querySelector('svg')) continue;
        el.classList.add('cc-mermaid--interactive');
        el.tabIndex = 0;
        el.setAttribute('role', 'button');
        el.setAttribute('aria-label', '放大查看图表');
    }
}

/**
 * 将容器内未绘制的 `<pre class="mermaid">` 交给 Mermaid 渲染（流式片段可能语法不完整，错误仅打日志）。
 * @param {ParentNode|null|undefined} root
 * @param {{ markInteractive?: boolean }} [opts]
 * @returns {Promise<void>}
 */
export async function hydrateMermaid(root, opts = {}) {
    const {markInteractive = true} = opts;
    if (!root || typeof root.querySelectorAll !== 'function') return;
    const nodes = root.querySelectorAll('pre.mermaid');
    try {
        if (nodes.length) {
            const mermaid = await getMermaid();
            await ensureMermaidTheme(mermaid);
            await mermaid.run({
                nodes: [...nodes],
                suppressErrors: true,
            });
        }
    } catch (e) {
        console.warn('acp-bridge: mermaid', e);
    }
    if (markInteractive) {
        markMermaidInteractive(root);
    }
}

/**
 * 从聊天区 `.cc-mermaid` 取 Mermaid 源码（属性优先；无属性时读尚未被替换的 pre）。
 * @param {HTMLElement} wrapperSource
 * @returns {string}
 */
function getMermaidSourceFromWrapper(wrapperSource) {
    const enc = wrapperSource.getAttribute('data-cc-mermaid-src');
    if (enc != null && enc !== '') {
        try {
            return decodeURIComponent(enc);
        } catch {
            /* fallthrough */
        }
    }
    const pre = wrapperSource.querySelector('pre.mermaid');
    const t = pre && String(pre.textContent || '').trim();
    return t || '';
}

/**
 * 放大层：`render(id, text, holder)` 使用挂到 body 的离屏容器，避免与聊天区 `run()` 争用同一批临时 DOM。
 * @param {HTMLElement} mountEl
 * @param {HTMLElement} wrapperSource
 * @returns {Promise<boolean>} 是否已成功出图（false 表示无源码，由调用方 clone 回退）
 */
export async function renderMermaidInLightboxMount(mountEl, wrapperSource) {
    if (!mountEl || !wrapperSource) return false;
    const src = getMermaidSourceFromWrapper(wrapperSource).trim();
    if (!src) return false;

    mountEl.textContent = '';
    const mermaid = await getMermaid();
    await ensureMermaidTheme(mermaid);
    const id = `cc-mb-${Date.now()}-${Math.random().toString(36).slice(2, 11)}`;

    const holder = document.createElement('div');
    holder.setAttribute('data-cc-mermaid-temp', '');
    holder.setAttribute('aria-hidden', 'true');
    holder.style.cssText =
        'position:fixed;left:-9999px;top:0;width:2400px;height:2400px;overflow:visible;visibility:hidden;pointer-events:none;margin:0;padding:0;border:0;';

    document.body.appendChild(holder);
    try {
        const {svg, bindFunctions} = await mermaid.render(id, src, holder);
        if (!svg || !String(svg).trim()) {
            throw new Error('mermaid returned empty svg');
        }
        const wrap = document.createElement('div');
        wrap.className = 'cc-mermaid';
        wrap.innerHTML = svg;
        /* 检查渲染出的 SVG 是否有实际内容（viewBox 非零、有子元素） */
        const svgEl = wrap.querySelector('svg');
        if (svgEl) {
            const vb = svgEl.getAttribute('viewBox') || '';
            const parts = vb.split(/[\s,]+/).map(Number);
            /* viewBox 宽高都为 0 说明渲染失败 */
            if (parts.length >= 4 && parts[2] === 0 && parts[3] === 0) {
                console.warn('acp-bridge: mermaid lightbox rendered 0-size svg, falling back to clone');
                return false;
            }
            /* 没有任何可见子元素也视为失败 */
            if (!svgEl.querySelector('g, path, rect, circle, text, line, polygon, polyline, ellipse, foreignObject')) {
                console.warn('acp-bridge: mermaid lightbox rendered empty svg, falling back to clone');
                return false;
            }
        }
        mountEl.appendChild(wrap);
        if (typeof bindFunctions === 'function') {
            bindFunctions(wrap);
        }
        return true;
    } catch (e) {
        console.warn('acp-bridge: mermaid lightbox render error, falling back to clone', e);
        /* 返回 false 让调用方走克隆路径，而非显示错误提示 */
        return false;
    } finally {
        holder.remove();
    }
}

/**
 * 从本地快照（IndexedDB）恢复 #log 时再过一遍净化（防篡改存储 XSS）。
 * @param {string} html
 * @returns {string}
 */
export function sanitizeChatLogHtml(html) {
    if (html == null || !String(html).trim()) return '';
    const rawLen = String(html).trim().length;
    try {
        const out = DOMPurify.sanitize(String(html), {
            /* svg：Mermaid 落盘后的聊天记录含 SVG，需保留以便刷新恢复 */
            USE_PROFILES: {html: true, svg: true, svgFilters: true},
            ADD_ATTR: [
                'id',
                'aria-hidden',
                'open',
                'role',
                'aria-modal',
                'aria-labelledby',
                'data-cc-mermaid-src',
            ],
        });
        if (rawLen > 80 && !String(out).trim()) {
            console.warn(
                'acp-bridge: 聊天记录净化后为空，若刷新后空白请检查本地快照是否被篡改或含不支持的标签'
            );
        }
        return out;
    } catch (e) {
        console.warn('sanitizeChatLogHtml', e);
        return '';
    }
}
