import { toCanvas } from 'html-to-image'
import { jsPDF } from 'jspdf'

/** Default wait for test_result screenshot placeholders before exporting. */
const TEST_SCREENSHOT_WAIT_MS = 5000

/**
 * Outer padding injected only on the html-to-image clone (not the live preview DOM).
 * Gold standard: page.html「改后·16px」; matches inline preview ancestor p-4.
 */
export const EXPORT_OUTER_PADDING_PX = 16

/** Soft browser canvas limits; downscale when exceeded. */
const MAX_CANVAS_SIDE = 16384
const MAX_CANVAS_AREA = 268_435_456

export type StructuredExportFormat = 'png' | 'pdf'

type StructuredExportResult = {
  filename: string
  incomplete: boolean
}

/** Strip the last extension and sanitize path-illegal characters (does not force .json). */
export function exportBasenameFromArtifactName(name: string): string {
  const raw = String(name || 'artifact').trim() || 'artifact'
  const withoutExt = raw.replace(/\.[^.]+$/, '') || 'artifact'
  return withoutExt.replace(/[^\w\u4e00-\u9fff\- .]/g, '_').replace(/[/\\]/g, '_')
}

export function exportFilename(artifactName: string, ext: StructuredExportFormat): string {
  return `${exportBasenameFromArtifactName(artifactName)}.${ext}`
}

/** Resolve current theme base color from CSS tokens (rgb channels on --c-base). */
export function resolveThemeBackgroundColor(el: Element = document.documentElement): string {
  const raw = getComputedStyle(el).getPropertyValue('--c-base').trim()
  if (raw) return `rgb(${raw})`
  return 'rgb(10, 10, 11)'
}

/** Trigger a browser download from a Blob (mirrors downloadJson pattern). */
function triggerBlobDownload(filename: string, blob: Blob): void {
  const a = document.createElement('a')
  const url = URL.createObjectURL(blob)
  a.href = url
  a.download = filename
  a.click()
  URL.revokeObjectURL(url)
}

/**
 * Wait until test_result screenshot shimmers leave the export root, or timeout.
 * @returns true if ready (or no loading placeholders); false if timed out while still loading.
 */
export async function waitForTestScreenshots(
  root: HTMLElement,
  timeoutMs = TEST_SCREENSHOT_WAIT_MS,
): Promise<boolean> {
  const hasLoading = () => !!root.querySelector('.shot-shimmer')
  if (!hasLoading()) return true

  const start = Date.now()
  return new Promise((resolve) => {
    const tick = () => {
      if (!hasLoading()) {
        resolve(true)
        return
      }
      if (Date.now() - start >= timeoutMs) {
        resolve(false)
        return
      }
      window.setTimeout(tick, 50)
    }
    window.setTimeout(tick, 50)
  })
}

export function computePixelRatio(width: number, height: number): number {
  let pixelRatio = Math.min(2, typeof window !== 'undefined' ? window.devicePixelRatio || 1 : 1)
  while (
    pixelRatio > 0.25 &&
    (width * pixelRatio > MAX_CANVAS_SIDE ||
      height * pixelRatio > MAX_CANVAS_SIDE ||
      width * height * pixelRatio * pixelRatio > MAX_CANVAS_AREA)
  ) {
    pixelRatio *= 0.75
  }
  return Math.max(0.25, pixelRatio)
}

/** Content box vs export canvas size (content + 2× outer padding). */
function measureStructuredExportBox(el: HTMLElement): {
  contentWidth: number
  contentHeight: number
  exportWidth: number
  exportHeight: number
  backgroundColor: string
} {
  const backgroundColor = resolveThemeBackgroundColor()
  const contentWidth = Math.max(el.scrollWidth, el.offsetWidth, 1)
  const contentHeight = Math.max(el.scrollHeight, el.offsetHeight, 1)
  return {
    contentWidth,
    contentHeight,
    exportWidth: contentWidth + 2 * EXPORT_OUTER_PADDING_PX,
    exportHeight: contentHeight + 2 * EXPORT_OUTER_PADDING_PX,
    backgroundColor,
  }
}

/** Capture the full scrollable box of `el` (not just the visible viewport). */
async function captureStructuredElement(el: HTMLElement): Promise<HTMLCanvasElement> {
  const { exportWidth, exportHeight, backgroundColor } = measureStructuredExportBox(el)
  const pixelRatio = computePixelRatio(exportWidth, exportHeight)

  return toCanvas(el, {
    backgroundColor,
    width: exportWidth,
    height: exportHeight,
    canvasWidth: Math.ceil(exportWidth * pixelRatio),
    canvasHeight: Math.ceil(exportHeight * pixelRatio),
    pixelRatio,
    style: {
      overflow: 'visible',
      height: `${exportHeight}px`,
      maxHeight: 'none',
      width: `${exportWidth}px`,
      padding: `${EXPORT_OUTER_PADDING_PX}px`,
      boxSizing: 'border-box',
    },
  })
}

function canvasToPngBlob(canvas: HTMLCanvasElement): Promise<Blob> {
  return new Promise((resolve, reject) => {
    canvas.toBlob((blob) => {
      if (blob) resolve(blob)
      else reject(new Error('PNG encoding failed'))
    }, 'image/png')
  })
}

type CanvasToPdfOptions = {
  /** CSS width of the export canvas (content + 2× outer padding). */
  exportCssWidth: number
  /** Theme background used to fill page margins (must match PNG padding). */
  backgroundColor: string
  /** Outer padding in CSS px; defaults to EXPORT_OUTER_PADDING_PX. */
  outerPaddingPx?: number
}

type PdfPageSlice = {
  srcX: number
  srcY: number
  srcW: number
  srcH: number
  destX: number
  destY: number
  destW: number
  destH: number
}

type PdfPageLayout = {
  marginMm: number
  pageWidthMm: number
  pageHeightMm: number
  pageBmpWidth: number
  pageBmpHeight: number
  insetPx: number
  slices: PdfPageSlice[]
}

/**
 * Slice only the content region (excluding baked-in 16px outer padding) so every
 * PDF page can be recomposed with the same inset margins. Do not feed the full
 * padded canvas through negative-Y addImage — that keeps top padding on page 1
 * and bottom padding on the last page only.
 */
export function computePdfPageLayout(
  canvas: { width: number; height: number },
  options: {
    exportCssWidth: number
    outerPaddingPx?: number
    pageWidthMm?: number
    pageHeightMm?: number
  },
): PdfPageLayout {
  const outerPaddingPx = options.outerPaddingPx ?? EXPORT_OUTER_PADDING_PX
  const pageWidthMm = options.pageWidthMm ?? 210
  const pageHeightMm = options.pageHeightMm ?? 297
  const exportCssWidth = Math.max(options.exportCssWidth, 1)
  const canvasWidth = Math.max(canvas.width, 1)
  const canvasHeight = Math.max(canvas.height, 1)

  const marginMm = (outerPaddingPx / exportCssWidth) * pageWidthMm
  const scale = canvasWidth / exportCssWidth
  const padCanvas = Math.min(outerPaddingPx * scale, (canvasWidth - 1) / 2, (canvasHeight - 1) / 2)

  const contentLeft = padCanvas
  const contentTop = padCanvas
  const contentWidth = Math.max(canvasWidth - 2 * padCanvas, 1)
  const contentHeight = Math.max(canvasHeight - 2 * padCanvas, 1)

  const pageBmpWidth = canvasWidth
  const pageBmpHeight = Math.max(1, Math.round(pageBmpWidth * (pageHeightMm / pageWidthMm)))
  const insetPx = padCanvas
  const destWidth = Math.max(pageBmpWidth - 2 * insetPx, 1)
  const destHeight = Math.max(pageBmpHeight - 2 * insetPx, 1)

  const slices: PdfPageSlice[] = []
  let remaining = contentHeight
  let srcY = contentTop
  while (remaining > 0.5 || slices.length === 0) {
    const srcH = Math.min(remaining, destHeight)
    slices.push({
      srcX: contentLeft,
      srcY,
      srcW: contentWidth,
      srcH,
      destX: insetPx,
      destY: insetPx,
      destW: destWidth,
      destH: srcH,
    })
    srcY += srcH
    remaining -= srcH
    if (remaining <= 0.5) break
  }

  return {
    marginMm,
    pageWidthMm,
    pageHeightMm,
    pageBmpWidth,
    pageBmpHeight,
    insetPx,
    slices,
  }
}

function cssColorToCanvasFill(color: string): string {
  const spaceRgb = color.match(/^rgba?\(\s*([\d.]+)\s+([\d.]+)\s+([\d.]+)((?:\s*\/\s*[\d.]+)?)?\s*\)$/i)
  if (spaceRgb) {
    const alpha = spaceRgb[4]
    if (alpha) return `rgba(${spaceRgb[1]}, ${spaceRgb[2]}, ${spaceRgb[3]}${alpha.replace('/', ',')})`
    return `rgb(${spaceRgb[1]}, ${spaceRgb[2]}, ${spaceRgb[3]})`
  }
  return color
}

/**
 * Build a multi-page A4 PDF: each page is a theme-filled bitmap with the content
 * slice drawn into a 16px-equivalent inset rect (margins live in the bitmap so
 * jsPDF's default white page never shows).
 */
export function canvasToPdfBlob(canvas: HTMLCanvasElement, options: CanvasToPdfOptions): Blob {
  const layout = computePdfPageLayout(canvas, {
    exportCssWidth: options.exportCssWidth,
    outerPaddingPx: options.outerPaddingPx,
  })
  const fillStyle = cssColorToCanvasFill(options.backgroundColor)
  const pdf = new jsPDF({ orientation: 'portrait', unit: 'mm', format: 'a4' })

  layout.slices.forEach((slice, index) => {
    const pageCanvas = document.createElement('canvas')
    pageCanvas.width = layout.pageBmpWidth
    pageCanvas.height = layout.pageBmpHeight
    const ctx = pageCanvas.getContext('2d')
    if (!ctx) throw new Error('PDF page canvas 2d context unavailable')
    ctx.fillStyle = fillStyle
    ctx.fillRect(0, 0, layout.pageBmpWidth, layout.pageBmpHeight)
    ctx.drawImage(
      canvas,
      slice.srcX,
      slice.srcY,
      slice.srcW,
      slice.srcH,
      slice.destX,
      slice.destY,
      slice.destW,
      slice.destH,
    )
    if (index > 0) pdf.addPage()
    pdf.addImage(pageCanvas.toDataURL('image/png'), 'PNG', 0, 0, layout.pageWidthMm, layout.pageHeightMm)
  })

  return pdf.output('blob')
}

/**
 * Export the visible StructuredArtifactView DOM to PNG or PDF.
 * Callers should surface incomplete/failure via toast.
 */
export async function exportStructuredArtifact(
  el: HTMLElement,
  artifactName: string,
  format: StructuredExportFormat,
  options?: { screenshotWaitMs?: number },
): Promise<StructuredExportResult> {
  const incomplete = !(await waitForTestScreenshots(el, options?.screenshotWaitMs ?? TEST_SCREENSHOT_WAIT_MS))
  const box = measureStructuredExportBox(el)
  const canvas = await captureStructuredElement(el)
  const filename = exportFilename(artifactName, format)

  if (format === 'png') {
    const blob = await canvasToPngBlob(canvas)
    triggerBlobDownload(filename, blob)
  } else {
    const blob = canvasToPdfBlob(canvas, {
      exportCssWidth: box.exportWidth,
      backgroundColor: box.backgroundColor,
    })
    triggerBlobDownload(filename, blob)
  }

  return { filename, incomplete }
}
