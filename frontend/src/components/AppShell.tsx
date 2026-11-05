/**
 * AppShell picks the interface experience the device is set to and renders it.
 *
 * `classic` is the rail-plus-topbar anatomy the app shipped with; the other
 * experiences are complete redesigns of the chrome — a cloud suite with an app
 * launcher and object tabs, a dark dropdown navigation bar, a workspace rail
 * with board panels, and a command-first minimal frame. All of them render the
 * same routed pages, the same search palette and the same permission-filtered
 * navigation model; only the arrangement differs, which is exactly what makes
 * each one feel like a different product rather than a reskin.
 */
import { useTheme } from '../theme'
import { ClassicShell } from '../shells/ClassicShell'
import { StratusShell } from '../shells/StratusShell'
import { HorizonShell } from '../shells/HorizonShell'
import { PulseShell } from '../shells/PulseShell'
import { SlateShell } from '../shells/SlateShell'

export function AppShell() {
  const { layout } = useTheme()
  switch (layout) {
    case 'stratus':
      return <StratusShell />
    case 'horizon':
      return <HorizonShell />
    case 'pulse':
      return <PulseShell />
    case 'slate':
      return <SlateShell />
    case 'classic':
    default:
      return <ClassicShell />
  }
}
