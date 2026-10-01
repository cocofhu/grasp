import assert from 'node:assert/strict';
import {test} from 'node:test';

import {
    READY_MESSAGE,
    THEME_MESSAGE,
    announceReady,
    bindEmbedTheme,
    isEmbedded,
    parseThemeHash,
    parseThemeMessage,
} from './embed.js';

function fakeWindow({embedded = true, origin = 'https://grasp.test'} = {}) {
    const listeners = [];
    const posted = [];
    const parent = {postMessage: (msg, target) => posted.push({msg, target})};
    const win = {
        location: {origin},
        addEventListener: (type, fn) => type === 'message' && listeners.push(fn),
        dispatch: (e) => listeners.forEach((fn) => fn(e)),
        posted,
    };
    win.parent = embedded ? parent : win;
    return win;
}

test('parseThemeHash reads only light or dark', () => {
    assert.equal(parseThemeHash('#theme=light'), 'light');
    assert.equal(parseThemeHash('#foo=1&theme=dark'), 'dark');
    assert.equal(parseThemeHash('theme=dark'), 'dark');
    assert.equal(parseThemeHash('#theme=blue'), null);
    assert.equal(parseThemeHash(''), null);
    assert.equal(parseThemeHash(undefined), null);
});

test('parseThemeMessage accepts only the Grasp theme message', () => {
    assert.equal(parseThemeMessage({type: THEME_MESSAGE, theme: 'light'}), 'light');
    assert.equal(parseThemeMessage({type: THEME_MESSAGE, theme: 'neon'}), null);
    assert.equal(parseThemeMessage({type: 'other', theme: 'dark'}), null);
    assert.equal(parseThemeMessage('grasp-embed:theme'), null);
    assert.equal(parseThemeMessage(null), null);
});

test('bindEmbedTheme ignores other windows and origins', () => {
    const win = fakeWindow();
    const got = [];
    bindEmbedTheme(win, (t) => got.push(t));
    const data = {type: THEME_MESSAGE, theme: 'light'};
    win.dispatch({source: {}, origin: win.location.origin, data});
    win.dispatch({source: win.parent, origin: 'https://evil.test', data});
    assert.deepEqual(got, []);
    win.dispatch({source: win.parent, origin: win.location.origin, data});
    assert.deepEqual(got, ['light']);
});

test('announceReady posts to the same-origin parent only when embedded', () => {
    const top = fakeWindow({embedded: false});
    assert.equal(isEmbedded(top), false);
    announceReady(top);
    assert.deepEqual(top.posted, []);

    const framed = fakeWindow();
    assert.equal(isEmbedded(framed), true);
    announceReady(framed);
    assert.deepEqual(framed.posted, [{msg: {type: READY_MESSAGE}, target: 'https://grasp.test'}]);
});
