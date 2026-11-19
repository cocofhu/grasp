import {runtimeIcon} from './icons.js';

const commands = {
    new: {title: '新对话', description: '在当前工作区开启新上下文', icon: 'new-request',
        status: '新对话已开始', detail: '下一条消息从新的对话上下文开始。'},
    compact: {title: '压缩上下文', description: '带上近期对话继续', icon: 'layers',
        status: '上下文已压缩', detail: '下一条消息将带上近期对话摘录。'},
    skill: {title: '使用技能', description: '输入本轮使用的技能名称', icon: 'skills'},
};

export function contextCommand(value) {
    if (value === '/new' || /^\/new\s+chat$/.test(value)) return 'new';
    if (value === '/compact') return 'compact';
    return '';
}

export function readSkillCommand(value) {
    const match = /^\/skill\s+([^\s/\\]+)(?:\s+([\s\S]*))?$/.exec(value.trim());
    if (!match || match[1] === '.' || match[1] === '..') return null;
    return {name: match[1], task: match[2] || ''};
}

function commandIcon(name, className, size) {
    const icon = document.createElement('span');
    icon.className = className;
    icon.innerHTML = runtimeIcon(commands[name].icon, size);
    return icon;
}

export function contextCommandNotice(command) {
    const notice = document.createElement('div');
    notice.className = 'cc-context-notice cc-context-notice--' + command;
    notice.dataset.contextCommand = command;
    notice.setAttribute('role', 'status');
    const heading = document.createElement('div');
    heading.className = 'cc-context-heading';
    heading.append(commandIcon(command, 'cc-context-icon', 15), commands[command].status);
    const detail = document.createElement('p');
    detail.textContent = commands[command].detail;
    notice.append(heading, detail);
    return notice;
}

function optionButton(name) {
    const button = document.createElement('button');
    button.type = 'button';
    button.tabIndex = -1;
    button.setAttribute('role', 'option');
    const copy = document.createElement('span');
    copy.className = 'cc-command-copy';
    const title = document.createElement('strong');
    title.textContent = commands[name].title;
    const description = document.createElement('span');
    description.textContent = commands[name].description;
    copy.append(title, description);
    button.append(commandIcon(name, 'cc-command-icon', 17), copy);
    return button;
}

export function bindChatCommands(input, menu) {
    if (!menu) return () => false;
    let selected = 0;
    let options = [];

    function close() {
        menu.hidden = true;
        menu.replaceChildren();
        options = [];
        selected = 0;
        input.removeAttribute('aria-activedescendant');
        input.removeAttribute('aria-controls');
    }

    function select(index) {
        selected = index;
        options.forEach(({button}, i) => button.setAttribute('aria-selected', String(i === selected)));
        input.setAttribute('aria-activedescendant', options[selected].button.id);
    }

    function choose(value) {
        input.value = value;
        input.focus();
        input.setSelectionRange(value.length, value.length);
        input.dispatchEvent(new Event('input', {bubbles: true}));
        close();
    }

    function show() {
        close();
        const query = input.value.trimStart();
        if (input.disabled || document.activeElement !== input || !query.startsWith('/') || /[\r\n]/.test(query)) return;
        for (const name of Object.keys(commands)) {
            const value = '/' + name + (name === 'skill' ? ' ' : '');
            if (!value.startsWith(query)) continue;
            const index = options.length;
            const button = optionButton(name);
            button.id = menu.id + '-' + index;
            button.addEventListener('pointerenter', () => select(index));
            button.addEventListener('pointerdown', (event) => event.preventDefault());
            button.addEventListener('click', () => choose(value));
            options.push({button, value});
            menu.appendChild(button);
        }
        if (!options.length) return;
        menu.hidden = false;
        input.setAttribute('aria-controls', menu.id);
        select(0);
    }

    input.setAttribute('aria-autocomplete', 'list');
    input.addEventListener('focus', show);
    input.addEventListener('input', show);
    input.addEventListener('blur', close);
    return (event) => {
        if (menu.hidden || event.isComposing || event.keyCode === 229) return false;
        if (event.key === 'Escape') {
            close();
        } else if (event.ctrlKey || event.metaKey || event.shiftKey || event.altKey) {
            return false;
        } else if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
            const next = selected + (event.key === 'ArrowDown' ? 1 : -1);
            select((next + options.length) % options.length);
        } else if (event.key === 'Enter') {
            const {value} = options[selected];
            if (contextCommand(input.value.trim()) && value === input.value.trim()) return false;
            choose(value);
        } else {
            return false;
        }
        event.preventDefault();
        return true;
    };
}
