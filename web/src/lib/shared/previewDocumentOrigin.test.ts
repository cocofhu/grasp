import { describe, expect, it } from 'vitest'
import { previewDocumentHost, toPreviewDocumentURL } from './previewDocumentOrigin'

describe('previewDocumentHost', () => {
  it('keeps a registrable name same-site under pv', () => {
    expect(previewDocumentHost('app.example.com')).toBe('pv.app.example.com')
    expect(previewDocumentHost('app.example.com', '8443')).toBe('pv.app.example.com:8443')
  })

  it('maps loopback to a trustworthy localhost name, not sslip.io', () => {
    expect(previewDocumentHost('127.0.0.1', '18081')).toBe('pv.127.0.0.1.localhost:18081')
    expect(previewDocumentHost('::1', '18081')).toBe('pv.v6-0-0-0-0-0-0-0-1.localhost:18081')
    expect(previewDocumentHost('localhost', '18082')).toBe('pv.localhost:18082')
    expect(previewDocumentHost('10.0.0.8', '18081')).toBe('pv.10.0.0.8.sslip.io:18081')
  })

  it('builds an absolute preview URL off the approval origin', () => {
    const page = { protocol: 'http:', hostname: '127.0.0.1', port: '18081' }
    expect(toPreviewDocumentURL('/preview/run-e2e/n1/9090/', page)).toBe(
      'http://pv.127.0.0.1.localhost:18081/preview/run-e2e/n1/9090/',
    )
    const local = { protocol: 'http:', hostname: 'localhost', port: '18082' }
    expect(toPreviewDocumentURL('/preview/run-e2e/n1/9090/', local)).toBe(
      'http://pv.localhost:18082/preview/run-e2e/n1/9090/',
    )
    const dns = { protocol: 'http:', hostname: 'app.example.com', port: '18080' }
    const embed = toPreviewDocumentURL('/preview/run-e2e/n1/9090/', dns)
    expect(embed).toBe('http://pv.app.example.com:18080/preview/run-e2e/n1/9090/')
    expect(new URL(embed).origin).not.toBe('http://app.example.com:18080')
  })
})
