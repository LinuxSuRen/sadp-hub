<template>
  <div>
    <el-card>
      <template #header>
        <div style="display: flex; align-items: center; gap: 12px">
          <span>设备库（localStorage）</span>
          <el-button @click="scanAndSave" :loading="loading">扫描并合并</el-button>
          <el-input v-model="keyword" placeholder="过滤（SN / IP / 型号）" style="width: 240px" clearable />
          <span style="color: var(--el-text-color-secondary)">{{ devices.length }} 台</span>
        </div>
      </template>
      <el-alert v-if="error" :title="error" type="error" show-icon style="margin-bottom: 12px" />
      <el-table :data="filtered" stripe>
        <el-table-column prop="serialNo" label="序列号" min-width="180" />
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
        <el-table-column prop="savedAt" label="保存时间" width="170">
          <template #default="{ row }">{{ fmtTime(row.savedAt) }}</template>
        </el-table-column>
        <el-table-column label="操作" width="160">
          <template #default="{ row }">
            <el-button size="small" type="primary" @click="openModify(row)">修改 IP</el-button>
            <el-button size="small" @click="remove(row)">移除</el-button>
          </template>
        </el-table-column>
        <template #empty>
          设备库为空——去「设备发现」扫描，或点「扫描并合并」，结果会自动保存在浏览器 localStorage
        </template>
      </el-table>
    </el-card>

    <el-dialog v-model="dialog" title="修改设备网络参数" width="560px">
      <el-alert v-if="target" type="info" :closable="false" style="margin-bottom: 12px"
        :title="`${target.model || target.serialNo}（${target.ipv4}）`"
        description="ISAPI 走设备 HTTP 接口（Digest 认证），新型号固件均支持；SADP 方言走 UDP，仅老固件（Encrypt=false）生效。" />
      <el-form :model="form" label-width="110px">
        <el-form-item label="修改方式">
          <el-radio-group v-model="form.via">
            <el-radio value="isapi">ISAPI（HTTP，推荐）</el-radio>
            <el-radio value="sadp">SADP（UDP，老固件）</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="DHCP">
          <el-switch v-model="form.dhcp" />
          <span style="margin-left: 8px; color: var(--el-text-color-secondary)">
            {{ form.dhcp ? '由路由器自动分配地址，无需填写' : '关闭则手动指定静态地址' }}
          </span>
        </el-form-item>
        <el-form-item label="新 IPv4">
          <el-input v-model="form.newIp" :disabled="form.dhcp" placeholder="DHCP 模式下由路由器分配" />
        </el-form-item>
        <el-form-item label="子网掩码">
          <el-input v-model="form.netMask" :disabled="form.dhcp" />
        </el-form-item>
        <el-form-item label="网关">
          <el-input v-model="form.gateway" :disabled="form.dhcp" />
        </el-form-item>
        <el-alert v-if="form.dhcp" type="warning" :closable="false"
          title="DHCP 模式下地址字段只读：值仅为参考显示（可点「读取当前配置」刷新），下发时不会写入，改完后请重新扫描确认新地址" />
        <el-form-item label="HTTP 端口"><el-input-number v-model="form.httpPort" :min="1" :max="65535" /></el-form-item>
        <el-form-item label="SDK 端口"><el-input-number v-model="form.sdkPort" :min="1" :max="65535" /></el-form-item>
        <el-form-item label="用户名"><el-input v-model="form.username" /></el-form-item>
        <el-form-item label="密码">
          <el-input v-model="form.password" type="password" show-password />
          <el-button style="margin-left: 8px" :loading="reading" @click="readNet">读取当前配置</el-button>
        </el-form-item>
        <el-form-item v-if="current" label="当前配置">
          <el-tag :type="current.addressingType === 'dhcp' ? 'warning' : 'success'" style="margin-right: 8px">
            {{ current.addressingType === 'dhcp' ? 'DHCP 自动获取' : '静态地址' }}
          </el-tag>
          <span>{{ current.ip || '—' }} / {{ current.netMask || '—' }} / 网关 {{ current.gateway || '—' }}</span>
          <span v-if="current.fromScan" style="margin-left: 8px; color: var(--el-text-color-secondary)">
            （来自扫描结果，可点「读取当前配置」获取实时值）
          </span>
        </el-form-item>
      </el-form>
      <el-alert v-if="result" :type="result.ok ? 'success' : 'error'" :closable="false" style="margin-top: 8px">
        {{ result.text }}
      </el-alert>
      <template #footer>
        <el-button @click="dialog = false">关闭</el-button>
        <el-button type="primary" :loading="modifying" @click="modify">下发</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { api } from '../api'
import { loadDevices, saveDevices, updateDevice, removeDevice } from '../devices'

const devices = ref([])
const keyword = ref('')
const loading = ref(false)
const error = ref('')
const dialog = ref(false)
const target = ref(null)
const form = ref({})
const modifying = ref(false)
const result = ref(null)
const reading = ref(false)
const current = ref(null)

async function readNet() {
  if (!form.value.password) {
    ElMessage.warning('请先输入设备密码')
    return
  }
  reading.value = true
  try {
    const resp = await api.post('/api/devicenet', {
      deviceIp: target.value.ipv4,
      httpPort: target.value.httpPort || 80,
      username: form.value.username,
      password: form.value.password,
    })
    if (resp.ok) {
      current.value = resp
      form.value.dhcp = resp.addressingType === 'dhcp'
      // DHCP 模式下也回填显示（只读参考）
      form.value.newIp = resp.ip || form.value.newIp
      form.value.netMask = resp.netMask || form.value.netMask
      form.value.gateway = resp.gateway || form.value.gateway
    } else {
      ElMessage.error(resp.error || '读取失败')
    }
  } catch (e) {
    ElMessage.error(e.message)
  } finally {
    reading.value = false
  }
}

const filtered = computed(() => {
  const kw = keyword.value.trim().toLowerCase()
  if (!kw) return devices.value
  return devices.value.filter((d) =>
    [d.serialNo, d.ipv4, d.model, d.mac].some((s) => (s || '').toLowerCase().includes(kw)))
})

async function scanAndSave() {
  loading.value = true
  error.value = ''
  try {
    const data = await api.post('/api/scan', { timeoutMs: 3000 })
    devices.value = saveDevices(data.devices || [])
    ElMessage.success(`已发现 ${data.count} 台并合并入库`)
  } catch (e) {
    error.value = e.message
  } finally {
    loading.value = false
  }
}

function openModify(row) {
  target.value = row
  form.value = {
    via: 'isapi',
    dhcp: !!row.dhcp,
    newIp: row.ipv4,
    netMask: row.netMask || '255.255.255.0',
    gateway: row.gateway || '',
    httpPort: row.httpPort || 80,
    sdkPort: row.sdkPort || 8000,
    username: 'admin',
    password: '',
  }
  result.value = null
  // 先用 SADP 扫描结果预显示寻址方式（读取后更新为设备实时值）
  current.value = {
    fromScan: true,
    addressingType: row.dhcp ? 'dhcp' : 'static',
    ip: row.ipv4,
    netMask: row.netMask,
    gateway: row.gateway,
  }
  dialog.value = true
}

async function modify() {
  modifying.value = true
  result.value = null
  try {
    const resp = await api.post('/api/modify', {
      deviceIp: target.value.ipv4,
      mac: target.value.mac,
      via: form.value.via,
      dhcp: form.value.dhcp,
      newIp: form.value.dhcp ? '' : form.value.newIp,
      newIp: form.value.newIp,
      netMask: form.value.netMask,
      gateway: form.value.gateway,
      httpPort: form.value.httpPort,
      sdkPort: form.value.sdkPort,
      username: form.value.username,
      password: form.value.password,
    })
    if (resp.ok) {
      result.value = { ok: true, text: '设备已接受修改，正在以新参数重启网络' }
      devices.value = updateDevice(target.value.serialNo, {
        ipv4: form.value.newIp,
        netMask: form.value.netMask,
        gateway: form.value.gateway,
        httpPort: form.value.httpPort,
        sdkPort: form.value.sdkPort,
      })
    } else {
      result.value = { ok: false, text: resp.error + (resp.reply ? `｜应答：${resp.reply.slice(0, 200)}` : '') }
    }
  } catch (e) {
    result.value = { ok: false, text: e.message }
  } finally {
    modifying.value = false
  }
}

async function remove(row) {
  await ElMessageBox.confirm(`从设备库移除「${row.model || row.serialNo}」？`, '确认', { type: 'warning' })
  devices.value = removeDevice(row.serialNo)
}

onMounted(() => {
  devices.value = loadDevices()
})

function fmtTime(v) {
  if (!v) return '—'
  return new Date(v).toLocaleString('zh-CN', { hour12: false })
}
</script>
