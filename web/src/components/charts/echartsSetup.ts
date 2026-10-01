import { use } from 'echarts/core'
import { CanvasRenderer } from 'echarts/renderers'
import { BarChart, HeatmapChart, LineChart, PieChart, TreemapChart } from 'echarts/charts'
import {
  DataZoomComponent,
  GridComponent,
  LegendComponent,
  TooltipComponent,
  VisualMapComponent,
} from 'echarts/components'

let registered = false

/** Register ECharts modules once for tree-shaken bundles. */
export function registerECharts(): void {
  if (registered) return
  registered = true
  use([
    CanvasRenderer,
    LineChart,
    PieChart,
    BarChart,
    HeatmapChart,
    TreemapChart,
    DataZoomComponent,
    GridComponent,
    TooltipComponent,
    LegendComponent,
    VisualMapComponent,
  ])
}
