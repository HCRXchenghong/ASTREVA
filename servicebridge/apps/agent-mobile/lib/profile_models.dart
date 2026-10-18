/// 访客画像（P5）数据模型与展示文案计算。
///
/// 对应客服侧契约（后端并行实现中，按冻结契约实现）：
/// `GET /api/agent/conversations/{id}/profile` →
/// `{"profile":{visitor_id,country,region,city,timezone,nationality_guess,
///   nationality_confidence,nationality_basis,languages,dialect,code_switching,
///   summary,tags,interests,evidence,visits,last_path,last_seen_at,ip_last}}`
/// 403 表示会话不属于当前客服；404 表示未开启画像或访客无画像。
///
/// 本文件**不依赖 Flutter**，只做 JSON 解析与纯函数文案计算，
/// 便于 `tool/profile_assertions.dart` 用内置 Dart SDK 直接断言。
library;

/// 推测国籍依据代码 → 中文名。
const Map<String, String> _nationalityBasisNames = <String, String>{
  'ip_cdn': 'IP 国别',
  'geoip': '本地库',
  'third_party': '第三方库',
  'language': '语言',
  'timezone': '时区',
};

/// 常见 ISO 3166-1 alpha-2 国家/地区码 → 中文名。
/// 只覆盖高频来源地；未知码原样回显，避免显示空白。
const Map<String, String> _countryNames = <String, String>{
  'SA': '沙特阿拉伯',
  'AE': '阿联酋',
  'EG': '埃及',
  'MA': '摩洛哥',
  'DZ': '阿尔及利亚',
  'TN': '突尼斯',
  'QA': '卡塔尔',
  'KW': '科威特',
  'OM': '阿曼',
  'BH': '巴林',
  'JO': '约旦',
  'LB': '黎巴嫩',
  'IQ': '伊拉克',
  'IR': '伊朗',
  'IL': '以色列',
  'TR': '土耳其',
  'US': '美国',
  'CA': '加拿大',
  'MX': '墨西哥',
  'BR': '巴西',
  'AR': '阿根廷',
  'CL': '智利',
  'CO': '哥伦比亚',
  'PE': '秘鲁',
  'ES': '西班牙',
  'PT': '葡萄牙',
  'FR': '法国',
  'DE': '德国',
  'IT': '意大利',
  'NL': '荷兰',
  'BE': '比利时',
  'CH': '瑞士',
  'AT': '奥地利',
  'SE': '瑞典',
  'NO': '挪威',
  'DK': '丹麦',
  'FI': '芬兰',
  'PL': '波兰',
  'RO': '罗马尼亚',
  'CZ': '捷克',
  'GR': '希腊',
  'RU': '俄罗斯',
  'UA': '乌克兰',
  'KZ': '哈萨克斯坦',
  'GB': '英国',
  'IE': '爱尔兰',
  'IN': '印度',
  'PK': '巴基斯坦',
  'BD': '孟加拉国',
  'LK': '斯里兰卡',
  'CN': '中国',
  'HK': '中国香港',
  'TW': '中国台湾',
  'MO': '中国澳门',
  'JP': '日本',
  'KR': '韩国',
  'TH': '泰国',
  'VN': '越南',
  'MY': '马来西亚',
  'SG': '新加坡',
  'ID': '印度尼西亚',
  'PH': '菲律宾',
  'AU': '澳大利亚',
  'NZ': '新西兰',
  'ZA': '南非',
  'NG': '尼日利亚',
  'KE': '肯尼亚',
};

/// ISO 国家/地区码 → 中文名；空串返回空串，未知码原样回显。
String countryLabelFor(String code) {
  final key = code.trim().toUpperCase();
  if (key.isEmpty) return '';
  return _countryNames[key] ?? key;
}

/// 推测国籍依据代码 → 中文名；未知代码原样回显（便于暴露后端新增依据）。
String nationalityBasisLabel(String code) {
  final key = code.trim().toLowerCase();
  if (key.isEmpty) return '';
  return _nationalityBasisNames[key] ?? key;
}

/// `code_switching` 为真时的提示文案。
const String codeSwitchingHint = '中英/多语混说';

/// 后端未返回画像（404）时的文案：与「未开启画像功能」语义保持一致。
const String profileNotFoundText = '该访客暂无画像（可能未开启画像功能）';

/// 会话不属于当前客服（403）时的文案。
const String profileForbiddenText = '无权查看该会话画像';

/// 画像接口错误 → 界面可读文案。
/// 403/404 用固定中文文案，其余原样使用后端 message（为空时兜底）。
String profileErrorText({required int statusCode, required String message}) {
  if (statusCode == 403) return profileForbiddenText;
  if (statusCode == 404) return profileNotFoundText;
  final text = message.trim();
  return text.isEmpty ? '画像加载失败，请稍后重试' : text;
}

/// 一条「依据消息」：AI 摘要引用的真实消息。
class ProfileEvidence {
  const ProfileEvidence({
    required this.messageId,
    required this.text,
    this.createdAt,
  });

  factory ProfileEvidence.fromJson(Map<String, dynamic> json) =>
      ProfileEvidence(
        messageId: '${json['message_id'] ?? ''}'.trim(),
        text: '${json['text'] ?? ''}',
        // 缺 created_at 或格式非法时保持 null，界面显示「时间未知」。
        createdAt: DateTime.tryParse('${json['created_at'] ?? ''}'),
      );

  final String messageId;
  final String text;
  final DateTime? createdAt;

  /// 能否点击定位到消息列表（缺 message_id 时只做展示）。
  bool get canJump => messageId.isNotEmpty;
}

/// `evidence.created_at` 的展示文案；缺失时给出明确兜底，不显示空白。
String evidenceTimeLabel(DateTime? value) {
  if (value == null) return '时间未知';
  final local = value.toLocal();
  String two(int v) => v.toString().padLeft(2, '0');
  return '${local.year}-${two(local.month)}-${two(local.day)} '
      '${two(local.hour)}:${two(local.minute)}';
}

/// 单条语言占比（如 `es 12/15 → 80%`）。
class ProfileLanguageShare {
  const ProfileLanguageShare({
    required this.code,
    required this.count,
    required this.total,
  });

  final String code;
  final int count;

  /// 全部语言计数之和（占比分母）。
  final int total;

  /// 四舍五入到整数百分比；分母非法时为 0，不产生除零。
  int get percent => total <= 0 ? 0 : ((count * 100) / total).round();
}

/// 「语言中文名 xx%」文案；[labelFor] 由调用方注入（App 里传 `languageLabelFor`）。
/// 语言名不可得时回落到语言码，保证不出现「 80%」这类空名。
String languageShareText(
  ProfileLanguageShare share,
  String Function(String code) labelFor,
) {
  final name = labelFor(share.code).trim();
  return '${name.isEmpty ? share.code : name} ${share.percent}%';
}

/// 访客画像（按访客聚合、跨会话）。
class VisitorProfile {
  const VisitorProfile({
    this.visitorId = '',
    this.country = '',
    this.region = '',
    this.city = '',
    this.timezone = '',
    this.nationalityGuess = '',
    this.nationalityConfidence = 0,
    this.nationalityBasis = const <String>[],
    this.languages = const <String, int>{},
    this.dialect = '',
    this.codeSwitching = false,
    this.summary = '',
    this.tags = const <String>[],
    this.interests = const <String>[],
    this.evidence = const <ProfileEvidence>[],
    this.visits = 0,
    this.lastPath = '',
    this.lastSeenAt,
    this.ipLast = '',
  });

  /// 容错解析：任何字段缺失 / 类型异常都退化为默认值，绝不抛异常。
  factory VisitorProfile.fromJson(Map<String, dynamic> json) {
    final rawLanguages = json['languages'];
    final languages = <String, int>{};
    if (rawLanguages is Map) {
      rawLanguages.forEach((key, value) {
        final code = '$key'.trim();
        if (code.isEmpty) return;
        final count = value is num
            ? value.toInt()
            : int.tryParse('$value'.trim()) ?? 0;
        if (count <= 0) return;
        languages[code] = (languages[code] ?? 0) + count;
      });
    }
    return VisitorProfile(
      visitorId: '${json['visitor_id'] ?? ''}'.trim(),
      country: '${json['country'] ?? ''}'.trim(),
      region: '${json['region'] ?? ''}'.trim(),
      city: '${json['city'] ?? ''}'.trim(),
      timezone: '${json['timezone'] ?? ''}'.trim(),
      nationalityGuess: '${json['nationality_guess'] ?? ''}'.trim(),
      nationalityConfidence: _confidence(json['nationality_confidence']),
      nationalityBasis: _stringList(json['nationality_basis']),
      languages: languages,
      dialect: '${json['dialect'] ?? ''}'.trim(),
      codeSwitching: json['code_switching'] == true,
      summary: '${json['summary'] ?? ''}'.trim(),
      tags: _stringList(json['tags']),
      interests: _stringList(json['interests']),
      evidence: _evidenceList(json['evidence']),
      visits: (json['visits'] as num?)?.toInt() ?? 0,
      lastPath: '${json['last_path'] ?? ''}'.trim(),
      lastSeenAt: DateTime.tryParse('${json['last_seen_at'] ?? ''}'),
      ipLast: '${json['ip_last'] ?? ''}'.trim(),
    );
  }

  /// 置信度：兼容 num / 字符串，缺失为 0，并夹取到 0..1。
  static double _confidence(Object? raw) {
    final value = raw is num
        ? raw.toDouble()
        : double.tryParse('$raw'.trim()) ?? 0;
    if (value.isNaN) return 0;
    return value.clamp(0.0, 1.0);
  }

  static List<String> _stringList(Object? raw) {
    if (raw is! List) return const <String>[];
    return raw
        .map((item) => '$item'.trim())
        .where((item) => item.isNotEmpty)
        .toList();
  }

  static List<ProfileEvidence> _evidenceList(Object? raw) {
    if (raw is! List) return const <ProfileEvidence>[];
    return raw
        .whereType<Map>()
        .map((item) => ProfileEvidence.fromJson(Map<String, dynamic>.from(item)))
        .where((item) => item.text.trim().isNotEmpty || item.messageId.isNotEmpty)
        .toList();
  }

  final String visitorId;
  final String country;
  final String region;
  final String city;
  final String timezone;

  /// ISO 国家/地区码，可能是空串（无推测）。
  final String nationalityGuess;
  final double nationalityConfidence;
  final List<String> nationalityBasis;
  final Map<String, int> languages;
  final String dialect;
  final bool codeSwitching;
  final String summary;
  final List<String> tags;
  final List<String> interests;
  final List<ProfileEvidence> evidence;
  final int visits;
  final String lastPath;
  final DateTime? lastSeenAt;

  /// 后端返回什么就显示什么：`show_full_ip=false` 时后端已脱敏（如 `1.2.3.*`），
  /// App 不自行补全或遮罩。
  final String ipLast;

  /// 首行地理位置：`国家 · 地区 · 城市`，全空时给出兜底文案。
  String get geoLine {
    final parts = <String>[country, region, city]
        .where((item) => item.isNotEmpty)
        .toList();
    return parts.isEmpty ? '未获取到地理位置' : parts.join(' · ');
  }

  String get timezoneLine => timezone.isEmpty ? '时区未知' : timezone;

  bool get hasNationalityGuess => nationalityGuess.isNotEmpty;

  /// 置信度百分比（0..100 整数）。
  int get nationalityPercent => (nationalityConfidence * 100).round().clamp(0, 100);

  /// 「推测：沙特阿拉伯 · 62%」；无推测时给明确文案。
  /// **必须**保留「推测」二字（规格要求 UI 显式标注）。
  String get nationalityGuessLine => hasNationalityGuess
      ? '推测：${countryLabelFor(nationalityGuess)} · $nationalityPercent%'
      : '暂无国籍推测';

  /// 依据中文名列表（如 `IP 国别 / 语言`）。
  List<String> get nationalityBasisLabels => nationalityBasis
      .map(nationalityBasisLabel)
      .where((item) => item.isNotEmpty)
      .toList();

  String get basisLine => nationalityBasisLabels.isEmpty
      ? '依据：无'
      : '依据：${nationalityBasisLabels.join('、')}';

  /// 语言占比：只保留计数 > 0 的语言，按占比（计数）降序；
  /// 计数相同时按语言码升序，保证顺序稳定可测。
  List<ProfileLanguageShare> get languageShares {
    final total = languageTotal;
    final shares = languages.entries
        .where((entry) => entry.value > 0)
        .map(
          (entry) => ProfileLanguageShare(
            code: entry.key,
            count: entry.value,
            total: total,
          ),
        )
        .toList();
    shares.sort((a, b) {
      final byCount = b.count.compareTo(a.count);
      if (byCount != 0) return byCount;
      return a.code.compareTo(b.code);
    });
    return shares;
  }

  int get languageTotal =>
      languages.values.where((count) => count > 0).fold(0, (a, b) => a + b);

  bool get hasLanguageData => languageShares.isNotEmpty;

  bool get hasTags => tags.isNotEmpty;
  bool get hasInterests => interests.isNotEmpty;
  bool get hasSummary => summary.isNotEmpty;
  bool get hasEvidence => evidence.isNotEmpty;

  /// IP 展示文案：后端返回什么就显示什么，未返回时给兜底。
  String get ipLine => ipLast.isEmpty ? '未返回' : ipLast;

  /// 最近访问页面的展示文案。
  String get lastPathLine => lastPath.isEmpty ? '未记录' : lastPath;

  String get lastSeenLine =>
      lastSeenAt == null ? '未知' : evidenceTimeLabel(lastSeenAt!);
}
