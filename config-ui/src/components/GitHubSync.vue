<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { server, type SyncStatus, type SyncStatusResponse } from '@/store/server'

type SyncOperation = 'loading' | 'saving' | 'pulling' | 'pushing' | 'diffing' | 'resolving' | null

const status = ref<SyncStatus | null>(null)
const repositoryUrl = ref('')
const branch = ref('')
const operation = ref<SyncOperation>(null)
const errorMessage = ref('')
const diff = ref('')
const showDiff = ref(false)

const isBusy = computed(() => operation.value !== null)
const isConflict = computed(() => status.value === 'conflict')
const statusText = computed(() => {
  const labels: Record<SyncStatus, string> = {
    synchronized: 'Synchronized',
    'local-only': 'Local changes waiting to be pushed',
    'remote-only': 'Remote changes waiting to be pulled',
    conflict: 'Conflict: both local and remote settings changed',
  }
  return status.value ? labels[status.value] : 'Status unavailable'
})
const statusColor = computed(() => {
  if (status.value === 'conflict') return 'error'
  if (status.value === 'synchronized') return 'success'
  return 'warning'
})

function readableError(value: unknown) {
  if (typeof value === 'string' && value.trim()) return value

  if (value && typeof value === 'object') {
    const error = value as { error?: { message?: unknown }, message?: unknown }
    if (typeof error.error?.message === 'string') return error.error.message
    if (typeof error.message === 'string') return error.message
  }

  return 'The sync service could not be reached. Check that MyKeymap is running, then try again.'
}

async function request<T>(operationName: Exclude<SyncOperation, null>, action: () => PromiseLike<{ data: { value: T | null }, error: { value: unknown } }>) {
  operation.value = operationName
  errorMessage.value = ''

  try {
    const response = await action()
    if (response.error.value) {
      throw response.error.value
    }
    if (!response.data.value) {
      throw new Error('No response was received from the sync service.')
    }
    return response.data.value
  } catch (error) {
    errorMessage.value = readableError(error)
    return null
  } finally {
    operation.value = null
  }
}

function applyStatus(response: SyncStatusResponse) {
  status.value = response.status
  repositoryUrl.value = response.settings.repositoryUrl
  branch.value = response.settings.branch
}

async function refreshStatus() {
  const response = await request('loading', () => server.getSyncStatus())
  if (response) applyStatus(response)
}

async function saveSettings() {
  const response = await request('saving', () => server.saveSyncSettings({
    repositoryUrl: repositoryUrl.value,
    branch: branch.value,
  }))
  if (response) {
    repositoryUrl.value = response.settings.repositoryUrl
    branch.value = response.settings.branch
    await refreshStatus()
  }
}

async function runAction(action: 'pull' | 'push') {
  const response = await request(action === 'pull' ? 'pulling' : 'pushing', () => action === 'pull' ? server.pullSync() : server.pushSync())
  if (response) status.value = response.status
  await refreshStatus()
}

async function viewDiff() {
  const response = await request('diffing', () => server.getSyncDiff())
  if (response) {
    status.value = response.status
    diff.value = response.diff || 'No textual differences are available.'
    showDiff.value = true
  }
}

async function resolveConflict(resolution: 'keep-local' | 'use-remote') {
  const response = await request('resolving', () => server.resolveSyncConflict(resolution))
  if (response) status.value = response.status
  await refreshStatus()
}

onMounted(refreshStatus)
</script>

<template>
  <v-card title="GitHub sync" min-width="350" elevation="2">
    <v-card-text>
      <v-alert v-if="errorMessage" class="mb-4" density="compact" type="error" variant="tonal" closable @click:close="errorMessage = ''">
        {{ errorMessage }}
      </v-alert>

      <v-text-field v-model="repositoryUrl" label="Repository" variant="underlined" :disabled="isBusy" />
      <v-text-field v-model="branch" label="Branch" variant="underlined" :disabled="isBusy" />

      <div class="d-flex align-center flex-wrap ga-2 mb-3">
        <v-chip :color="statusColor" label size="small">
          {{ statusText }}
        </v-chip>
        <v-progress-circular v-if="isBusy" indeterminate color="primary" size="20" width="2" />
      </div>

      <div class="d-flex flex-wrap ga-2">
        <v-btn class="text-none" color="primary" variant="outlined" :disabled="isBusy" @click="saveSettings">Save repository</v-btn>
        <v-btn class="text-none" color="blue" variant="outlined" :disabled="isBusy" @click="runAction('pull')">Pull</v-btn>
        <v-btn class="text-none" color="green" variant="outlined" :disabled="isBusy" @click="runAction('push')">Push</v-btn>
        <v-btn class="text-none" variant="outlined" :disabled="isBusy" @click="viewDiff">View diff</v-btn>
        <v-btn icon="mdi-refresh" variant="text" :disabled="isBusy" aria-label="Refresh sync status" @click="refreshStatus" />
      </div>

      <v-alert v-if="isConflict" class="mt-4" density="compact" type="warning" variant="tonal">
        Local and remote settings both changed. Choose which version to keep; the other copy is backed up by MyKeymap.
        <div class="d-flex flex-wrap ga-2 mt-3">
          <v-btn class="text-none" color="primary" variant="outlined" :disabled="isBusy" @click="resolveConflict('keep-local')">Keep local</v-btn>
          <v-btn class="text-none" color="warning" variant="outlined" :disabled="isBusy" @click="resolveConflict('use-remote')">Use remote</v-btn>
        </div>
      </v-alert>
    </v-card-text>
  </v-card>

  <v-dialog v-model="showDiff" max-width="900">
    <v-card title="Configuration differences">
      <v-card-text>
        <pre class="sync-diff">{{ diff }}</pre>
      </v-card-text>
      <v-card-actions class="justify-end">
        <v-btn class="text-none" color="primary" @click="showDiff = false">Close</v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>

<style scoped>
.sync-diff {
  max-height: 55vh;
  overflow: auto;
  padding: 12px;
  white-space: pre-wrap;
  word-break: break-word;
  background: #f5f5f5;
  border-radius: 4px;
}
</style>
