import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:http/http.dart' as http;
import 'package:shared_preferences/shared_preferences.dart';
import 'package:web_socket_channel/io.dart';
import 'package:web_socket_channel/web_socket_channel.dart';

import 'models.dart';

class LoginResult {
  const LoginResult({required this.token, required this.agent});

  final String token;
  final AgentProfile agent;
}

class ServiceBridgeException implements Exception {
  const ServiceBridgeException(this.message);
  final String message;
  @override
  String toString() => message;
}

class ServiceBridgeClient {
  ServiceBridgeClient({required this.baseUrl});

  String baseUrl;
  String token = '';
  AgentProfile? agent;
  WebSocketChannel? _channel;
  StreamSubscription<dynamic>? _socketSubscription;
  Timer? _heartbeat;
  Timer? _reconnectTimer;
  bool _closedByUser = false;

  final _eventController = StreamController<Map<String, dynamic>>.broadcast();
  Stream<Map<String, dynamic>> get events => _eventController.stream;

  static Future<String> savedServerUrl() async {
    final preferences = await SharedPreferences.getInstance();
    return preferences.getString('servicebridge_url') ??
        const String.fromEnvironment(
          'SERVICEBRIDGE_URL',
          defaultValue: 'http://10.0.2.2:8080',
        );
  }

  Future<void> saveServerUrl(String value) async {
    baseUrl = normalizeBaseUrl(value);
    final preferences = await SharedPreferences.getInstance();
    await preferences.setString('servicebridge_url', baseUrl);
  }

  static String normalizeBaseUrl(String value) {
    var normalized = value.trim();
    if (!normalized.startsWith('http://') &&
        !normalized.startsWith('https://')) {
      normalized = 'http://$normalized';
    }
    final uri = Uri.tryParse(normalized);
    if (uri == null || uri.host.isEmpty) {
      throw const ServiceBridgeException('请输入有效的服务器地址');
    }
    return normalized.replaceFirst(RegExp(r'/+$'), '');
  }

  Future<LoginResult> login(String account, String password) async {
    final payload = await _request(
      'POST',
      '/api/agent/login',
      body: {'account': account.trim(), 'password': password},
      authenticated: false,
    );
    token = '${payload['token'] ?? ''}';
    agent = AgentProfile.fromJson(
      Map<String, dynamic>.from(payload['agent'] as Map? ?? {}),
    );
    if (token.isEmpty || agent!.id.isEmpty) {
      throw const ServiceBridgeException('登录响应不完整');
    }
    return LoginResult(token: token, agent: agent!);
  }

  Future<AgentProfile> setStatus(String status) async {
    final payload = await _request(
      'POST',
      '/api/agent/status',
      body: {'status': status},
    );
    final updated = AgentProfile.fromJson(
      Map<String, dynamic>.from(payload['agent'] as Map? ?? {}),
    );
    agent = updated;
    return updated;
  }

  Future<AgentProfile> updateAvatar(String avatarUrl) async {
    final payload = await _request(
      'POST',
      '/api/agent/avatar',
      body: {'avatar_url': avatarUrl},
    );
    final updated = AgentProfile.fromJson(
      Map<String, dynamic>.from(payload['agent'] as Map? ?? {}),
    );
    agent = updated;
    return updated;
  }

  Future<RatingSummary> ratingSummary() async {
    final payload = await _request('GET', '/api/agent/ratings/summary');
    return RatingSummary.fromJson(Map<String, dynamic>.from(payload));
  }

  Future<void> registerDeviceToken(String deviceToken) async {
    await _request(
      'POST',
      '/api/agent/device-token',
      body: {'device_token': deviceToken},
      authenticated: true,
    );
  }

  Future<List<Conversation>> conversations() async {
    final payload = await _request('GET', '/api/agent/conversations');
    return (payload['conversations'] as List? ?? [])
        .map(
          (item) =>
              Conversation.fromJson(Map<String, dynamic>.from(item as Map)),
        )
        .toList();
  }

  Future<List<ChatMessage>> messages(String conversationId) async {
    final payload = await _request(
      'GET',
      '/api/conversations/${Uri.encodeComponent(conversationId)}/messages?limit=100',
    );
    return (payload['messages'] as List? ?? [])
        .map(
          (item) =>
              ChatMessage.fromJson(Map<String, dynamic>.from(item as Map)),
        )
        .toList();
  }

  Future<void> markRead(String conversationId) async {
    await _request(
      'POST',
      '/api/conversations/${Uri.encodeComponent(conversationId)}/read',
      body: const {},
    );
  }

  Future<void> closeConversation(String conversationId) async {
    await _request(
      'POST',
      '/api/conversations/${Uri.encodeComponent(conversationId)}/close',
      body: const {},
    );
  }

  Future<void> deleteConversation(String conversationId) async {
    await _request(
      'DELETE',
      '/api/conversations/${Uri.encodeComponent(conversationId)}',
    );
  }

  Future<void> changePassword(
    String currentPassword,
    String newPassword,
  ) async {
    await _request(
      'POST',
      '/api/account/password',
      body: {'current_password': currentPassword, 'new_password': newPassword},
    );
  }

  Future<String> upload(File file) async {
    final uri = Uri.parse('$baseUrl/api/uploads');
    final request = http.MultipartRequest('POST', uri)
      ..headers['Authorization'] = 'Bearer $token'
      ..files.add(await http.MultipartFile.fromPath('file', file.path));
    final response = await request.send();
    final text = await response.stream.bytesToString();
    final payload = _decode(text);
    if (response.statusCode < 200 || response.statusCode >= 300) {
      throw ServiceBridgeException(_errorMessage(payload, response.statusCode));
    }
    return '${payload['url'] ?? ''}';
  }

  void sendMessage(
    String conversationId,
    String content, {
    String messageType = 'text',
  }) {
    final value = content.trim();
    if (_channel == null) throw const ServiceBridgeException('实时通道尚未连接');
    if (value.isEmpty) throw const ServiceBridgeException('消息不能为空');
    _channel!.sink.add(
      jsonEncode({
        'event': 'message.send',
        'data': {
          'conversation_id': conversationId,
          'client_msg_id': 'app_${DateTime.now().microsecondsSinceEpoch}',
          'message_type': messageType,
          'content': value,
        },
      }),
    );
  }

  /// 客服正在输入指示：访客聊天窗口会显示"客服正在回复"。
  void sendTyping(String conversationId, {bool typing = true}) {
    if (_channel == null) return;
    try {
      _channel!.sink.add(
        jsonEncode({
          'event': 'conversation.typing',
          'data': {
            'conversation_id': conversationId,
            'typing': typing,
          },
        }),
      );
    } catch (_) {}
  }

  void connect() {
    if (token.isEmpty) return;
    _closedByUser = false;
    _connectNow();
  }

  void _connectNow() {
    if (_closedByUser || token.isEmpty) return;
    _socketSubscription?.cancel();
    _heartbeat?.cancel();
    _reconnectTimer?.cancel();
    try {
      final httpUri = Uri.parse(baseUrl);
      final socketUri = httpUri.replace(
        scheme: httpUri.scheme == 'https' ? 'wss' : 'ws',
        path: '/ws',
        queryParameters: {'role': 'agent', 'token': token},
      );
      _channel = IOWebSocketChannel.connect(
        socketUri,
        pingInterval: const Duration(seconds: 20),
      );
      _socketSubscription = _channel!.stream.listen(
        (raw) {
          final decoded = _decode('$raw');
          _eventController.add(Map<String, dynamic>.from(decoded));
        },
        onError: (_) => _scheduleReconnect(),
        onDone: _scheduleReconnect,
      );
      _heartbeat = Timer.periodic(const Duration(seconds: 25), (_) {
        try {
          _channel?.sink.add(
            jsonEncode({
              'event': 'ping',
              'data': {'at': DateTime.now().toUtc().toIso8601String()},
            }),
          );
        } catch (_) {
          _scheduleReconnect();
        }
      });
    } catch (_) {
      _channel = null;
      _scheduleReconnect();
    }
  }

  void _scheduleReconnect() {
    _heartbeat?.cancel();
    _channel = null;
    if (_closedByUser || token.isEmpty) return;
    _reconnectTimer?.cancel();
    _reconnectTimer = Timer(const Duration(seconds: 3), _connectNow);
  }

  Future<Map<String, dynamic>> _request(
    String method,
    String path, {
    Map<String, dynamic>? body,
    bool authenticated = true,
  }) async {
    final uri = Uri.parse('$baseUrl$path');
    final headers = <String, String>{'Accept': 'application/json'};
    if (authenticated) headers['Authorization'] = 'Bearer $token';
    if (body != null) headers['Content-Type'] = 'application/json';
    final request = http.Request(method, uri)
      ..headers.addAll(headers)
      ..body = body == null ? '' : jsonEncode(body);
    final streamed = await request.send().timeout(const Duration(seconds: 15));
    final text = await streamed.stream.bytesToString();
    final payload = _decode(text);
    if (streamed.statusCode < 200 || streamed.statusCode >= 300) {
      throw ServiceBridgeException(_errorMessage(payload, streamed.statusCode));
    }
    return payload;
  }

  Map<String, dynamic> _decode(String text) {
    if (text.trim().isEmpty) return <String, dynamic>{};
    try {
      return Map<String, dynamic>.from(jsonDecode(text) as Map);
    } catch (_) {
      return {'message': text};
    }
  }

  String _errorMessage(Map<String, dynamic> payload, int status) {
    final error = payload['error'];
    final message = error is Map && error['message'] != null
        ? '${error['message']}'
        : payload['message'] != null
        ? '${payload['message']}'
        : error is String
        ? error
        : '';
    return switch (message.toLowerCase()) {
      'invalid credentials' => '账号或密码错误',
      'unauthorized' => '登录已失效，请重新登录',
      'forbidden' => '当前账号没有操作权限',
      'not found' => '请求的数据不存在',
      'invalid input' => '输入内容不符合要求',
      _ when message.isNotEmpty => message,
      _ => '请求失败（HTTP $status）',
    };
  }

  Future<void> close() async {
    _closedByUser = true;
    _heartbeat?.cancel();
    _reconnectTimer?.cancel();
    await _socketSubscription?.cancel();
    await _channel?.sink.close();
    _channel = null;
  }

  Future<void> dispose() async {
    await close();
    await _eventController.close();
  }
}
