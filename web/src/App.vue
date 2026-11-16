<script setup lang="ts">
import { ConfigProvider, theme } from 'ant-design-vue'
import zhCN from 'ant-design-vue/es/locale/zh_CN'
</script>
<template>
  <ConfigProvider
    :locale="zhCN"
    :theme="{
      algorithm: theme.darkAlgorithm,
      token: {
        colorPrimary: '#43c6a8',
        colorInfo: '#78a9e8',
        colorSuccess: '#50d2b2',
        colorWarning: '#e5ad5c',
        colorError: '#ed7c82',
        colorBgBase: '#0d141b',
        colorBgContainer: '#121c25',
        colorBgElevated: '#18232d',
        colorBorder: '#2a3742',
        colorText: '#e4ecef',
        colorTextSecondary: '#96a5af',
        borderRadius: 8,
        fontFamily: 'Avenir Next, Segoe UI, PingFang SC, Microsoft YaHei, sans-serif',
      },
    }"
  >
    <RouterView />
  </ConfigProvider>
</template>
