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
  });

  factory Conversation.fromJson(Map<String, dynamic> json) => Conversation(
    id: '${json['id'] ?? ''}',
    visitorLabel: '${json['visitor_remark'] ?? json['visitor_ip'] ?? '访客'}',
    status: '${json['status'] ?? 'waiting'}',
    lastMessage: '${json['last_message'] ?? ''}',
    updatedAt: DateTime.tryParse('${json['updated_at'] ?? ''}')?.toLocal(),
    unread: (json['unread_for_agent'] as num?)?.toInt() ?? 0,
  );

  final String id;
  final String visitorLabel;
  final String status;
  final String lastMessage;
  final DateTime? updatedAt;
  final int unread;
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
  });

  factory ChatMessage.fromJson(Map<String, dynamic> json) => ChatMessage(
    id: '${json['server_msg_id'] ?? ''}',
    clientMessageId: '${json['client_msg_id'] ?? ''}',
    conversationId: '${json['conversation_id'] ?? ''}',
    senderType: '${json['sender_type'] ?? ''}',
    messageType: '${json['message_type'] ?? 'text'}',
    content: '${json['content'] ?? ''}',
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
  final DateTime createdAt;
  final bool revoked;
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
