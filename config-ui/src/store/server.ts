import { createFetch } from "@vueuse/core"

export const useMyFetch = createFetch({
  baseUrl: import.meta.env.MODE == 'development' ? 'http://localhost:12333' : '',
  options: {
  }
})

export type SyncStatus = 'synchronized' | 'local-only' | 'remote-only' | 'conflict'

export interface SyncSettings {
  repositoryUrl: string
  branch: string
}

export interface SyncStatusResponse {
  status: SyncStatus
  settings: SyncSettings
}

export interface SyncActionResponse {
  ok: boolean
  action: 'pull' | 'push' | 'resolve'
  status: SyncStatus
}

export interface SyncDiffResponse {
  status: SyncStatus
  diff: string
}

async function syncRequest<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, init)
  const body = await response.json().catch(() => null)
  if (!response.ok) throw body ?? new Error(`同步请求失败（HTTP ${response.status}）`)
  return body as T
}

export const server = {
  runWindowSpy: () => useMyFetch('/server/command/2').post(),
  enableRunAtStartup: () => useMyFetch('/server/command/3').post(),
  disableRunAtStartup: () => useMyFetch('/server/command/4').post(),
  getSyncStatus: () => syncRequest<SyncStatusResponse>('/sync/status'),
  saveSyncSettings: (settings: SyncSettings) => syncRequest<{ settings: SyncSettings }>('/sync/settings', {
    method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(settings),
  }),
  pullSync: () => syncRequest<SyncActionResponse>('/sync/pull', { method: 'POST' }),
  pushSync: () => syncRequest<SyncActionResponse>('/sync/push', { method: 'POST' }),
  getSyncDiff: () => syncRequest<SyncDiffResponse>('/sync/diff'),
  resolveSyncConflict: (resolution: 'keep-local' | 'use-remote') => syncRequest<SyncActionResponse>('/sync/resolve', {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ resolution }),
  }),
}
