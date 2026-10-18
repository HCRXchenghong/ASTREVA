/// 客服策略相关的纯数据模型（提醒渠道 / 接管事件 / SLA 事件）。
///
/// 本文件**不依赖 Flutter**，因此可以被界面层（main.dart）与临时断言脚本
/// 同时 import；解析与校验逻辑集中在这里，避免散落在 Widget 里。
library;

/// 渠道类型：telegram / bark / email（契约冻结）。
abstract final class AlertChannelKind {
  AlertChannelKind._();

  static const String telegram = 'telegram';
  static const String bark = 'bark';
  static const String email = 'email';

  static const List<String> all = <String>[telegram, bark, email];

  /// 后端若新增未知类型，[label] 会原样回显，界面不至于空白。
  static bool isKnown(String kind) => all.contains(kind);

  static String label(String kind) {
    switch (kind) {
      case telegram:
        return 'Telegram';
      case bark:
        return 'Bark';
      case email:
        return '邮箱';
      default:
        return kind.trim().isEmpty ? '未知渠道' : kind;
    }
  }
}

/// 一种渠道类型的单个表单字段。[name] 即提交给后端的 config key。
class AlertChannelField {
  const AlertChannelField({
    required this.name,
    required this.label,
    required this.hint,
    this.required = false,
    this.secret = false,
  });

  final String name;
  final String label;
  final String hint;

  /// 新增时必须填写。
  final bool required;

  /// 密钥类字段：密码框展示；编辑时留空表示不修改。
  final bool secret;
}

/// 各渠道类型的字段定义与固定文案。
abstract final class AlertChannelSpec {
  AlertChannelSpec._();

  /// bark 的 server 可选，后端默认 https://api.day.app。
  static const String barkDefaultServer = 'https://api.day.app';

  /// 邮箱渠道依赖后台 SMTP，测试失败时如实展示后端 message，并在表单里说明。
  static const String emailSmtpNote = '邮箱提醒依赖后台配置的 SMTP，若未配置请联系管理员';

  static const List<AlertChannelField> telegramFields = <AlertChannelField>[
    AlertChannelField(
      name: 'token',
      label: 'Bot Token',
      hint: 'Telegram Bot 的 Token，例如 123456:ABC-DEF',
      required: true,
      secret: true,
    ),
    AlertChannelField(
      name: 'chat_id',
      label: 'Chat ID',
      hint: '接收提醒的会话 ID，例如 -1001234567890',
      required: true,
    ),
  ];

  static const List<AlertChannelField> barkFields = <AlertChannelField>[
    AlertChannelField(
      name: 'key',
      label: '推送 Key',
      hint: 'Bark App 里显示的 Key',
      required: true,
      secret: true,
    ),
    AlertChannelField(
      name: 'server',
      label: '服务器地址（可选）',
      hint: '留空使用默认 $barkDefaultServer',
    ),
  ];

  static const List<AlertChannelField> emailFields = <AlertChannelField>[
    AlertChannelField(
      name: 'address',
      label: '收件地址',
      hint: '例如 agent@example.com',
      required: true,
    ),
  ];

  static List<AlertChannelField> fieldsFor(String kind) {
    switch (kind) {
      case AlertChannelKind.telegram:
        return telegramFields;
      case AlertChannelKind.bark:
        return barkFields;
      case AlertChannelKind.email:
        return emailFields;
      default:
        return const <AlertChannelField>[];
    }
  }
}

/// 渠道数量上限（契约：最多 3 个，第 4 个后端返回 400）。
abstract final class AlertChannelRules {
  AlertChannelRules._();

  static const int maxChannels = 3;

  /// 按钮禁用时的提示文案。
  static const String limitMessage = '最多 3 个提醒渠道';

  /// 已达上限时列表上方的说明文案。
  static const String limitDetail = '已达上限：最多 3 个提醒渠道，请先删除后再新增';

  static bool canAdd(int currentCount) => currentCount < maxChannels;
}

/// 一条已保存的提醒渠道（`GET /api/agent/alert-channels`）。
///
/// 注意：服务端只回传掩码 [masked]，不回传任何明文密钥，
/// 因此「发送测试」必须重新收集 config（见 AlertChannelsPage）。
class AlertChannel {
  const AlertChannel({
    required this.id,
    required this.kind,
    required this.enabled,
    required this.masked,
    this.createdAt,
  });

  factory AlertChannel.fromJson(Map<String, dynamic> json) => AlertChannel(
    id: '${json['id'] ?? ''}'.trim(),
    kind: '${json['kind'] ?? ''}'.trim(),
    // 字段缺失时按启用处理（与后端默认一致）。
    enabled: json['enabled'] != false,
    masked: '${json['masked'] ?? ''}'.trim(),
    createdAt: DateTime.tryParse('${json['created_at'] ?? ''}')?.toLocal(),
  );

  final String id;
  final String kind;
  final bool enabled;

  /// 服务端掩码，例如 ****1234；可能为空。
  final String masked;
  final DateTime? createdAt;

  String get kindLabel => AlertChannelKind.label(kind);

  String get maskedLabel => masked.isEmpty ? '未返回掩码' : masked;

  AlertChannel copyWith({bool? enabled}) => AlertChannel(
    id: id,
    kind: kind,
    enabled: enabled ?? this.enabled,
    masked: masked,
    createdAt: createdAt,
  );
}

/// `POST /api/agent/alert-channels/test` 的响应。
class AlertChannelTestResult {
  const AlertChannelTestResult({required this.ok, required this.message});

  factory AlertChannelTestResult.fromJson(Map<String, dynamic> json) =>
      AlertChannelTestResult(
        ok: json['ok'] == true,
        message: '${json['message'] ?? ''}'.trim(),
      );

  final bool ok;
  final String message;

  /// 后端 message 如实展示；缺失时给兜底文案，不吞掉失败。
  String get display => message.isEmpty
      ? (ok ? '测试消息已发送' : '测试失败，后端未返回失败原因')
      : message;
}

/// 表单草稿：把用户输入转换成契约要求的 config JSON，并做类型校验。
class AlertChannelDraft {
  AlertChannelDraft({required this.kind, Map<String, String>? values})
    : values = <String, String>{...?values};

  final String kind;
  final Map<String, String> values;

  String valueOf(String name) => (values[name] ?? '').trim();

  void setValue(String name, String value) => values[name] = value;

  /// 是否填了任意一个字段。
  bool get hasAnyValue =>
      values.values.any((value) => value.trim().isNotEmpty);

  /// 提交给后端的 config：只带非空字段。
  /// bark 的 server 留空时不提交，由后端使用默认地址。
  Map<String, dynamic> toConfigJson() {
    final config = <String, dynamic>{};
    for (final field in AlertChannelSpec.fieldsFor(kind)) {
      final value = valueOf(field.name);
      if (value.isEmpty) continue;
      config[field.name] = value;
    }
    return config;
  }

  /// 校验规则：
  /// - 新增 / 发送测试：[requireSecrets] 为 true，必填项不能为空；
  /// - 编辑：[requireSecrets] 为 false，密钥类字段留空表示不修改，
  ///   其余字段留空同样不提交（保持服务端原值）。
  /// 返回 null 表示通过，否则是可直接展示的中文错误。
  String? validate({bool requireSecrets = true}) {
    final fields = AlertChannelSpec.fieldsFor(kind);
    if (fields.isEmpty) return '不支持的渠道类型';
    for (final field in fields) {
      final value = valueOf(field.name);
      if (value.isEmpty) {
        final skippedOnEdit = field.secret && !requireSecrets;
        if (field.required && !skippedOnEdit) {
          return '请填写${field.label}';
        }
        continue;
      }
      if (field.name == 'address' && !isValidEmail(value)) {
        return '请输入有效的邮箱地址';
      }
      if (field.name == 'server' && !isValidServer(value)) {
        return '服务器地址需以 http:// 或 https:// 开头';
      }
    }
    return null;
  }

  static bool isValidEmail(String value) {
    final at = value.indexOf('@');
    if (at <= 0 || at != value.lastIndexOf('@')) return false;
    final domain = value.substring(at + 1);
    return domain.contains('.') &&
        !domain.startsWith('.') &&
        !domain.endsWith('.') &&
        !value.contains(' ');
  }

  static bool isValidServer(String value) =>
      value.startsWith('http://') || value.startsWith('https://');
}

/// 与后端无关的接收侧模型：接管请求事件。
class TakeoverRequest {
  const TakeoverRequest({
    required this.conversationId,
    this.visitorName = '',
    this.waitingSeconds,
  });

  factory TakeoverRequest.fromJson(Map<String, dynamic> json) => TakeoverRequest(
    conversationId: '${json['conversation_id'] ?? ''}'.trim(),
    visitorName: '${json['visitor_name'] ?? ''}'.trim(),
    waitingSeconds: _optionalInt(json['waiting_seconds']),
  );

  final String conversationId;
  final String visitorName;
  final int? waitingSeconds;

  /// 没有会话 ID 的事件无法定位，直接忽略。
  bool get isValid => conversationId.isNotEmpty;

  String get displayVisitorName => visitorName.isEmpty ? '访客' : visitorName;

  /// 等待时长文案；缺失或为负时返回 null。
  String? get waitingLabel {
    final seconds = waitingSeconds;
    if (seconds == null || seconds < 0) return null;
    if (seconds < 60) return '已等待 $seconds 秒';
    final minutes = seconds ~/ 60;
    final rest = seconds % 60;
    if (rest == 0) return '已等待 $minutes 分钟';
    return '已等待 $minutes 分 $rest 秒';
  }

  /// 会话列表行上的等待提示。
  String get listLabel {
    final label = waitingLabel;
    return label == null ? '访客等待接入' : '访客等待接入 · $label';
  }
}

/// SLA 提醒事件（`conversation.sla`）：只在 App 内提示，不发系统通知。
class SlaAlert {
  const SlaAlert({
    required this.conversationId,
    required this.kind,
    this.minutes,
  });

  static const String kindFirstResponse = 'first_response';
  static const String kindIdle = 'idle';

  factory SlaAlert.fromJson(Map<String, dynamic> json) => SlaAlert(
    conversationId: '${json['conversation_id'] ?? ''}'.trim(),
    kind: '${json['kind'] ?? ''}'.trim(),
    minutes: _optionalInt(json['minutes']),
  );

  final String conversationId;

  /// first_response / idle。
  final String kind;
  final int? minutes;

  bool get isValid =>
      conversationId.isNotEmpty &&
      (kind == kindFirstResponse || kind == kindIdle);

  /// 列表行上的橙色小标签。
  String get shortLabel => shortLabelFor(kind);

  static String shortLabelFor(String kind) {
    switch (kind) {
      case kindFirstResponse:
        return '首响超时';
      case kindIdle:
        return '待回复超时';
      default:
        return '响应超时';
    }
  }

  /// 会话页顶部提示条。
  String get bannerText {
    final value = minutes;
    if (value == null || value <= 0) return '$shortLabel，请尽快回复';
    return '$shortLabel $value 分钟，请尽快回复';
  }
}

/// 每个会话同一时刻只显示一条 SLA 提示：
/// 同一 kind 重复到达不重复触发，新的 kind 覆盖旧 kind。
class SlaAlertTracker {
  final Map<String, String> _visibleKind = <String, String>{};

  /// 返回 true 表示这是一条需要刷新界面的新提示。
  bool offers(String conversationId, String kind) {
    if (conversationId.isEmpty || kind.isEmpty) return false;
    if (_visibleKind[conversationId] == kind) return false;
    _visibleKind[conversationId] = kind;
    return true;
  }

  String? kindFor(String conversationId) => _visibleKind[conversationId];

  /// 客服回复后清除该会话的提示。
  void clear(String conversationId) => _visibleKind.remove(conversationId);

  void clearAll() => _visibleKind.clear();
}

/// 本次运行期间「已弹过接管弹窗」的会话集合：
/// 用户点「稍后」后不再重复打扰（不改变服务端状态）。
class TakeoverNoticeRegistry {
  final Set<String> _notified = <String>{};

  /// 首次标记返回 true，重复标记返回 false。
  bool markNotified(String conversationId) {
    if (conversationId.isEmpty) return false;
    return _notified.add(conversationId);
  }

  bool alreadyNotified(String conversationId) =>
      _notified.contains(conversationId);

  /// 接管成功后可以清理，便于后续再次提醒。
  void clear(String conversationId) => _notified.remove(conversationId);

  void clearAll() => _notified.clear();
}

/// 后端可能把整数下发成 num 或字符串，这里统一转换。
int? _optionalInt(Object? value) {
  if (value is num) return value.toInt();
  if (value is String) return int.tryParse(value.trim());
  return null;
}
