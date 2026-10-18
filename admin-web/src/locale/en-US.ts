import localeSettings from './en-US/settings';

// 后台以中文为主，英文包只保留框架自身用到的少量文案。
export default {
  'menu.dashboard': 'Workspace',
  'navbar.action.locale': 'Switch to English',
  'messageBox.switchRoles': 'Switch roles',
  'messageBox.userCenter': 'User center',
  'messageBox.userSettings': 'User settings',
  'messageBox.logout': 'Logout',
  ...localeSettings,
};
