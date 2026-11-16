import { createApp } from 'vue'
import { createPinia } from 'pinia'
import {
  Alert,
  Badge,
  Button,
  ConfigProvider,
  Checkbox,
  Descriptions,
  Drawer,
  Empty,
  Form,
  Input,
  InputNumber,
  Progress,
  Radio,
  Select,
  Space,
  Table,
  Tabs,
  Tag,
  Timeline,
  Tooltip,
} from 'ant-design-vue'
import 'ant-design-vue/dist/reset.css'
import '@vue-flow/core/dist/style.css'
import '@vue-flow/core/dist/theme-default.css'
import '@vue-flow/controls/dist/style.css'
import '@/assets/styles/main.css'
import App from '@/App.vue'
import router from '@/router'

const app = createApp(App).use(createPinia()).use(router)
for (const component of [
  Alert,
  Badge,
  Button,
  ConfigProvider,
  Checkbox,
  Descriptions,
  Drawer,
  Empty,
  Form,
  Input,
  InputNumber,
  Progress,
  Radio,
  Select,
  Space,
  Table,
  Tabs,
  Tag,
  Timeline,
  Tooltip,
])
  app.use(component)
app.mount('#app')
