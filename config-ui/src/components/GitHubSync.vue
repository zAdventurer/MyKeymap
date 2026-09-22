<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { server, type SyncDifference, type SyncStatus, type SyncStatusResponse } from '@/store/server'

type SyncOperation = 'loading' | 'saving' | 'pulling' | 'pushing' | 'diffing' | 'resolving' | null

const status = ref<SyncStatus | null>(null)
const repositoryUrl = ref('')
const branch = ref('')
const operation = ref<SyncOperation>(null)
const errorMessage = ref('')
const differences = ref<SyncDifference[]>([])
const rawDiff = ref('')
const rawPanel = ref<string>()
const copyMessage = ref('')
const showDiff = ref(false)

const isBusy = computed(() => operation.value !== null)
const isConflict = computed(() => status.value === 'conflict')
const statusText = computed(() => {
  const labels: Record<SyncStatus, string> = {
    synchronized: '已同步',
    'local-only': '本地配置有更新，等待推送',
    'remote-only': '远端配置有更新，等待拉取',
    conflict: '存在冲突：本地和远端均已修改',
  }
  return status.value ? labels[status.value] : '状态不可用'
})
const statusColor = computed(() => {
  if (status.value === 'conflict') return 'error'
  if (status.value === 'synchronized') return 'success'
  return 'warning'
})
const groupedDifferences = computed(() => {
  const groups = new Map<string, SyncDifference[]>()
  for (const difference of differences.value) {
    const items = groups.get(difference.group) ?? []
    items.push(difference)
    groups.set(difference.group, items)
  }
  return Array.from(groups, ([name, items]) => ({ name, items }))
})
const kindPresentation: Record<SyncDifference['kind'], { label: string, color: string }> = {
  added: { label: '新增', color: 'success' },
  removed: { label: '删除', color: 'error' },
  modified: { label: '修改', color: 'warning' },
}

function readableError(value: unknown) {
  if (typeof value === 'string' && value.trim()) return value

  if (value && typeof value === 'object') {
    const error = value as { error?: { message?: unknown }, message?: unknown }
    if (typeof error.error?.message === 'string') return error.error.message
    if (typeof error.message === 'string') return error.message
  }

  return '无法连接同步服务。请确认 MyKeymap 正在运行后重试。'
}

async function request<T>(operationName: Exclude<SyncOperation, null>, action: () => Promise<T>) {
  operation.value = operationName
  errorMessage.value = ''

  try {
    return await action()
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
    differences.value = response.differences ?? []
    rawDiff.value = response.rawDiff ?? ''
    rawPanel.value = undefined
    copyMessage.value = ''
    showDiff.value = true
  }
}

async function copyRawDiff() {
  try {
    await navigator.clipboard.writeText(rawDiff.value)
    copyMessage.value = '已复制'
  } catch {
    copyMessage.value = '复制失败，请手动选择文本'
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
  <v-card title="GitHub 配置同步" min-width="350" elevation="2">
    <v-card-text>
      <v-alert v-if="errorMessage" class="mb-4" density="compact" type="error" variant="tonal" closable @click:close="errorMessage = ''">
        {{ errorMessage }}
      </v-alert>

      <v-text-field v-model="repositoryUrl" label="仓库地址" variant="underlined" :disabled="isBusy" />
      <v-text-field v-model="branch" label="分支" variant="underlined" :disabled="isBusy" />

      <div class="d-flex align-center flex-wrap ga-2 mb-3">
        <v-chip :color="statusColor" label size="small">
          {{ statusText }}
        </v-chip>
        <v-progress-circular v-if="isBusy" indeterminate color="primary" size="20" width="2" />
      </div>

      <div class="d-flex flex-wrap ga-2">
        <v-btn class="text-none" color="primary" variant="outlined" :disabled="isBusy" @click="saveSettings">保存仓库</v-btn>
        <v-btn class="text-none" color="blue" variant="outlined" :disabled="isBusy" @click="runAction('pull')">拉取</v-btn>
        <v-btn class="text-none" color="green" variant="outlined" :disabled="isBusy" @click="runAction('push')">推送</v-btn>
        <v-btn class="text-none" variant="outlined" :disabled="isBusy" @click="viewDiff">查看差异</v-btn>
        <v-btn icon="mdi-refresh" variant="text" :disabled="isBusy" aria-label="刷新同步状态" @click="refreshStatus" />
      </div>

      <v-alert v-if="isConflict" class="mt-4" density="compact" type="warning" variant="tonal">
        本地与远端配置都已修改。请选择保留哪个版本；另一个版本会由 MyKeymap 自动备份。
        <div class="d-flex flex-wrap ga-2 mt-3">
          <v-btn class="text-none" color="primary" variant="outlined" :disabled="isBusy" @click="resolveConflict('keep-local')">保留本地</v-btn>
          <v-btn class="text-none" color="warning" variant="outlined" :disabled="isBusy" @click="resolveConflict('use-remote')">使用远端</v-btn>
        </div>
      </v-alert>
    </v-card-text>
  </v-card>

  <v-dialog v-model="showDiff" max-width="1100">
    <v-card title="配置差异">
      <v-card-text class="diff-dialog-body">
        <v-alert v-if="differences.length === 0" type="info" variant="tonal">
          本地与远端没有可显示的配置差异。
        </v-alert>

        <section v-for="group in groupedDifferences" :key="group.name" class="mb-6">
          <h3 class="text-h6 mb-3">{{ group.name }}</h3>
          <v-card v-for="item in group.items" :key="`${item.path}:${item.kind}`" class="mb-3" variant="outlined">
            <v-card-title class="d-flex align-center flex-wrap ga-2 text-subtitle-1">
              <span>{{ item.path }}</span>
              <v-chip :color="kindPresentation[item.kind].color" size="small" label>
                {{ kindPresentation[item.kind].label }}
              </v-chip>
            </v-card-title>
            <v-card-text>
              <v-row>
                <v-col cols="12" md="6">
                  <div class="text-caption text-medium-emphasis mb-1">本地</div>
                  <div class="semantic-value">{{ item.local }}</div>
                </v-col>
                <v-col cols="12" md="6">
                  <div class="text-caption text-medium-emphasis mb-1">远端</div>
                  <div class="semantic-value">{{ item.remote }}</div>
                </v-col>
              </v-row>
            </v-card-text>
          </v-card>
        </section>

        <v-expansion-panels v-if="rawDiff" v-model="rawPanel" class="mt-4">
          <v-expansion-panel value="raw" title="高级：原始 JSON 差异">
            <v-expansion-panel-text>
              <div class="d-flex align-center ga-2 mb-2">
                <v-btn class="text-none" size="small" variant="outlined" @click="copyRawDiff">复制</v-btn>
                <span class="text-caption">{{ copyMessage }}</span>
              </div>
              <pre class="sync-diff">{{ rawDiff }}</pre>
            </v-expansion-panel-text>
          </v-expansion-panel>
        </v-expansion-panels>
      </v-card-text>
      <v-card-actions class="justify-end">
        <v-btn class="text-none" color="primary" @click="showDiff = false">关闭</v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>

<style scoped>
.diff-dialog-body {
  max-height: 75vh;
  overflow-y: auto;
}

.semantic-value {
  white-space: pre-wrap;
  word-break: break-word;
}

.sync-diff {
  max-height: 45vh;
  overflow: auto;
  padding: 12px;
  white-space: pre-wrap;
  word-break: break-word;
  background: #f5f5f5;
  border-radius: 4px;
}
</style>
