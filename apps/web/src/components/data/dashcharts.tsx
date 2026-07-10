// Theme-aware dashboard charts built on Recharts (tooltips, responsive, a11y).
import {
  Area,
  AreaChart,
  CartesianGrid,
  Cell,
  Pie,
  PieChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'

const tooltipStyle = {
  background: 'var(--color-card)',
  border: '1px solid var(--color-border)',
  borderRadius: 8,
  color: 'var(--color-foreground)',
  fontSize: 12,
} as const

export type DonutSlice = { label: string; value: number; color: string }

/** Donut chart with a centered total; interactive tooltips per slice. */
export function Donut({ slices, size = 140 }: { slices: DonutSlice[]; size?: number }) {
  const total = slices.reduce((s, x) => s + x.value, 0)
  const data = slices.filter((s) => s.value > 0)
  return (
    <div className="relative" style={{ width: size, height: size }}>
      <ResponsiveContainer width="100%" height="100%">
        <PieChart>
          <Pie
            data={data.length ? data : [{ label: 'none', value: 1, color: 'var(--color-border)' }]}
            dataKey="value"
            nameKey="label"
            innerRadius="66%"
            outerRadius="100%"
            paddingAngle={data.length > 1 ? 2 : 0}
            strokeWidth={0}
            isAnimationActive={false}
          >
            {(data.length ? data : [{ color: 'var(--color-border)' }]).map((s, i) => (
              <Cell key={i} fill={s.color} />
            ))}
          </Pie>
          {data.length > 0 && <Tooltip contentStyle={tooltipStyle} />}
        </PieChart>
      </ResponsiveContainer>
      <div className="pointer-events-none absolute inset-0 flex items-center justify-center">
        <span className="text-xl font-semibold tabular-nums text-[var(--color-foreground)]">{total}</span>
      </div>
    </div>
  )
}

/** Horizontal used/total capacity bar (a single-value progress bar). */
export function UsageBar({ used, total }: { used: number; total: number }) {
  const pct = total > 0 ? Math.min(100, (used / total) * 100) : 0
  const tone = pct >= 90 ? '#dc2626' : pct >= 75 ? '#d97706' : 'var(--color-primary)'
  return (
    <div className="h-3 w-full overflow-hidden rounded-full bg-[var(--color-muted,rgba(120,120,120,0.18))]">
      <div className="h-full rounded-full transition-all" style={{ width: `${pct}%`, background: tone }} />
    </div>
  )
}

/** Filled area sparkline for a numeric series, with a hover tooltip. When
 * `axis` is set it becomes a fuller chart with a formatted Y-axis + gridlines. */
export function AreaSparkline({
  points,
  emptyLabel,
  height = 64,
  format,
  axis = false,
}: {
  points: number[]
  emptyLabel: string
  height?: number
  format?: (v: number) => string
  axis?: boolean
}) {
  if (points.length < 2) {
    return <div className="flex items-center text-xs text-[var(--color-muted-foreground)]" style={{ height }}>{emptyLabel}</div>
  }
  const data = points.map((v, i) => ({ i, v }))
  const tickFmt = format ? (v: number) => format(v) : undefined
  return (
    <ResponsiveContainer width="100%" height={height}>
      <AreaChart data={data} margin={{ top: 4, right: 4, bottom: 0, left: axis ? 4 : 0 }}>
        <defs>
          <linearGradient id="dashArea" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor="var(--color-primary)" stopOpacity={0.35} />
            <stop offset="100%" stopColor="var(--color-primary)" stopOpacity={0} />
          </linearGradient>
        </defs>
        {axis ? (
          <CartesianGrid stroke="var(--color-border)" strokeDasharray="3 3" vertical={false} />
        ) : null}
        <XAxis dataKey="i" hide />
        <YAxis
          hide={!axis}
          width={axis ? 56 : 0}
          domain={[0, 'dataMax']}
          tick={{ fontSize: 10, fill: 'var(--color-muted-foreground)' }}
          tickFormatter={tickFmt}
          tickCount={4}
          stroke="var(--color-border)"
        />
        <Tooltip
          contentStyle={tooltipStyle}
          labelFormatter={() => ''}
          formatter={(value) => [format ? format(Number(value)) : String(value), '']}
        />
        <Area
          type="monotone"
          dataKey="v"
          stroke="var(--color-primary)"
          strokeWidth={1.75}
          fill="url(#dashArea)"
          isAnimationActive={false}
        />
      </AreaChart>
    </ResponsiveContainer>
  )
}

/** Small colored legend dot + label + value row. */
export function LegendRow({ color, label, value }: { color: string; label: string; value: string | number }) {
  return (
    <div className="flex items-center justify-between gap-2 text-sm">
      <span className="flex items-center gap-2">
        <span className="inline-block h-2.5 w-2.5 rounded-full" style={{ background: color }} />
        {label}
      </span>
      <span className="tabular-nums text-[var(--color-muted-foreground)]">{value}</span>
    </div>
  )
}

/**
 * A labelled metric line: names what is measured, shows the current value and
 * peak (with units), and a trend chart with a hover tooltip. Used across the
 * dashboard, performance, and detail pages so every chart has a reference.
 */
export function MetricLine({
  label,
  points,
  format,
  emptyLabel,
  peakLabel,
  height = 48,
  axis = false,
}: {
  label: string
  points: number[]
  format: (v: number) => string
  emptyLabel: string
  peakLabel: string
  height?: number
  axis?: boolean
}) {
  const current = points.at(-1) ?? 0
  const peak = points.length ? Math.max(...points) : 0
  return (
    <div>
      <div className="mb-0.5 flex items-baseline justify-between gap-2">
        <span className="text-xs font-medium text-[var(--color-foreground)]">{label}</span>
        <span className="tabular-nums text-sm font-semibold text-[var(--color-foreground)]">
          {format(current)}
        </span>
      </div>
      <div className="mb-1 text-[10px] text-[var(--color-muted-foreground)]">
        {peakLabel}: <span className="tabular-nums">{format(peak)}</span>
      </div>
      <AreaSparkline
        points={points}
        emptyLabel={emptyLabel}
        height={axis ? Math.max(height, 120) : height}
        format={format}
        axis={axis}
      />
    </div>
  )
}
