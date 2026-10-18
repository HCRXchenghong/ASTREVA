// 临时自查脚本：访客画像（P5）解析与展示文案断言，纯 Dart、不启动 Flutter。
//
// 运行：
//   /Users/seron-cheng/development/flutter/bin/cache/dart-sdk/bin/dart \
//     run tool/profile_assertions.dart
//
// 覆盖：画像 JSON 容错解析（缺字段 / 空 languages / 置信度 0 与 1 / tags 与
// interests 为 null / evidence 缺 created_at）、百分比与排序、依据中文名映射、
// 语言占比文案、403/404 错误文案、IP 原样透传。
// ignore_for_file: avoid_print

import 'dart:convert';

import 'package:online_customer_service/language_labels.dart';
import 'package:online_customer_service/profile_models.dart';

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

VisitorProfile _profile(String text) =>
    VisitorProfile.fromJson(_json(text));

void main() {
  // 1. 完整契约 JSON 的解析（含后端已脱敏的 IP）。
  final full = _profile('''
{
  "visitor_id":"v1","country":"沙特阿拉伯","region":"利雅得省","city":"利雅得",
  "timezone":"Asia/Riyadh","nationality_guess":"SA","nationality_confidence":0.62,
  "nationality_basis":["ip_cdn","language","timezone"],
  "languages":{"es":12,"en":3},"dialect":"es-419","code_switching":true,
  "summary":"关注批量采购与交期。","tags":["批发","价格敏感"],
  "interests":["太阳能逆变器"],
  "evidence":[{"message_id":"m1","text":"我要 500 台","created_at":"2026-05-01T10:00:00Z"}],
  "visits":3,"last_path":"/products/","last_seen_at":"2026-05-02T03:04:05Z",
  "ip_last":"1.2.3.*"
}
''');
  _eq('访客 id', full.visitorId, 'v1');
  _eq('国家/地区/城市首行', full.geoLine, '沙特阿拉伯 · 利雅得省 · 利雅得');
  _eq('时区行', full.timezoneLine, 'Asia/Riyadh');
  _eq('推测国籍 ISO 码', full.nationalityGuess, 'SA');
  _eq('推测国籍文案带「推测」与置信度', full.nationalityGuessLine, '推测：沙特阿拉伯 · 62%');
  _eq('国籍置信度百分比取整', full.nationalityPercent, 62);
  _eq('依据中文名映射', full.nationalityBasisLabels.join('、'), 'IP 国别、语言、时区');
  _eq('依据行文案', full.basisLine, '依据：IP 国别、语言、时区');
  _eq('语言总数', full.languageTotal, 15);
  _eq('语言占比条数（只保留计数 > 0）', full.languageShares.length, 2);
  _eq('占比分母', full.languageShares.first.total, 15);
  _eq('code_switching 提示文案', codeSwitchingHint, '中英/多语混说');
  _eq('方言标签用于展示', full.dialect, 'es-419');
  _eq('标签', full.tags.join(','), '批发,价格敏感');
  _eq('兴趣', full.interests.join(','), '太阳能逆变器');
  _eq('摘要全文', full.summary, '关注批量采购与交期。');
  _eq('依据消息 id', full.evidence.first.messageId, 'm1');
  _eq('依据消息可定位', full.evidence.first.canJump, true);
  _eq('访问次数', full.visits, 3);
  _eq('最近访问页面', full.lastPathLine, '/products/');
  _eq('IP 原样透传（不补全、不遮罩）', full.ipLine, '1.2.3.*');
  _eq('最近活跃时间（本地时区）', full.lastSeenLine.isNotEmpty, true);

  // 2. 缺字段 / 类型异常：不能抛异常，全部退化为安全默认值。
  final sparse = VisitorProfile.fromJson(_json('{}'));
  _eq('缺字段：国家行兜底', sparse.geoLine, '未获取到地理位置');
  _eq('缺字段：时区兜底', sparse.timezoneLine, '时区未知');
  _eq('缺字段：无推测文案', sparse.nationalityGuessLine, '暂无国籍推测');
  _eq('缺字段：无推测不显示百分比', sparse.nationalityPercent, 0);
  _eq('缺字段：languages 为空', sparse.languages.isEmpty, true);
  _eq('缺字段：无语言数据', sparse.hasLanguageData, false);
  _eq('缺字段：无标签', sparse.hasTags, false);
  _eq('缺字段：无兴趣', sparse.hasInterests, false);
  _eq('缺字段：无摘要', sparse.hasSummary, false);
  _eq('缺字段：无依据消息', sparse.hasEvidence, false);
  _eq('缺字段：IP 兜底', sparse.ipLine, '未返回');
  _eq('缺字段：最近页面兜底', sparse.lastPathLine, '未记录');
  _eq('缺字段：最近活跃兜底', sparse.lastSeenLine, '未知');
  _eq('缺字段：依据行兜底', sparse.basisLine, '依据：无');

  // 3. languages 为空对象 / tags 与 interests 为 null / 非法置信度。
  final nullLists = _profile(
    '{"languages":{},"tags":null,"interests":null,"nationality_confidence":null}',
  );
  _eq('languages 空对象：无占比', nullLists.languageShares.isEmpty, true);
  _eq('languages 空对象：总数为 0', nullLists.languageTotal, 0);
  _eq('tags 为 null：空列表', nullLists.tags, const <String>[]);
  _eq('interests 为 null：空列表', nullLists.interests, const <String>[]);
  _eq('置信度为 null：按 0 处理', nullLists.nationalityConfidence, 0);
  _eq(
    '标签 / 兴趣非字符串时转字符串并过滤空白',
    '${_profile('{"tags":[1,"vip","  "],"interests":["a b"]}').tags.join(',')}'
        '|${_profile('{"interests":["a b","","x"]}').interests.join(',')}',
    '1,vip|a b,x',
  );

  // 4. 置信度边界 0 / 1，以及越界值的夹取。
  final zero = _profile('{"nationality_guess":"SA","nationality_confidence":0}');
  _eq('置信度 0 文案', zero.nationalityGuessLine, '推测：沙特阿拉伯 · 0%');
  _eq('置信度 0 百分比', zero.nationalityPercent, 0);
  final one = _profile('{"nationality_guess":"US","nationality_confidence":1}');
  _eq('置信度 1 文案', one.nationalityGuessLine, '推测：美国 · 100%');
  _eq('置信度 1 百分比', one.nationalityPercent, 100);
  final over = _profile(
    '{"nationality_guess":"US","nationality_confidence":1.8}',
  );
  _eq('置信度越界夹到 1', over.nationalityPercent, 100);
  final negative = _profile(
    '{"nationality_guess":"US","nationality_confidence":-0.5}',
  );
  _eq('置信度为负夹到 0', negative.nationalityPercent, 0);
  final numericString = _profile('{"nationality_confidence":"0.62"}');
  _eq('置信度字符串可解析', numericString.nationalityPercent, 62);
  _eq(
    '置信度缺失且无猜测：不显示百分比',
    _profile('{"nationality_guess":""}').nationalityGuessLine,
    '暂无国籍推测',
  );

  // 5. 百分比与排序：按计数降序，同计数按语言码升序。
  final sorted = _profile('{"languages":{"en":3,"es":12,"zh":12,"ar":1}}');
  _eq(
    '语言排序（占比降序，同占比按码升序）',
    sorted.languageShares.map((s) => s.code).join(','),
    'es,zh,en,ar',
  );
  _eq('12/28 四舍五入', sorted.languageShares.first.percent, 43);
  _eq('3/28 四舍五入', sorted.languageShares[2].percent, 11);
  _eq('1/28 四舍五入', sorted.languageShares.last.percent, 4);
  final zeroCounts = _profile('{"languages":{"es":0,"en":0}}');
  _eq('计数为 0 的语言不展示', zeroCounts.languageShares.isEmpty, true);
  _eq('计数为 0 时分母为 0，不除零', zeroCounts.languageTotal, 0);
  final oddTotal = _profile('{"languages":{"es":1,"en":2}}');
  _eq('1/3 四舍五入为 33', oddTotal.languageShares.last.percent, 33);
  _eq('2/3 四舍五入为 67', oddTotal.languageShares.first.percent, 67);

  // 6. 语言占比文案：复用 languageLabelFor（语言码 → 中文名）。
  final shares = _profile('{"languages":{"es":12,"en":3,"xx":1}}').languageShares;
  _eq(
    '语言占比文案',
    shares.map((s) => languageShareText(s, languageLabelFor)).join(' / '),
    '西班牙语 75% / 英语 19% / XX 6%',
  );
  _eq(
    '方言码 es-419 归到基础语言名',
    languageShareText(
      const ProfileLanguageShare(code: 'es-419', count: 1, total: 1),
      languageLabelFor,
    ),
    '西班牙语 100%',
  );
  _eq(
    '语言名不可得时回落到语言码',
    languageShareText(
      const ProfileLanguageShare(code: 'es', count: 1, total: 1),
      (code) => '',
    ),
    'es 100%',
  );
  _eq('语言中文名复用 languageLabelFor', languageLabelFor('es'), '西班牙语');
  _eq('未知语言码大写回显', languageLabelFor('xx'), 'XX');
  _eq('空语言码标签为空', languageLabelFor(''), '');

  // 7. evidence：缺 created_at 只影响时间文案，不影响条目本身。
  final missingTime = _profile(
    '{"evidence":[{"message_id":"m9","text":"hello"}]}',
  );
  _eq('evidence 缺 created_at 仍解析', missingTime.evidence.length, 1);
  _eq('evidence 缺 created_at 时时间未知', missingTime.evidence.first.createdAt, null);
  _eq('evidence 时间文案兜底', evidenceTimeLabel(null), '时间未知');
  _eq(
    'evidence 时间文案格式化',
    evidenceTimeLabel(DateTime.utc(2026, 5, 1, 10, 30)),
    evidenceTimeLabel(DateTime.utc(2026, 5, 1, 10, 30).toLocal()),
  );
  _eq(
    'evidence 缺 message_id 时不可定位',
    _profile('{"evidence":[{"text":"无 id"}]}').evidence.first.canJump,
    false,
  );
  _eq(
    'evidence 非 Map 元素被忽略',
    _profile('{"evidence":["bad",{"message_id":"m1","text":"ok"}]}')
        .evidence
        .length,
    1,
  );
  _eq('evidence 为 null：空列表', _profile('{"evidence":null}').evidence, const <ProfileEvidence>[]);

  // 8. 依据中文名映射：已知码、别名、未知码、空码。
  _eq('依据 ip_cdn', nationalityBasisLabel('ip_cdn'), 'IP 国别');
  _eq('依据 geoip', nationalityBasisLabel('geoip'), '本地库');
  _eq('依据 third_party', nationalityBasisLabel('third_party'), '第三方库');
  _eq('依据 language', nationalityBasisLabel('language'), '语言');
  _eq('依据 timezone', nationalityBasisLabel('timezone'), '时区');
  _eq('依据未知码原样回显', nationalityBasisLabel('new_source'), 'new_source');
  _eq('依据空码为空', nationalityBasisLabel('  '), '');
  _eq(
    '依据列表过滤空值',
    _profile('{"nationality_basis":["ip_cdn","","language"]}')
        .nationalityBasisLabels
        .join(','),
    'IP 国别,语言',
  );
  _eq(
    '依据非列表时按空处理',
    _profile('{"nationality_basis":"ip_cdn"}').nationalityBasisLabels.isEmpty,
    true,
  );

  // 9. 国家码 → 中文名（推测国籍展示）。
  _eq('SA → 沙特阿拉伯', countryLabelFor('SA'), '沙特阿拉伯');
  _eq('小写码归一化', countryLabelFor('us'), '美国');
  _eq('未知国家码原样回显', countryLabelFor('ZZ'), 'ZZ');
  _eq('空国家码为空', countryLabelFor(''), '');

  // 10. 403 / 404 / 其它错误的界面文案。
  _eq('403 文案', profileErrorText(statusCode: 403, message: 'forbidden'), profileForbiddenText);
  _eq('404 文案', profileErrorText(statusCode: 404, message: 'not found'), profileNotFoundText);
  _eq(
    '其它错误用后端 message',
    profileErrorText(statusCode: 500, message: '上游超时'),
    '上游超时',
  );
  _eq(
    '无 message 时兜底',
    profileErrorText(statusCode: 500, message: '   '),
    '画像加载失败，请稍后重试',
  );
  _eq('404 文案含未开启画像提示', profileNotFoundText.contains('未开启画像'), true);

  print('\n合计：$_passed 项通过，$_failed 项失败');
  if (_failed > 0) throw StateError('访客画像断言失败：$_failed 项');
}
