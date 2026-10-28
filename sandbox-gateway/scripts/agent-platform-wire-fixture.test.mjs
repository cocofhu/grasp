import assert from 'node:assert/strict'
import {mkdtempSync, readFileSync, rmSync} from 'node:fs'
import {tmpdir} from 'node:os'
import path from 'node:path'
import {test} from 'node:test'
import {checkpointOperation} from './agent-platform-wire-fixture.mjs'

test('platform wire fixtures perform real bounded checkpoint reads and appends', () => {
  const root = mkdtempSync(path.join(tmpdir(), 'grasp-platform-checkpoint-'))
  try {
    const first = 'task-0123456789abcdef01234567:step=0'
    const second = 'task-0123456789abcdef01234567:step=1'
    assert.equal(checkpointOperation(`printf "%s\\n" ${first} >> checkpoint.txt; cat checkpoint.txt`, root), first+'\n')
    assert.equal(checkpointOperation(`printf "%s\\n" ${second} >> checkpoint.txt; cat checkpoint.txt`, root), first+'\n'+second+'\n')
    assert.equal(checkpointOperation('cat checkpoint.txt', root), first+'\n'+second+'\n')
  } finally { rmSync(root, {recursive: true, force: true}) }
})

test('model-provided shell syntax and paths cannot execute in a platform fixture', () => {
  const root = mkdtempSync(path.join(tmpdir(), 'grasp-platform-reject-'))
  try {
    const marker = 'task-0123456789abcdef01234567:step=0'
    checkpointOperation(`printf "%s\\n" ${marker} >> checkpoint.txt; cat checkpoint.txt`, root)
    for (const command of ['cat /etc/passwd', 'cat checkpoint.txt; touch escaped', '$(touch escaped)', `printf "%s\\n" ${marker};touch escaped >> checkpoint.txt; cat checkpoint.txt`, `printf "%s\\n" ${marker} >> ../checkpoint.txt; cat checkpoint.txt`, null]) {
      assert.throws(() => checkpointOperation(command, root), /unsupported platform fixture/)
    }
    assert.equal(readFileSync(path.join(root, 'checkpoint.txt'), 'utf8'), marker+'\n')
  } finally { rmSync(root, {recursive: true, force: true}) }
})
