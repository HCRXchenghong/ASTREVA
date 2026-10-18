// 临时自查脚本：只依赖纯 Dart 的 lib/alert_models.dart，不启动 Flutter。
//
// 运行：
//   /Users/seron-cheng/development/flutter/bin/cache/dart-sdk/bin/dart \
//     run tool/contract_assertions.dart
//
// 覆盖：渠道 config 序列化、类型/必填校验、上限判断、事件解析、去重规则。
// ignore_for_file: avoid_print

import 'dart:convert';

import 'package:online_customer_service/alert_models.dart';

int _passed = 0;
int _failed = 0;

void _check(String name, bool Function() body) {
  try {
    final ok = body();
    if (ok) {
      _passed++;
      print('PASS  $name');
    } else {
      _failed++;
      print('FAIL  $name');
    }
  } catch (error) {
    _failed++;
    print('FAIL  $name -> 抛出异常：$error');
  }
}

void _eq(String name, Object? actual, Object? expected) {
  _check('$name（期望 $expected，实际 $actual）', () => actual == expected);
}

Map<String, dynamic> _json(String text) =>
    Map<String, dynamic>.from(jsonDecode(text) as Map);

void main() {
  // 1. 渠道类型的合法性与中文名。
  _eq('已知类型 telegram 合法', AlertChannelKind.isKnown('telegram'), true);
  _eq('未知类型 wechat 不合法', AlertChannelKind.isKnown('wechat'), false);
  _eq('email 中文名', AlertChannelKind.label('email'), '邮箱');
  _eq('未知类型原样回显', AlertChannelKind.label('wechat'), 'wechat');

  // 2. 渠道列表解析：掩码、启停、创建时间。
  final channel = AlertChannel.fromJson(
    _json(
      '{"id":"c1","kind":"telegram","enabled":false,'
      '"masked":"****1234","created_at":"2026-05-01T10:00:00Z"}',
    ),
  );
  _eq('渠道 id', channel.id, 'c1');
  _eq('渠道 enabled=false', channel.enabled, false);
  _eq('渠道掩码', channel.maskedLabel, '****1234');
  _eq('渠道 created_at 可解析', channel.createdAt != null, true);

  // 3. 字段缺失时的兜底：enabled 默认 true，掩码兜底文案。
  final sparse = AlertChannel.fromJson(_json('{"id":"c2","kind":"bark"}'));
  _eq('缺 enabled 默认启用', sparse.enabled, true);
  _eq('缺掩码兜底文案', sparse.maskedLabel, '未返回掩码');
  _eq('copyWith 只改 enabled', sparse.copyWith(enabled: false).masked, '');

  // 4. 各类型的 config 字段定义。
  _eq(
    'telegram 字段',
    AlertChannelSpec.fieldsFor('telegram').map((f) => f.name).join(','),
    'token,chat_id',
  );
  _eq(
    'bark 字段',
    AlertChannelSpec.fieldsFor('bark').map((f) => f.name).join(','),
    'key,server',
  );
  _eq(
    'email 字段',
    AlertChannelSpec.fieldsFor('email').map((f) => f.name).join(','),
    'address',
  );
  _eq(
    'telegram token 为密钥字段',
    AlertChannelSpec.fieldsFor('telegram').first.secret,
    true,
  );
  _eq(
    'email address 非密钥字段',
    AlertChannelSpec.fieldsFor('email').first.secret,
    false,
  );
  _eq('未知类型无字段', AlertChannelSpec.fieldsFor('wechat').isEmpty, true);

  // 5. telegram config 序列化。
  final telegramDraft = AlertChannelDraft(kind: 'telegram')
    ..setValue('token', ' 123456:ABC ')
    ..setValue('chat_id', '-1001234567890');
  _eq(
    'telegram config JSON',
    jsonEncode(telegramDraft.toConfigJson()),
    '{"token":"123456:ABC","chat_id":"-1001234567890"}',
  );
  _eq('telegram 新增校验通过', telegramDraft.validate(), null);
  _eq(
    'telegram config 值均为字符串',
    telegramDraft.toConfigJson().values.every((v) => v is String),
    true,
  );

  // 6. 必填校验：缺 token。
  final missingToken = AlertChannelDraft(kind: 'telegram')
    ..setValue('chat_id', '-100');
  _eq('缺 Bot Token 报错', missingToken.validate(), '请填写Bot Token');
  _eq('缺 Bot Token 时 config 只含有值的字段', missingToken.toConfigJson().keys.join(','), 'chat_id');

  // 7. bark：server 可选，留空不提交；填了就必须是 http(s)。
  final barkDefault = AlertChannelDraft(kind: 'bark')..setValue('key', 'abc123');
  _eq('bark 只提交 key', jsonEncode(barkDefault.toConfigJson()), '{"key":"abc123"}');
  _eq('bark 留空 server 校验通过', barkDefault.validate(), null);
  final barkServer = AlertChannelDraft(kind: 'bark')
    ..setValue('key', 'abc123')
    ..setValue('server', 'https://api.day.app');
  _eq(
    'bark 自定义 server 提交',
    jsonEncode(barkServer.toConfigJson()),
    '{"key":"abc123","server":"https://api.day.app"}',
  );
  final badServer = AlertChannelDraft(kind: 'bark')
    ..setValue('key', 'abc123')
    ..setValue('server', 'api.day.app');
  _eq('bark server 缺协议报错', badServer.validate(), '服务器地址需以 http:// 或 https:// 开头');

  // 8. email：地址格式校验。
  final goodEmail = AlertChannelDraft(kind: 'email')
    ..setValue('address', 'agent@example.com');
  _eq('邮箱格式通过', goodEmail.validate(), null);
  _eq(
    '邮箱 config JSON',
    jsonEncode(goodEmail.toConfigJson()),
    '{"address":"agent@example.com"}',
  );
  final badEmail = AlertChannelDraft(kind: 'email')
    ..setValue('address', 'agent@localhost');
  _eq('邮箱缺少域名点报错', badEmail.validate(), '请输入有效的邮箱地址');
  final emptyEmail = AlertChannelDraft(kind: 'email');
  _eq('邮箱必填报错', emptyEmail.validate(), '请填写收件地址');

  // 9. 编辑模式（留空不修改）：密钥留空合法且不提交该字段。
  final editDraft = AlertChannelDraft(kind: 'telegram')
    ..setValue('token', '')
    ..setValue('chat_id', '-100999');
  _eq('编辑时密钥留空校验通过', editDraft.validate(requireSecrets: false), null);
  _eq(
    '编辑时密钥留空不提交',
    jsonEncode(editDraft.toConfigJson()),
    '{"chat_id":"-100999"}',
  );
  _eq(
    '编辑时必填项缺失仍报错',
    AlertChannelDraft(kind: 'email').validate(requireSecrets: false),
    '请填写收件地址',
  );
  _eq(
    '编辑时已填邮箱仍校验格式',
    (AlertChannelDraft(kind: 'email')..setValue('address', 'bad@')).validate(
      requireSecrets: false,
    ),
    '请输入有效的邮箱地址',
  );

  // 10. 未知类型无法提交。
  _eq(
    '未知类型校验失败',
    AlertChannelDraft(kind: 'wechat').validate(),
    '不支持的渠道类型',
  );

  // 11. 上限判断（最多 3 个）。
  _eq('0 个可新增', AlertChannelRules.canAdd(0), true);
  _eq('2 个可新增', AlertChannelRules.canAdd(2), true);
  _eq('3 个不可新增', AlertChannelRules.canAdd(3), false);
  _eq('上限文案', AlertChannelRules.limitMessage, '最多 3 个提醒渠道');

  // 12. 测试响应解析：失败时 message 原样保留。
  final testOk = AlertChannelTestResult.fromJson(
    _json('{"ok":true,"message":"测试消息已发送"}'),
  );
  _eq('测试成功 ok', testOk.ok, true);
  _eq('测试成功 display', testOk.display, '测试消息已发送');
  final testFail = AlertChannelTestResult.fromJson(
    _json('{"ok":false,"message":"SMTP 未配置"}'),
  );
  _eq('测试失败 ok', testFail.ok, false);
  _eq('测试失败 display 原样', testFail.display, 'SMTP 未配置');
  _eq(
    '测试失败缺 message 兜底',
    AlertChannelTestResult.fromJson(_json('{"ok":false}')).display,
    '测试失败，后端未返回失败原因',
  );

  // 13. 接管事件解析与等待时长格式化。
  final takeover = TakeoverRequest.fromJson(
    _json('{"conversation_id":"cv1","visitor_name":"小明","waiting_seconds":95}'),
  );
  _eq('接管事件会话 ID', takeover.conversationId, 'cv1');
  _eq('接管事件访客名', takeover.displayVisitorName, '小明');
  _eq('等待 95 秒文案', takeover.waitingLabel, '已等待 1 分 35 秒');
  _eq('列表等待提示', takeover.listLabel, '访客等待接入 · 已等待 1 分 35 秒');
  _eq(
    '等待不足 1 分钟',
    TakeoverRequest.fromJson(_json('{"conversation_id":"cv1","waiting_seconds":45}'))
        .waitingLabel,
    '已等待 45 秒',
  );
  _eq(
    '等待整分钟',
    TakeoverRequest.fromJson(_json('{"conversation_id":"cv1","waiting_seconds":120}'))
        .waitingLabel,
    '已等待 2 分钟',
  );
  _eq(
    '等待秒数字符串也能解析',
    TakeoverRequest.fromJson(_json('{"conversation_id":"cv1","waiting_seconds":"90"}'))
        .waitingLabel,
    '已等待 1 分 30 秒',
  );
  final takeoverNoTime = TakeoverRequest.fromJson(
    _json('{"conversation_id":"cv1"}'),
  );
  _eq('无等待时长返回 null', takeoverNoTime.waitingLabel, null);
  _eq('无等待时长列表文案', takeoverNoTime.listLabel, '访客等待接入');
  _eq('无访客名兜底', takeoverNoTime.displayVisitorName, '访客');
  _eq(
    '缺少会话 ID 视为非法',
    TakeoverRequest.fromJson(_json('{"visitor_name":"小明"}')).isValid,
    false,
  );

  // 14. SLA 事件解析与文案。
  final firstResponse = SlaAlert.fromJson(
    _json('{"conversation_id":"cv1","kind":"first_response","minutes":5}'),
  );
  _eq('SLA 事件合法', firstResponse.isValid, true);
  _eq('SLA 列表标签', firstResponse.shortLabel, '首响超时');
  _eq('SLA 提示条', firstResponse.bannerText, '首响超时 5 分钟，请尽快回复');
  final idle = SlaAlert.fromJson(
    _json('{"conversation_id":"cv1","kind":"idle"}'),
  );
  _eq('待回复超时标签', idle.shortLabel, '待回复超时');
  _eq('缺 minutes 的提示条', idle.bannerText, '待回复超时，请尽快回复');
  _eq(
    '未知 kind 非法',
    SlaAlert.fromJson(_json('{"conversation_id":"cv1","kind":"x"}')).isValid,
    false,
  );
  _eq(
    '缺会话 ID 非法',
    SlaAlert.fromJson(_json('{"kind":"idle"}')).isValid,
    false,
  );
  _eq('静态标签映射', SlaAlert.shortLabelFor('first_response'), '首响超时');

  // 15. SLA 去重：同一会话同一 kind 只提示一次，可被新 kind 覆盖。
  final slaTracker = SlaAlertTracker();
  _eq('首次 first_response 提示', slaTracker.offers('cv1', 'first_response'), true);
  _eq('重复 first_response 不再提示', slaTracker.offers('cv1', 'first_response'), false);
  _eq('新 kind 覆盖并提示', slaTracker.offers('cv1', 'idle'), true);
  _eq('覆盖后当前 kind', slaTracker.kindFor('cv1'), 'idle');
  _eq('回到 first_response 仍会提示', slaTracker.offers('cv1', 'first_response'), true);
  slaTracker.clear('cv1');
  _eq('清除后可再次提示', slaTracker.offers('cv1', 'idle'), true);
  _eq('空会话 ID 不提示', slaTracker.offers('', 'idle'), false);
  _eq('空 kind 不提示', slaTracker.offers('cv2', ''), false);

  // 16. 接管弹窗去重：同一会话只弹一次。
  final notices = TakeoverNoticeRegistry();
  _eq('首次弹窗', notices.markNotified('cv1'), true);
  _eq('重复不再弹窗', notices.markNotified('cv1'), false);
  _eq('已记录', notices.alreadyNotified('cv1'), true);
  notices.clear('cv1');
  _eq('清除后可再弹', notices.markNotified('cv1'), true);
  _eq('空会话 ID 不弹窗', notices.markNotified(''), false);

  print('\n合计：$_passed 项通过，$_failed 项失败');
  if (_failed > 0) throw StateError('解析断言失败：$_failed 项');
}
