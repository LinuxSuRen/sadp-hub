// 设备库：发现结果按序列号合并保存在 localStorage
const KEY = 'sadp-hub.devices'

export function loadDevices() {
  try {
    return JSON.parse(localStorage.getItem(KEY)) || []
  } catch {
    return []
  }
}

// 按序列号合并（新扫描覆盖旧记录），返回总数
export function saveDevices(scanned) {
  const bySN = {}
  for (const d of loadDevices()) bySN[d.serialNo] = d
  for (const d of scanned || []) bySN[d.serialNo] = { ...d, savedAt: new Date().toISOString() }
  const list = Object.values(bySN)
  localStorage.setItem(KEY, JSON.stringify(list))
  return list
}

export function updateDevice(sn, patch) {
  const list = loadDevices().map((d) => (d.serialNo === sn ? { ...d, ...patch } : d))
  localStorage.setItem(KEY, JSON.stringify(list))
  return list
}

export function removeDevice(sn) {
  const list = loadDevices().filter((d) => d.serialNo !== sn)
  localStorage.setItem(KEY, JSON.stringify(list))
  return list
}
