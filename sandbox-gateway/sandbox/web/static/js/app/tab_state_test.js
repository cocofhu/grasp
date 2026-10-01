import assert from 'node:assert/strict';
import {test} from 'node:test';

import {
    DEFAULT_CHAT_ID,
    modelDisplayName,
    modelPickerItems,
    neighborAfterClose,
    pickActiveId,
    reconcileChats,
    tabLabel,
} from './tab_state.js';

test('tabLabel prefers title, then default name, then position', () => {
    assert.equal(tabLabel({id: 'c_1', title: '  重构  '}, 3), '重构');
    assert.equal(tabLabel({id: DEFAULT_CHAT_ID}, 0), '默认');
    assert.equal(tabLabel({id: 'c_2', title: ''}, 2), '会话 3');
});

test('reconcileChats follows the server list', () => {
    const {added, removed} = reconcileChats(
        ['default', 'c_old', 'c_keep'],
        [{id: 'default'}, {id: 'c_keep'}, {id: 'c_new'}]
    );
    assert.deepEqual(added.map((c) => c.id), ['c_new']);
    assert.deepEqual(removed, ['c_old']);

    const empty = reconcileChats([], null);
    assert.deepEqual(empty, {added: [], removed: []});
});

test('pickActiveId restores the saved tab when it still exists', () => {
    assert.equal(pickActiveId('c_2', ['default', 'c_2']), 'c_2');
    assert.equal(pickActiveId('c_gone', ['default', 'c_2']), 'default');
    assert.equal(pickActiveId(null, ['c_1', 'c_2']), 'c_1');
    assert.equal(pickActiveId(null, []), '');
});

test('neighborAfterClose picks the right neighbour, then the left one', () => {
    assert.equal(neighborAfterClose(['default', 'a', 'b'], 'a'), 'b');
    assert.equal(neighborAfterClose(['default', 'a', 'b'], 'b'), 'a');
    assert.equal(neighborAfterClose(['default'], 'x'), 'default');
});

test('modelDisplayName maps auto and catalog names', () => {
    const models = [{id: 'gpt-5', name: 'GPT-5'}];
    assert.equal(modelDisplayName('', models), 'Auto');
    assert.equal(modelDisplayName('auto', models), 'Auto');
    assert.equal(modelDisplayName('gpt-5', models), 'GPT-5');
    assert.equal(modelDisplayName('other', models), 'other');
});

test('modelPickerItems leads with a follow-default entry', () => {
    const models = [{id: 'gpt-5', name: 'GPT-5'}, {id: 'sonnet'}];
    const items = modelPickerItems(models, '');
    assert.equal(items[0].id, '');
    assert.equal(items[0].name, '默认（Auto）');
    assert.equal(items[0].followDefault, true);
    assert.equal(items[2].name, 'sonnet');
    assert.equal(modelPickerItems(models, 'gpt-5')[0].name, '默认（GPT-5）');
});
