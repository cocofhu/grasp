import type { Locator } from '@playwright/test'

/** Valid, silent PCM audio for browser playback without external services. */
export function waveFixture(): Buffer {
  const samples = 8000
  const data = Buffer.alloc(44 + samples * 2)
  data.write('RIFF'); data.writeUInt32LE(data.length - 8, 4); data.write('WAVEfmt ', 8)
  data.writeUInt32LE(16, 16); data.writeUInt16LE(1, 20); data.writeUInt16LE(1, 22)
  data.writeUInt32LE(8000, 24); data.writeUInt32LE(16000, 28); data.writeUInt16LE(2, 32); data.writeUInt16LE(16, 34)
  data.write('data', 36); data.writeUInt32LE(samples * 2, 40)
  return data
}

/** Create the transfer inside the input's frame, including when it is embedded. */
export async function dropTextFile(input: Locator, name: string, text: string) {
  const dataTransfer = await input.evaluateHandle((_, file) => {
    const dt = new DataTransfer()
    dt.items.add(new File([file.text], file.name, { type: 'text/plain' }))
    return dt
  }, { name, text })
  await input.dispatchEvent('dragenter', { dataTransfer })
  await input.dispatchEvent('drop', { dataTransfer })
  await dataTransfer.dispose()
}
