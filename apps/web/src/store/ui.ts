import { create } from 'zustand'

const SIDEBAR_KEY = 'highland-sidebar-collapsed'

function readCollapsed(): boolean {
  try {
    return localStorage.getItem(SIDEBAR_KEY) === '1'
  } catch {
    return false
  }
}

type UIState = {
  sidebarCollapsed: boolean
  mobileSidebarOpen: boolean
  commandPaletteOpen: boolean
  setSidebarCollapsed: (v: boolean) => void
  toggleSidebar: () => void
  setMobileSidebarOpen: (v: boolean) => void
  setCommandPaletteOpen: (v: boolean) => void
  toggleCommandPalette: () => void
}

export const useUIStore = create<UIState>((set, get) => ({
  sidebarCollapsed: typeof window === 'undefined' ? false : readCollapsed(),
  mobileSidebarOpen: false,
  commandPaletteOpen: false,
  setSidebarCollapsed: (v) => {
    try {
      localStorage.setItem(SIDEBAR_KEY, v ? '1' : '0')
    } catch {
      /* ignore */
    }
    set({ sidebarCollapsed: v })
  },
  toggleSidebar: () => {
    const next = !get().sidebarCollapsed
    try {
      localStorage.setItem(SIDEBAR_KEY, next ? '1' : '0')
    } catch {
      /* ignore */
    }
    set({ sidebarCollapsed: next })
  },
  setMobileSidebarOpen: (v) => set({ mobileSidebarOpen: v }),
  setCommandPaletteOpen: (v) => set({ commandPaletteOpen: v }),
  toggleCommandPalette: () => set({ commandPaletteOpen: !get().commandPaletteOpen }),
}))

export { SIDEBAR_KEY }
