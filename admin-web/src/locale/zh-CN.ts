import localeSettings from './zh-CN/settings';

// 后台菜单标题直接使用中文 meta.title：vue-i18n 找不到 key 时会原样返回，
// 因此这里只保留框架自身用到的少量文案与设置项。
export default {
  'menu.dashboard': '工作台',
  'navbar.action.locale': '切换为中文',
  'messageBox.switchRoles': '切换角色',
  'messageBox.userCenter': '用户中心',
  'messageBox.userSettings': '用户设置',
  'messageBox.logout': '退出登录',
  ...localeSettings,
};
