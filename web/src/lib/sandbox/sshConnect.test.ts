// @vitest-environment happy-dom
import { afterEach, describe, expect, it } from 'vitest'
import {
  REMOTE_IDES, loadPreferredIDE, remoteIDELink, savePreferredIDE, sshCommand, sshConfigSnippet, sshEndpoint, sshTarget, sshUser,
} from './sshConnect'

const ep = { host: 'ssh.example.com', port: 2222 }

describe('sshConnect', () => {
  afterEach(() => localStorage.clear())

  it('falls back to the page host and port 22', () => {
    expect(sshEndpoint({ enabled: true, host: ' ', port: 0 }, 'grasp.example.com')).toEqual({ host: 'grasp.example.com', port: 22 })
    expect(sshEndpoint({ enabled: true, host: 'ssh.example.com', port: 2222 }, 'x')).toEqual(ep)
  })

  it('builds targets, the direct command and the optional alias snippet', () => {
    expect(sshUser(12)).toBe('sbx-12')
    expect(sshTarget(12, ep)).toBe('sbx-12@ssh.example.com:2222')
    expect(sshTarget(12, { host: 'h', port: 22 })).toBe('sbx-12@h')
    expect(sshCommand(12, ep)).toBe('ssh -p 2222 sbx-12@ssh.example.com')
    expect(sshCommand(12, { host: 'h', port: 22 })).toBe('ssh sbx-12@h')
    expect(sshConfigSnippet(ep)).toBe('Host grasp\n  HostName ssh.example.com\n  Port 2222\n  ServerAliveInterval 30\n')
  })

  it('links every VS Code based IDE without a ~/.ssh/config entry', () => {
    expect(remoteIDELink('vscode', 12, ep)).toBe('vscode://vscode-remote/ssh-remote+sbx-12@ssh.example.com:2222/root/workspace')
    expect(remoteIDELink('cursor', 7, ep, { folder: '/tmp' })).toBe('cursor://vscode-remote/ssh-remote+sbx-7@ssh.example.com:2222/tmp')
    expect(remoteIDELink('codebuddy', 1, ep)).toBe('codebuddy://vscode-remote/ssh-remote+sbx-1@ssh.example.com:2222/root/workspace')
    expect(remoteIDELink('codebuddy', 1, ep, { cn: true })).toMatch(/^codebuddycn:\/\//)
    expect(remoteIDELink('trae', 1, ep)).toMatch(/^trae:\/\//)
    expect(remoteIDELink('trae', 1, ep, { cn: true })).toMatch(/^trae-cn:\/\//)
    expect(remoteIDELink('vscode', 1, ep, { cn: true })).toBe('')
    expect(remoteIDELink('jetbrains', 1, ep)).toBe('')
    expect(REMOTE_IDES.map((ide) => ide.id)).toEqual(['vscode', 'cursor', 'codebuddy', 'trae', 'jetbrains'])
  })

  it('remembers the preferred IDE and ignores unusable values', () => {
    expect(loadPreferredIDE()).toEqual({ id: 'vscode', cn: false })
    savePreferredIDE({ id: 'trae', cn: true })
    expect(loadPreferredIDE()).toEqual({ id: 'trae', cn: true })
    localStorage.setItem('grasp.sandbox.localIde', JSON.stringify({ id: 'cursor', cn: true }))
    expect(loadPreferredIDE()).toEqual({ id: 'cursor', cn: false })
    localStorage.setItem('grasp.sandbox.localIde', JSON.stringify({ id: 'emacs' }))
    expect(loadPreferredIDE()).toEqual({ id: 'vscode', cn: false })
    localStorage.setItem('grasp.sandbox.localIde', '{broken')
    expect(loadPreferredIDE()).toEqual({ id: 'vscode', cn: false })
  })
})
