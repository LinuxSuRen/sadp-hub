<template>
  <el-card>
    <template #header>这是什么</template>
    <p>
      sadp-hub 是一个 SADP 风格的局域网设备发现工具（Go 单二进制 + 内嵌 Web 管理台）。
      基于 <b>UDP 组播 239.255.255.250:37020</b>：客户端组播 Probe（XML），
      设备<b>单播回源</b>应答序列号 / MAC / IP / 端口 / 激活状态——与海康 SADP、WS-Discovery 同类思路。
    </p>
    <ul>
      <li><b>设备发现</b>：向所有组播网卡发 Probe，按序列号去重聚合应答，支持关键字过滤</li>
      <li><b>协议兼容</b>：解析真实海康 SADP v3.x <code>ProbeMatch</code> 报文（SN / 型号 / 掩码 / 网关 / DHCP / 固件 / 激活状态）</li>
      <li><b>REST API</b>：<code>POST /api/scan</code>，供脚本集成</li>
    </ul>
    <div style="margin-top: 12px">
      <el-button type="primary" @click="goto('discover')">去发现设备</el-button>
    </div>
    <p style="color: var(--el-text-color-secondary)">
      组播发现无鉴权、仅暴露设备标识，与海康 SADP 的发现层思路一致；敏感操作请走独立鉴权通道。
    </p>
  </el-card>
</template>

<script setup>
const emit = defineEmits(['goto'])

function goto(page) {
  emit('goto', page)
}
</script>
