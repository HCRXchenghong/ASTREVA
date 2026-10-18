class AgentProfile {
  const AgentProfile({
    required this.id,
    required this.account,
    required this.name,
    required this.group,
    required this.status,
    required this.avatarUrl,
  });

  factory AgentProfile.fromJson(Map<String, dynamic> json) => AgentProfile(
    id: '${json['id'] ?? ''}',
    account: '${json['account'] ?? ''}',
    name: '${json['name'] ?? ''}',
    group: '${json['group'] ?? ''}',
    status: '${json['status'] ?? 'offline'}',
    avatarUrl: '${json['avatar_url'] ?? ''}',
  );

  final String id;
  final String account;
  final String name;
  final String group;
  final String status;
  final String avatarUrl;

  AgentProfile copyWith({String? status, String? avatarUrl}) => AgentProfile(
    id: id,
    account: account,
    name: name,
    group: group,
    status: status ?? this.status,
    avatarUrl: avatarUrl ?? this.avatarUrl,
  );
}

class Conversation {
  const Conversation({
    required this.id,
    required this.visitorLabel,
    required this.status,
    required this.lastMessage,
    required this.updatedAt,
    required this.unread,
    this.remark = '',
    this.assignedAgentName = '',
    this.assignedAgentAvatar = '',
  });

  factory Conversation.fromJson(Map<String, dynamic> json) => Conversation(
    id: '${json['id'] ?? ''}',
    visitorLabel: '${json['visitor_remark'] ?? json['visitor_ip'] ?? '访客'}',
    status: '${json['status'] ?? 'waiting'}',
    lastMessage: '${json['last_message'] ?? ''}',
    updatedAt: DateTime.tryParse('${json['updated_at'] ?? ''}')?.toLocal(),
    unread: (json['unread_for_agent'] as num?)?.toInt() ?? 0,
    remark: '${json['visitor_remark'] ?? ''}',
    // 依赖后端 A7：会话对象新增 assigned_agent_name / assigned_agent_avatar。
    assignedAgentName: '${json['assigned_agent_name'] ?? ''}',
    assignedAgentAvatar: '${json['assigned_agent_avatar'] ?? ''}',
  );

  final String id;
  final String visitorLabel;
  final String status;
  final String lastMessage;
  final DateTime? updatedAt;
  final int unread;
  final String remark;
  final String assignedAgentName;
  final String assignedAgentAvatar;

  Conversation copyWith({
    String? visitorLabel,
    String? status,
    String? lastMessage,
    int? unread,
    String? remark,
  }) => Conversation(
    id: id,
    visitorLabel: visitorLabel ?? this.visitorLabel,
    status: status ?? this.status,
    lastMessage: lastMessage ?? this.lastMessage,
    updatedAt: updatedAt,
    unread: unread ?? this.unread,
    remark: remark ?? this.remark,
    assignedAgentName: assignedAgentName,
    assignedAgentAvatar: assignedAgentAvatar,
  );
}

class ChatMessage {
  const ChatMessage({
    required this.id,
    required this.conversationId,
    required this.senderType,
    required this.messageType,
    required this.content,
    required this.createdAt,
    this.clientMessageId = '',
    this.revoked = false,
    this.originalContent = '',
    this.detectedLang = '',
    this.translationStatus = '',
  });

  factory ChatMessage.fromJson(Map<String, dynamic> json) => ChatMessage(
    id: '${json['server_msg_id'] ?? ''}',
    clientMessageId: '${json['client_msg_id'] ?? ''}',
    conversationId: '${json['conversation_id'] ?? ''}',
    senderType: '${json['sender_type'] ?? ''}',
    messageType: '${json['message_type'] ?? 'text'}',
    content: '${json['content'] ?? ''}',
    originalContent: '${json['original_content'] ?? ''}',
    detectedLang: '${json['detected_lang'] ?? ''}',
    translationStatus: '${json['translation_status'] ?? ''}',
    createdAt:
        DateTime.tryParse('${json['created_at'] ?? ''}')?.toLocal() ??
        DateTime.now(),
    revoked: json['revoked_at'] != null || json['message_type'] == 'revoked',
  );

  final String id;
  final String clientMessageId;
  final String conversationId;
  final String senderType;
  final String messageType;
  final String content;
  final String originalContent;

  /// 这条消息实际使用的语言（基础码，如 es / ar / zh）。
  final String detectedLang;

  /// 翻译状态：skipped / pending / done / failed。
  final String translationStatus;
  final DateTime createdAt;
  final bool revoked;

  /// 本地撤回成功后原地更新气泡（B5）。
  ChatMessage asRevoked() => ChatMessage(
    id: id,
    clientMessageId: clientMessageId,
    conversationId: conversationId,
    senderType: senderType,
    messageType: 'revoked',
    content: content,
    originalContent: originalContent,
    detectedLang: detectedLang,
    translationStatus: translationStatus,
    createdAt: createdAt,
    revoked: true,
  );
}

/// 客服自己的翻译 / AI 偏好（`GET|PATCH /api/agent/preferences`）。
class AgentPreferences {
  const AgentPreferences({
    required this.autoTranslateIncoming,
    required this.autoTranslateOutgoing,
    required this.reviewTranslationBeforeSend,
    required this.translationDialectMode,
    required this.aiSuggestReplies,
  });

  factory AgentPreferences.fromJson(Map<String, dynamic> json) {
    final mode = '${json['translation_dialect_mode'] ?? ''}'
        .trim()
        .toLowerCase();
    return AgentPreferences(
      // 字段缺失或类型异常时按服务端默认值 true 处理。
      autoTranslateIncoming: json['auto_translate_incoming'] != false,
      autoTranslateOutgoing: json['auto_translate_outgoing'] != false,
      reviewTranslationBeforeSend:
          json['review_translation_before_send'] != false,
      translationDialectMode: mode == dialectModeDialect
          ? dialectModeDialect
          : dialectModeStandard,
      aiSuggestReplies: json['ai_suggest_replies'] != false,
    );
  }

  static const String dialectModeStandard = 'standard';
  static const String dialectModeDialect = 'dialect';

  /// 服务端默认值：5 项均为 true / standard。
  static const AgentPreferences defaults = AgentPreferences(
    autoTranslateIncoming: true,
    autoTranslateOutgoing: true,
    reviewTranslationBeforeSend: true,
    translationDialectMode: dialectModeStandard,
    aiSuggestReplies: true,
  );

  final bool autoTranslateIncoming;
  final bool autoTranslateOutgoing;
  final bool reviewTranslationBeforeSend;

  /// 翻译风格：standard（标准语）/ dialect（含方言）。
  final String translationDialectMode;
  final bool aiSuggestReplies;

  bool get usesDialect => translationDialectMode == dialectModeDialect;

  AgentPreferences copyWith({
    bool? autoTranslateIncoming,
    bool? autoTranslateOutgoing,
    bool? reviewTranslationBeforeSend,
    String? translationDialectMode,
    bool? aiSuggestReplies,
  }) => AgentPreferences(
    autoTranslateIncoming: autoTranslateIncoming ?? this.autoTranslateIncoming,
    autoTranslateOutgoing: autoTranslateOutgoing ?? this.autoTranslateOutgoing,
    reviewTranslationBeforeSend:
        reviewTranslationBeforeSend ?? this.reviewTranslationBeforeSend,
    translationDialectMode:
        translationDialectMode ?? this.translationDialectMode,
    aiSuggestReplies: aiSuggestReplies ?? this.aiSuggestReplies,
  );
}

/// 偏好局部更新体：只把显式赋值的字段写入 JSON，未提供的字段服务端保持原值。
class AgentPreferencesPatch {
  const AgentPreferencesPatch({
    this.autoTranslateIncoming,
    this.autoTranslateOutgoing,
    this.reviewTranslationBeforeSend,
    this.translationDialectMode,
    this.aiSuggestReplies,
  });

  final bool? autoTranslateIncoming;
  final bool? autoTranslateOutgoing;
  final bool? reviewTranslationBeforeSend;
  final String? translationDialectMode;
  final bool? aiSuggestReplies;

  bool get isEmpty =>
      autoTranslateIncoming == null &&
      autoTranslateOutgoing == null &&
      reviewTranslationBeforeSend == null &&
      translationDialectMode == null &&
      aiSuggestReplies == null;

  Map<String, dynamic> toJson() => <String, dynamic>{
    if (autoTranslateIncoming != null)
      'auto_translate_incoming': autoTranslateIncoming,
    if (autoTranslateOutgoing != null)
      'auto_translate_outgoing': autoTranslateOutgoing,
    if (reviewTranslationBeforeSend != null)
      'review_translation_before_send': reviewTranslationBeforeSend,
    if (translationDialectMode != null)
      'translation_dialect_mode': translationDialectMode,
    if (aiSuggestReplies != null) 'ai_suggest_replies': aiSuggestReplies,
  };
}

/// 发送前译文预览（`POST /api/agent/translate-preview`）：只翻译，不产生消息。
class TranslationPreview {
  const TranslationPreview({
    required this.targetLang,
    required this.sourceLang,
    required this.translatedText,
    this.dialect = '',
  });

  factory TranslationPreview.fromJson(Map<String, dynamic> json) =>
      TranslationPreview(
        targetLang: '${json['target_lang'] ?? ''}'.trim(),
        sourceLang: '${json['source_lang'] ?? ''}'.trim(),
        translatedText: '${json['translated_text'] ?? ''}',
        dialect: '${json['dialect'] ?? ''}'.trim(),
      );

  final String targetLang;
  final String sourceLang;
  final String translatedText;

  /// 会话检测到的方言标签（如 es-419），标准语模式下为空。
  final String dialect;

  /// 源语言与目标语言一致时后端原样返回，界面上无需再确认译文。
  /// 响应缺字段时同样按"无需确认"处理，避免卡住发送。
  bool get sameLanguage {
    final source = sourceLang.toLowerCase();
    final target = targetLang.toLowerCase();
    if (source.isEmpty || target.isEmpty) return true;
    return source == target;
  }
}

class RatingSummary {
  const RatingSummary({
    required this.total,
    required this.satisfied,
    required this.satisfactionRate,
    this.manualSatisfactionRate,
    this.displaySatisfactionRate,
  });

  factory RatingSummary.fromJson(Map<String, dynamic> json) => RatingSummary(
    total: (json['total'] as num?)?.toInt() ?? 0,
    satisfied: (json['satisfied'] as num?)?.toInt() ?? 0,
    satisfactionRate: (json['satisfaction_rate'] as num?)?.toDouble() ?? 0,
    manualSatisfactionRate: (json['manual_satisfaction_rate'] as num?)?.toDouble(),
    displaySatisfactionRate: (json['display_satisfaction_rate'] as num?)?.toDouble(),
  );

  final int total;
  final int satisfied;
  final double satisfactionRate;
  final double? manualSatisfactionRate;
  final double? displaySatisfactionRate;

  int get percent =>
      ((displaySatisfactionRate ?? satisfactionRate) * 100).round();
}
