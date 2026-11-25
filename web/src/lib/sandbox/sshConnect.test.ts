import { describe, expect, it } from 'vitest'
import { remoteIDELink, sshCommand, sshConfigSnippet, sshEndpoint, sshUser } from './sshConnect'

describe('sshConnect', () => {
  it('falls back to the page host and port 22', () => {
    expect(sshEndpoint({ enabled: true, host: ' ', port: 0 }, 'grasp.example.com')).toEqual({ host: 'grasp.example.com', port: 22 })
    expect(sshEndpoint({ enabled: true, host: 'ssh.example.com', port: 2222 }, 'x')).toEqual({ host: 'ssh.example.com', port: 2222 })
  })

  it('builds the direct command, config snippet and IDE links', () => {
    expect(sshUser(12)).toBe('sbx-12')
    expect(sshCommand(12, { host: 'ssh.example.com', port: 2222 })).toBe('ssh -p 2222 sbx-12@ssh.example.com')
    expect(sshCommand(12, { host: 'ssh.example.com', port: 22 })).toBe('ssh sbx-12@ssh.example.com')
    expect(sshConfigSnippet({ host: 'ssh.example.com', port: 2222 })).toBe('Host grasp\n  HostName ssh.example.com\n  Port 2222\n  ServerAliveInterval 30\n')
    expect(remoteIDELink('vscode', 12)).toBe('vscode://vscode-remote/ssh-remote+sbx-12@grasp/root/workspace')
    expect(remoteIDELink('cursor', 7, '/tmp')).toBe('cursor://vscode-remote/ssh-remote+sbx-7@grasp/tmp')
  })
})
