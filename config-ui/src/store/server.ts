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


export const server = {
  runWindowSpy: () => useMyFetch('/server/command/2').post(),
  enableRunAtStartup: () => useMyFetch('/server/command/3').post(),
  disableRunAtStartup: () => useMyFetch('/server/command/4').post(),
  getSyncStatus: () => useMyFetch('/sync/status').json<SyncStatusResponse>(),
  saveSyncSettings: (settings: SyncSettings) => useMyFetch('/sync/settings').put(settings).json<{ settings: SyncSettings }>(),
  pullSync: () => useMyFetch('/sync/pull').post().json<SyncActionResponse>(),
  pushSync: () => useMyFetch('/sync/push').post().json<SyncActionResponse>(),
  getSyncDiff: () => useMyFetch('/sync/diff').json<SyncDiffResponse>(),
  resolveSyncConflict: (resolution: 'keep-local' | 'use-remote') => useMyFetch('/sync/resolve').post({ resolution }).json<SyncActionResponse>(),
}
