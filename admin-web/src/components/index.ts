import { App } from 'vue';
import Breadcrumb from './breadcrumb/index.vue';

// 只注册后台真正用到的全局组件；Arco Pro 模板里的 ECharts 图表、消息中心等示例组件已移除。
export default {
  install(Vue: App) {
    Vue.component('Breadcrumb', Breadcrumb);
  },
};
