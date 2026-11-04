/**
 * The active workspace.
 *
 * A single stored choice, read on mount and written on change, mirroring the
 * theme provider. It is a lens on the existing navigation, not a permission: the
 * shell filters the workspace's routes through the same `can()` check the sidebar
 * always used, so a workspace can only hide links the user could not follow
 * anyway. It is personal and lives in localStorage, next to the appearance
 * choice, because it describes how this person works rather than shared data.
 */
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from 'react'
import {
  DEFAULT_WORKSPACE,
  WORKSPACE_STORAGE_KEY,
  isWorkspace,
  type Workspace,
} from '../workspaces'

interface WorkspaceValue {
  workspace: Workspace
  setWorkspace: (workspace: Workspace) => void
}

const WorkspaceContext = createContext<WorkspaceValue | null>(null)

export function resolveInitialWorkspace(): Workspace {
  if (typeof window === 'undefined') return DEFAULT_WORKSPACE
  try {
    const stored = window.localStorage.getItem(WORKSPACE_STORAGE_KEY)
    return isWorkspace(stored) ? stored : DEFAULT_WORKSPACE
  } catch {
    return DEFAULT_WORKSPACE
  }
}

export function WorkspaceProvider({ children }: { children: ReactNode }) {
  const [workspace, setWorkspaceState] = useState<Workspace>(resolveInitialWorkspace)

  useEffect(() => {
    // Reflected on the document so tests and any future styling can observe the
    // active workspace without reaching into React state.
    document.documentElement.setAttribute('data-workspace', workspace)
  }, [workspace])

  const setWorkspace = useCallback((next: Workspace) => {
    setWorkspaceState(next)
    try {
      window.localStorage.setItem(WORKSPACE_STORAGE_KEY, next)
    } catch {
      // The choice still applies for this session if storage is unavailable.
    }
  }, [])

  const value = useMemo<WorkspaceValue>(
    () => ({ workspace, setWorkspace }),
    [workspace, setWorkspace],
  )
  return <WorkspaceContext.Provider value={value}>{children}</WorkspaceContext.Provider>
}

export function useWorkspace(): WorkspaceValue {
  const ctx = useContext(WorkspaceContext)
  if (!ctx) throw new Error('useWorkspace must be used inside a WorkspaceProvider')
  return ctx
}
