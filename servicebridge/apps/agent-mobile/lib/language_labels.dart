/// 语言代码 → 中文名（与后台的语种标签保持一致）。
///
/// 说明：本文件**不依赖 Flutter**（原来内联在 `main.dart`），
/// 便于 `tool/profile_assertions.dart` 这类纯 Dart 脚本直接断言文案，
/// 同时 App 界面继续复用同一个函数，避免两份语言名表漂移。
library;

const Map<String, String> _languageNames = {
  'zh': '中文',
  'en': '英语',
  'ar': '阿拉伯语',
  'es': '西班牙语',
  'fr': '法语',
  'de': '德语',
  'pt': '葡萄牙语',
  'it': '意大利语',
  'nl': '荷兰语',
  'id': '印尼语',
  'vi': '越南语',
  'tr': '土耳其语',
  'pl': '波兰语',
  'ro': '罗马尼亚语',
  'ru': '俄语',
  'ja': '日语',
  'ko': '韩语',
  'th': '泰语',
  'he': '希伯来语',
  'hi': '印地语',
  'el': '希腊语',
};

String languageLabelFor(String code) {
  final key = code.trim().toLowerCase().split('-').first;
  if (key.isEmpty) return '';
  return _languageNames[key] ?? key.toUpperCase();
}
