<template>
  <div>
    <el-card>
      <template #header>
        <div style="display: flex; align-items: center; gap: 12px">
          <span>设备发现</span>
          <el-select v-model="timeoutMs" style="width: 130px">
            <el-option label="1 秒" :value="1000" />
            <el-option label="3 秒" :value="3000" />
            <el-option label="5 秒" :value="5000" />
            <el-option label="10 秒" :value="10000" />
          </el-select>
          <el-input v-model="keyword" placeholder="关键字（SN / IP / 名称 / 型号）" style="width: 260px" clearable
            @keyup.enter="scan" />
          <el-button type="primary" :loading="loading" @click="scan">扫描</el-button>
          <span style="color: var(--el-text-color-secondary)">UDP 组播 239.255.255.250:37020</span>
        </div>
      </template>
      <el-alert v-if="error" :title="error" type="error" show-icon style="margin-bottom: 12px" />
      <el-alert v-if="savedCount > 0" type="success" :closable="true" @close="savedCount = 0"
        :title="`已发现 ${savedCount} 台，合并入设备库（localStorage）`" style="margin-bottom: 12px" />
      <el-table :data="devices" v-loading="loading" stripe>
        <el-table-column prop="serialNo" label="序列号" min-width="160" />
        <el-table-column prop="model" label="型号" min-width="150" />
        <el-table-column prop="ipv4" label="IPv4" width="140" />
        <el-table-column label="寻址方式" width="100">
          <template #default="{ row }">
            <el-tag :type="row.dhcp ? 'warning' : 'success'">{{ row.dhcp ? 'DHCP' : '静态' }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="mac" label="MAC" width="150" />
        <el-table-column label="HTTP / SDK" width="120">
          <template #default="{ row }">{{ row.httpPort }} / {{ row.sdkPort }}</template>
        </el-table-column>
        <el-table-column prop="version" label="固件" width="160" />
        <el-table-column label="激活" width="80">
          <template #default="{ row }">
            <el-tag :type="row.activated ? 'success' : 'warning'">{{ row.activated ? '已激活' : '未激活' }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="source" label="应答来源" width="150" />
        <template #empty>暂无结果，点击「扫描」开始发现</template>
      </el-table>
    </el-card>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { api } from '../api'
import { saveDevices } from '../devices'

const timeoutMs = ref(3000)
const keyword = ref('')
const loading = ref(false)
const error = ref('')
const devices = ref([])
const savedCount = ref(0)

async function scan() {
  loading.value = true
  error.value = ''
  savedCount.value = 0
  try {
    const data = await api.post('/api/scan', {
      timeoutMs: timeoutMs.value,
      keyword: keyword.value,
    })
    devices.value = data.devices || []
    if (devices.value.length > 0) {
      saveDevices(devices.value)
      savedCount.value = devices.value.length
    }
  } catch (e) {
    error.value = e.message
  } finally {
    loading.value = false
  }
}
</script>
