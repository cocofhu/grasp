import { computed, reactive } from 'vue'
import { api, type BrandSettings } from '@/lib/api/api'

export const DEFAULT_PRODUCT_NAME = 'Grasp'

const brand = reactive<BrandSettings>({
  product_name: '',
  home_subtitle: '',
})
let request: Promise<void> | null = null

export function setBrandSettings(value?: Partial<BrandSettings> | null) {
  brand.product_name = (value?.product_name || '').trim()
  brand.home_subtitle = value?.home_subtitle?.trim() || ''
}

export async function loadBrandSettings() {
  if (request) return request
  request = api.getSettings()
    .then((response) => setBrandSettings(response.brand))
    .catch(() => setBrandSettings(null))
    .finally(() => {
      request = null
    })
  return request
}

export function useBrandSettings() {
  const productName = computed(() => brand.product_name || DEFAULT_PRODUCT_NAME)
  const homeSubtitle = computed(() => brand.home_subtitle)
  return { productName, homeSubtitle, loadBrandSettings, setBrandSettings }
}
