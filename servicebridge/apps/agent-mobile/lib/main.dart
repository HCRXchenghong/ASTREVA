import 'dart:async';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:image_picker/image_picker.dart';

import 'models.dart';
import 'push_service.dart';
import 'update_service.dart';
import 'service_bridge.dart';

abstract final class AppColors {
  AppColors._();

  // Brand
  static const Color primary = Color(0xFF0878DC);
  static const Color primaryDark = Color(0xFF07345F);
  static const Color primarySoft = Color(0xFFE8F4FB);

  // Surfaces
  static const Color background = Color(0xFFF3F7FA);
  static const Color surface = Colors.white;
  static const Color border = Color(0xFFE0E8EF);
  static const Color divider = Color(0xFFE6EBF0);

  // Text
  static const Color textPrimary = Color(0xFF142033);
  static const Color textSecondary = Color(0xFF68778C);
  static const Color textMuted = Color(0xFF9AA7B4);
  static const Color textFaint = Color(0xFFB1C0CD);

  // Semantic
  static const Color success = Color(0xFF16835C);
  static const Color successSoft = Color(0xFFE5F6EF);
  static const Color successBorder = Color(0xFFBFE8D3);
  static const Color warning = Color(0xFFA46600);
  static const Color danger = Color(0xFFEF5B4C);
  static const Color neutral = Color(0xFF7A8794);

  // Chat identity (WeChat-style bubbles; intentionally scoped to chat UI)
  static const Color chatBubbleMine = Color(0xFF95EC69);
}

abstract final class AppRadii {
  AppRadii._();

  static const double card = 16;
  static const double input = 14;
  static const double bubble = 12;
  static const double bubbleTail = 4;
  static const double chip = 9;
  static const double pill = 999;
}

abstract final class AppTheme {
  AppTheme._();

  static ThemeData light() {
    const scheme = ColorScheme.light(
      primary: AppColors.primary,
      onPrimary: Colors.white,
      secondary: AppColors.primaryDark,
      onSecondary: Colors.white,
      surface: AppColors.surface,
      onSurface: AppColors.textPrimary,
      error: AppColors.danger,
      onError: Colors.white,
    );

    return ThemeData(
      useMaterial3: true,
      colorScheme: scheme,
      scaffoldBackgroundColor: AppColors.background,
      fontFamilyFallback: const ['PingFang SC', 'Microsoft YaHei'],
      splashFactory: InkSparkle.splashFactory,
      appBarTheme: const AppBarTheme(
        backgroundColor: AppColors.surface,
        foregroundColor: AppColors.textPrimary,
        surfaceTintColor: Colors.transparent,
        elevation: 0,
        scrolledUnderElevation: 0,
        centerTitle: false,
        titleTextStyle: TextStyle(
          color: AppColors.textPrimary,
          fontSize: 18,
          fontWeight: FontWeight.w700,
          fontFamilyFallback: ['PingFang SC', 'Microsoft YaHei'],
        ),
      ),
      inputDecorationTheme: InputDecorationTheme(
        filled: true,
        fillColor: AppColors.surface,
        labelStyle: const TextStyle(color: AppColors.textSecondary),
        hintStyle: const TextStyle(color: AppColors.textMuted),
        border: OutlineInputBorder(
          borderRadius: BorderRadius.circular(AppRadii.input),
          borderSide: const BorderSide(color: AppColors.border),
        ),
        enabledBorder: OutlineInputBorder(
          borderRadius: BorderRadius.circular(AppRadii.input),
          borderSide: const BorderSide(color: AppColors.border),
        ),
        focusedBorder: OutlineInputBorder(
          borderRadius: BorderRadius.circular(AppRadii.input),
          borderSide: const BorderSide(color: AppColors.primary, width: 1.5),
        ),
        contentPadding: const EdgeInsets.symmetric(
          horizontal: 16,
          vertical: 15,
        ),
      ),
      filledButtonTheme: FilledButtonThemeData(
        style: FilledButton.styleFrom(
          backgroundColor: AppColors.primary,
          foregroundColor: Colors.white,
          minimumSize: const Size(0, 48),
          shape: RoundedRectangleBorder(
            borderRadius: BorderRadius.circular(AppRadii.input),
          ),
          textStyle: const TextStyle(
            fontSize: 16,
            fontWeight: FontWeight.w700,
          ),
        ),
      ),
      textButtonTheme: TextButtonThemeData(
        style: TextButton.styleFrom(
          foregroundColor: AppColors.primary,
          textStyle: const TextStyle(fontWeight: FontWeight.w600),
        ),
      ),
      dividerTheme: const DividerThemeData(
        color: AppColors.divider,
        thickness: 1,
        space: 1,
      ),
      listTileTheme: const ListTileThemeData(
        iconColor: AppColors.textPrimary,
        textColor: AppColors.textPrimary,
      ),
      snackBarTheme: SnackBarThemeData(
        backgroundColor: AppColors.textPrimary,
        contentTextStyle: const TextStyle(color: Colors.white),
        behavior: SnackBarBehavior.floating,
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(AppRadii.input),
        ),
      ),
      dialogTheme: DialogThemeData(
        backgroundColor: AppColors.surface,
        surfaceTintColor: Colors.transparent,
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(AppRadii.card),
        ),
      ),
    );
  }

  static BoxDecoration cardDecoration({Color? color}) => BoxDecoration(
    color: color ?? AppColors.surface,
    borderRadius: BorderRadius.circular(AppRadii.card),
    border: Border.all(color: AppColors.border),
  );
}

void main() {
  WidgetsFlutterBinding.ensureInitialized();
  PushService.instance.init();
  runApp(const CustomerServiceApp());
}

class CustomerServiceApp extends StatelessWidget {
  const CustomerServiceApp({super.key});

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: '在线客服',
      debugShowCheckedModeBanner: false,
      navigatorKey: PushService.instance.navigatorKey,
      theme: AppTheme.light(),
      home: const LoginPage(),
    );
  }
}

class LoginPage extends StatefulWidget {
  const LoginPage({super.key});

  @override
  State<LoginPage> createState() => _LoginPageState();
}

class _LoginPageState extends State<LoginPage> {
  final _formKey = GlobalKey<FormState>();
  final _accountController = TextEditingController();
  final _passwordController = TextEditingController();
  final _serverController = TextEditingController();
  bool _busy = false;
  bool _obscurePassword = true;

  @override
  void initState() {
    super.initState();
    ServiceBridgeClient.savedServerUrl().then((url) {
      if (mounted) setState(() => _serverController.text = url);
      _checkAppUpdate(url);
    });
  }

  Future<void> _checkAppUpdate(String serverUrl) async {
    if (serverUrl.trim().isEmpty) return;
    try {
      final baseUrl = ServiceBridgeClient.normalizeBaseUrl(serverUrl);
      final info = await AppUpdater.checkForUpdate(baseUrl);
      if (!mounted || !info.updateAvailable) return;
      await _showUpdateDialog(info, baseUrl);
    } catch (_) {
      // Update check is best-effort; login must never be blocked by it.
    }
  }

  Future<void> _showUpdateDialog(AppUpdateInfo info, String baseUrl) async {
    final notes = info.releaseNotes.trim().isEmpty
        ? '本次更新包含功能优化与问题修复。'
        : info.releaseNotes;
    await showDialog<void>(
      context: context,
      barrierDismissible: !info.forceUpdate,
      builder: (dialogContext) => AlertDialog(
        title: Text(info.forceUpdate ? '必须更新到新版本' : '发现新版本'),
        content: Text(
          '当前版本过低，请升级到 v${info.latestVersion} 后继续使用。\n\n更新内容：\n$notes',
        ),
        actions: [
          if (!info.forceUpdate)
            TextButton(
              onPressed: () => Navigator.of(dialogContext).pop(),
              child: const Text('稍后再说'),
            ),
          FilledButton(
            onPressed: () async {
              Navigator.of(dialogContext).pop();
              await _performUpdate(info, baseUrl);
              if (info.forceUpdate) {
                await _showUpdateDialog(info, baseUrl);
              }
            },
            child: const Text('立即更新'),
          ),
        ],
      ),
    );
  }

  Future<void> _performUpdate(AppUpdateInfo info, String baseUrl) async {
    if (AppUpdater.isIOS) {
      if (mounted) {
        _showMessage(context, '请前往 App Store 更新到 v${info.latestVersion}。');
      }
      return;
    }
    if (!mounted) return;
    showDialog<void>(
      context: context,
      barrierDismissible: false,
      builder: (_) => const AlertDialog(
        content: Row(
          children: [
            CircularProgressIndicator(),
            SizedBox(width: 18),
            Expanded(child: Text('正在下载更新包…')),
          ],
        ),
      ),
    );
    try {
      final allowed = await AppUpdater.canInstallUnknownApps();
      if (!allowed) {
        if (mounted) {
          Navigator.of(context, rootNavigator: true).pop();
          _showMessage(context, '请允许"在线客服"安装未知来源应用，然后重新点击更新。');
        }
        await AppUpdater.openInstallSettings();
        return;
      }
      final path = await AppUpdater.downloadApk(info.downloadUrl);
      if (mounted) Navigator.of(context, rootNavigator: true).pop();
      await AppUpdater.installApk(path);
    } catch (error) {
      if (mounted) {
        Navigator.of(context, rootNavigator: true).pop();
        _showMessage(context, '更新失败：${_readableError(error)}');
      }
    }
  }

  @override
  void dispose() {
    _accountController.dispose();
    _passwordController.dispose();
    _serverController.dispose();
    super.dispose();
  }

  Future<void> _login() async {
    if (!_formKey.currentState!.validate()) return;
    setState(() => _busy = true);
    try {
      final client = ServiceBridgeClient(
        baseUrl: ServiceBridgeClient.normalizeBaseUrl(_serverController.text),
      );
      final result = await client.login(
        _accountController.text,
        _passwordController.text,
      );
      await client.saveServerUrl(_serverController.text);
      await PushService.instance.registerToBackend(client);
      if (!mounted) return;
      await Navigator.of(context).pushReplacement(
        MaterialPageRoute(
          builder: (_) => HomePage(client: client, profile: result.agent),
        ),
      );
    } catch (error) {
      if (mounted) _showMessage(context, _readableError(error));
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: Stack(
        children: [
          const Positioned.fill(
            child: DecoratedBox(
              decoration: BoxDecoration(
                gradient: LinearGradient(
                  begin: Alignment.topLeft,
                  end: Alignment.bottomRight,
                  colors: [Color(0xFFF5FAFD), Color(0xFFE8F2F7)],
                ),
              ),
            ),
          ),
          Positioned(
            top: -110,
            right: -90,
            child: Container(
              width: 280,
              height: 280,
              decoration: BoxDecoration(
                shape: BoxShape.circle,
                border: Border.all(color: const Color(0x2218B7D8), width: 34),
              ),
            ),
          ),
          SafeArea(
            child: Center(
              child: SingleChildScrollView(
                padding: const EdgeInsets.all(24),
                child: ConstrainedBox(
                  constraints: const BoxConstraints(maxWidth: 440),
                  child: Form(
                    key: _formKey,
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.stretch,
                      children: [
                        Center(
                          child: Image.asset(
                            'assets/astreva-logo-transparent.png',
                            width: 112,
                            height: 112,
                          ),
                        ),
                        const SizedBox(height: 12),
                        const Text(
                          '在线客服',
                          textAlign: TextAlign.center,
                          style: TextStyle(
                            fontSize: 32,
                            fontWeight: FontWeight.w800,
                            color: AppColors.primaryDark,
                          ),
                        ),
                        const SizedBox(height: 6),
                        const Text(
                          'ASTREVA 客服坐席工作台',
                          textAlign: TextAlign.center,
                          style: TextStyle(
                            color: AppColors.textSecondary,
                            letterSpacing: 1.2,
                          ),
                        ),
                        const SizedBox(height: 34),
                        TextFormField(
                          controller: _accountController,
                          textInputAction: TextInputAction.next,
                          autofillHints: const [AutofillHints.username],
                          decoration: const InputDecoration(
                            labelText: '客服账号',
                            prefixIcon: Icon(Icons.badge_outlined),
                          ),
                          validator: _required,
                        ),
                        const SizedBox(height: 14),
                        TextFormField(
                          controller: _passwordController,
                          obscureText: _obscurePassword,
                          autofillHints: const [AutofillHints.password],
                          onFieldSubmitted: (_) => _login(),
                          decoration: InputDecoration(
                            labelText: '登录密码',
                            prefixIcon: const Icon(Icons.lock_outline),
                            suffixIcon: IconButton(
                              icon: Icon(
                                _obscurePassword
                                    ? Icons.visibility_outlined
                                    : Icons.visibility_off_outlined,
                              ),
                              onPressed: () => setState(
                                () => _obscurePassword = !_obscurePassword,
                              ),
                            ),
                          ),
                          validator: _required,
                        ),
                        const SizedBox(height: 14),
                        TextFormField(
                          controller: _serverController,
                          keyboardType: TextInputType.url,
                          decoration: const InputDecoration(
                            labelText: '服务器地址',
                            hintText: 'http://电脑局域网IP:8080',
                            prefixIcon: Icon(Icons.dns_outlined),
                          ),
                          validator: _required,
                        ),
                        const SizedBox(height: 9),
                        const Text(
                          '真机请输入电脑和手机同一 Wi-Fi 下的局域网地址。',
                          style: TextStyle(
                            fontSize: 12,
                            color: AppColors.textSecondary,
                          ),
                        ),
                        const SizedBox(height: 24),
                        FilledButton.icon(
                          onPressed: _busy ? null : _login,
                          icon: _busy
                              ? const SizedBox(
                                  width: 18,
                                  height: 18,
                                  child: CircularProgressIndicator(
                                    strokeWidth: 2,
                                    color: Colors.white,
                                  ),
                                )
                              : const Icon(Icons.login),
                          label: const Padding(
                            padding: EdgeInsets.symmetric(vertical: 15),
                            child: Text(
                              '登录工作台',
                              style: TextStyle(
                                fontSize: 16,
                                fontWeight: FontWeight.w800,
                              ),
                            ),
                          ),
                        ),
                      ],
                    ),
                  ),
                ),
              ),
            ),
          ),
        ],
      ),
    );
  }
}

class HomePage extends StatefulWidget {
  const HomePage({super.key, required this.client, required this.profile});
  final ServiceBridgeClient client;
  final AgentProfile profile;
  @override
  State<HomePage> createState() => _HomePageState();
}

class _HomePageState extends State<HomePage> {
  late AgentProfile _profile;
  List<Conversation> _conversations = const [];
  StreamSubscription<Map<String, dynamic>>? _events;
  Timer? _pollTimer;
  RatingSummary? _rating;
  bool _loading = true;
  int _tab = 0;

  @override
  void initState() {
    super.initState();
    _profile = widget.profile;
    _loadRating();
    _events = widget.client.events.listen(_handleEvent);
    widget.client.connect();
    _setStatus('online');
    _refresh();
    _pollTimer = Timer.periodic(
      const Duration(seconds: 15),
      (_) => _refresh(silent: true),
    );
    PushService.instance.openHandler = _openConversationFromPush;
    final pending = PushService.instance.consumePendingConversation();
    if (pending != null && pending.isNotEmpty) {
      _openConversationFromPush(pending);
    }
  }

  Future<void> _loadRating() async {
    try {
      final rating = await widget.client.ratingSummary();
      if (mounted) setState(() => _rating = rating);
    } catch (_) {
      // 好评率展示失败不影响主流程，刷新时会重试。
    }
  }

  Future<void> _pickAvatar() async {
    final picker = ImagePicker();
    final image = await picker.pickImage(
      source: ImageSource.gallery,
      imageQuality: 82,
      maxWidth: 640,
      maxHeight: 640,
    );
    if (image == null) return;
    try {
      final url = await widget.client.upload(File(image.path));
      final profile = await widget.client.updateAvatar(url);
      if (mounted) {
        setState(() => _profile = profile);
        _showMessage(context, '头像已更新');
      }
    } catch (error) {
      if (mounted) _showMessage(context, _readableError(error));
    }
  }

  Future<void> _openConversationFromPush(String conversationId) async {
    await _refresh(silent: true);
    if (!mounted) return;
    Conversation? target;
    for (final c in _conversations) {
      if (c.id == conversationId) {
        target = c;
        break;
      }
    }
    if (target == null) {
      _showMessage(context, '会话不存在或已被移除');
      return;
    }
    await _openConversation(target);
  }

  Future<void> _openConversation(Conversation conversation) async {
    await Navigator.of(context).push(
      MaterialPageRoute(
        builder: (_) => ChatPage(
          client: widget.client,
          conversation: conversation,
          myAvatarUrl: _profile.avatarUrl,
        ),
      ),
    );
    if (mounted) _refresh(silent: true);
  }

  void _handleEvent(Map<String, dynamic> event) {
    final type = '${event['event'] ?? ''}';
    if (type == 'session.revoked') {
      _logout(message: '账号已退出，请重新登录');
      return;
    }
    final data = Map<String, dynamic>.from(event['data'] as Map? ?? {});
    if (type == 'agent.notification') {
      final title = '${data['title'] ?? ''}'.trim();
      final body = '${data['body'] ?? ''}'.trim();
      final conversationId = '${data['conversation_id'] ?? ''}'.trim();
      PushService.instance.showLocalNotification(
        title: title.isEmpty ? '新客服消息' : title,
        body: body.isEmpty ? '您有新的客服消息' : body,
        conversationId: conversationId.isEmpty ? null : conversationId,
      );
    }
    if (type == 'message.receive' ||
        type == 'conversation.assigned' ||
        type == 'conversation.status_changed' ||
        type == 'agent.notification') {
      _refresh(silent: true);
    }
  }

  Future<void> _refresh({bool silent = false}) async {
    if (!silent && mounted) setState(() => _loading = true);
    try {
      final items = await widget.client.conversations();
      items.sort(
        (a, b) =>
            (b.updatedAt ?? DateTime(0)).compareTo(a.updatedAt ?? DateTime(0)),
      );
      if (mounted) setState(() => _conversations = items);
    } catch (error) {
      if (!silent && mounted) _showMessage(context, _readableError(error));
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  Future<void> _setStatus(String status) async {
    try {
      final profile = await widget.client.setStatus(status);
      if (mounted) setState(() => _profile = profile);
    } catch (error) {
      if (mounted) _showMessage(context, _readableError(error));
    }
  }

  Future<void> _deleteConversation(Conversation conversation) async {
    if (conversation.status != 'closed') {
      return;
    }
    if (!await _confirm(
      context,
      '删除会话',
      '确定删除与 ${conversation.visitorLabel} 的已结束会话？删除后不可恢复。',
    )) {
      return;
    }
    try {
      await widget.client.deleteConversation(conversation.id);
      await _refresh(silent: true);
      if (mounted) _showMessage(context, '会话已删除');
    } catch (error) {
      if (mounted) _showMessage(context, _readableError(error));
    }
  }

  Future<void> _logout({String? message}) async {
    _pollTimer?.cancel();
    PushService.instance.openHandler = null;
    await PushService.instance.unregisterFromBackend();
    await widget.client.close();
    if (!mounted) return;
    Navigator.of(context).pushAndRemoveUntil(
      MaterialPageRoute(builder: (_) => const LoginPage()),
      (_) => false,
    );
    if (message != null) _showMessage(context, message);
  }

  @override
  void dispose() {
    _pollTimer?.cancel();
    _events?.cancel();
    widget.client.setStatus('offline').catchError((_) => _profile);
    widget.client.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final unread = _conversations.fold<int>(0, (sum, c) => sum + c.unread);
    final now = DateTime.now();
    final todayServed = _conversations.where((c) {
      final t = c.updatedAt;
      return t != null &&
          t.year == now.year &&
          t.month == now.month &&
          t.day == now.day;
    }).length;

    return Scaffold(
      body: IndexedStack(
        index: _tab,
        children: [
          ChatListView(
            conversations: _conversations,
            loading: _loading,
            onRefresh: _refresh,
            onOpen: _openConversation,
            onDelete: _deleteConversation,
          ),
          MePage(
            profile: _profile,
            unread: unread,
            todayServed: todayServed,
            avatarUrl: _profile.avatarUrl,
            baseUrl: widget.client.baseUrl,
            rating: _rating,
            onPickAvatar: _pickAvatar,
            onStatusChanged: _setStatus,
            onChangePassword: () => showDialog<void>(
              context: context,
              builder: (_) => ChangePasswordDialog(client: widget.client),
            ),
            onLogout: _logout,
          ),
        ],
      ),
      bottomNavigationBar: _CustomBottomNav(
        currentIndex: _tab,
        onChanged: (value) {
          setState(() => _tab = value);
          if (value == 1) _loadRating();
        },
      ),
    );
  }
}

class ChatListView extends StatelessWidget {
  const ChatListView({
    super.key,
    required this.conversations,
    required this.loading,
    required this.onRefresh,
    required this.onOpen,
    required this.onDelete,
  });
  final List<Conversation> conversations;
  final bool loading;
  final Future<void> Function() onRefresh;
  final ValueChanged<Conversation> onOpen;
  final ValueChanged<Conversation> onDelete;

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text(
          '在线客服',
          style: TextStyle(fontWeight: FontWeight.w700),
        ),
      ),
      body: RefreshIndicator(
        onRefresh: onRefresh,
        child: loading && conversations.isEmpty
            ? const Center(child: CircularProgressIndicator())
            : conversations.isEmpty
                ? ListView(
                    physics: const AlwaysScrollableScrollPhysics(),
                    children: const [
                      SizedBox(height: 180),
                      _EmptyInbox(),
                    ],
                  )
                : ListView.separated(
                    physics: const AlwaysScrollableScrollPhysics(),
                    padding: const EdgeInsets.only(top: 4, bottom: 24),
                    itemCount: conversations.length,
                    separatorBuilder: (_, _) =>
                        const Divider(height: 1, indent: 76),
                    itemBuilder: (context, index) {
                      final conversation = conversations[index];
                      return _ConversationRow(
                        conversation: conversation,
                        onTap: () => onOpen(conversation),
                        onLongPress: conversation.status == 'closed'
                            ? () => onDelete(conversation)
                            : null,
                      );
                    },
                  ),
      ),
    );
  }
}

class _ConversationRow extends StatelessWidget {
  const _ConversationRow({
    required this.conversation,
    required this.onTap,
    this.onLongPress,
  });
  final Conversation conversation;
  final VoidCallback onTap;
  final VoidCallback? onLongPress;

  @override
  Widget build(BuildContext context) {
    return InkWell(
      onTap: onTap,
      onLongPress: onLongPress,
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 12),
        child: Row(
          children: [
            const _VisitorAvatar(),
            const SizedBox(width: 12),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Row(
                    children: [
                      Expanded(
                        child: Text(
                          conversation.visitorLabel,
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: const TextStyle(
                            fontSize: 16,
                            fontWeight: FontWeight.w700,
                            color: AppColors.textPrimary,
                          ),
                        ),
                      ),
                      _StatusBadge(status: conversation.status),
                      const SizedBox(width: 6),
                      Text(
                        _formatTime(conversation.updatedAt),
                        style: const TextStyle(
                          fontSize: 11,
                          color: AppColors.textMuted,
                        ),
                      ),
                    ],
                  ),
                  const SizedBox(height: 5),
                  Row(
                    children: [
                      Expanded(
                        child: Text(
                          conversation.lastMessage.isEmpty
                              ? '暂无消息'
                              : conversation.lastMessage,
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: const TextStyle(
                            fontSize: 13,
                            color: AppColors.textSecondary,
                          ),
                        ),
                      ),
                      if (conversation.unread > 0)
                        Container(
                          margin: const EdgeInsets.only(left: 8),
                          constraints: const BoxConstraints(minWidth: 20),
                          padding: const EdgeInsets.symmetric(
                            horizontal: 6,
                            vertical: 2,
                          ),
                          decoration: const BoxDecoration(
                            color: AppColors.danger,
                            borderRadius: BorderRadius.all(Radius.circular(AppRadii.chip)),
                          ),
                          child: Text(
                            '${conversation.unread}',
                            textAlign: TextAlign.center,
                            style: const TextStyle(
                              color: Colors.white,
                              fontSize: 11,
                              fontWeight: FontWeight.w700,
                            ),
                          ),
                        ),
                    ],
                  ),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _VisitorAvatar extends StatelessWidget {
  const _VisitorAvatar();
  @override
  Widget build(BuildContext context) => const CircleAvatar(
    radius: 23,
    backgroundColor: AppColors.primarySoft,
    foregroundColor: AppColors.primary,
    child: Icon(Icons.person_outline),
  );
}

class _AgentAvatar extends StatelessWidget {
  const _AgentAvatar({
    required this.radius,
    required this.name,
    this.imageUrl,
    this.baseUrl,
  });
  final double radius;
  final String? imageUrl;
  final String? baseUrl;
  final String name;

  @override
  Widget build(BuildContext context) {
    final initials = Text(
      name.isEmpty ? '客' : name.substring(0, 1),
      style: TextStyle(
        color: Colors.white,
        fontSize: radius * 0.78,
        fontWeight: FontWeight.w700,
      ),
    );
    final url = imageUrl ?? '';
    if (url.isNotEmpty) {
      final resolved = url.startsWith('/') && baseUrl != null
          ? '$baseUrl$url'
          : url;
      return CircleAvatar(
        radius: radius,
        backgroundColor: AppColors.primary,
        child: ClipOval(
          child: Image.network(
            resolved,
            width: radius * 2,
            height: radius * 2,
            fit: BoxFit.cover,
            errorBuilder: (_, _, _) => Center(child: initials),
          ),
        ),
      );
    }
    return CircleAvatar(
      radius: radius,
      backgroundColor: AppColors.primary,
      child: initials,
    );
  }
}

class _StatusBadge extends StatelessWidget {
  const _StatusBadge({required this.status});
  final String status;
  @override
  Widget build(BuildContext context) {
    final waiting = status == 'waiting' || status == 'human_requested';
    final closed = status == 'closed';
    final color = closed
        ? AppColors.neutral
        : waiting
        ? AppColors.warning
        : AppColors.success;
    final label = closed
        ? '已结束'
        : waiting
        ? '待接入'
        : status == 'ai_serving'
        ? 'AI接待'
        : '服务中';
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 7, vertical: 3),
      decoration: BoxDecoration(
        color: color.withValues(alpha: .12),
        borderRadius: BorderRadius.circular(AppRadii.chip),
      ),
      child: Text(
        label,
        style: TextStyle(
          color: color,
          fontSize: 10,
          fontWeight: FontWeight.w700,
        ),
      ),
    );
  }
}

class _EmptyInbox extends StatelessWidget {
  const _EmptyInbox();
  @override
  Widget build(BuildContext context) => const Center(
    child: Column(
      mainAxisSize: MainAxisSize.min,
      children: [
        Icon(Icons.forum_outlined, size: 66, color: AppColors.textFaint),
        SizedBox(height: 15),
        Text('暂时没有分配给你的会话', style: TextStyle(fontWeight: FontWeight.w700)),
        SizedBox(height: 6),
        Text('保持在线，新咨询会自动出现在这里', style: TextStyle(color: AppColors.textMuted)),
      ],
    ),
  );
}

class MePage extends StatelessWidget {
  const MePage({
    super.key,
    required this.profile,
    required this.unread,
    required this.todayServed,
    required this.avatarUrl,
    required this.baseUrl,
    required this.rating,
    required this.onPickAvatar,
    required this.onStatusChanged,
    required this.onChangePassword,
    required this.onLogout,
  });
  final AgentProfile profile;
  final int unread;
  final int todayServed;
  final String avatarUrl;
  final String baseUrl;
  final RatingSummary? rating;
  final VoidCallback onPickAvatar;
  final ValueChanged<String> onStatusChanged;
  final VoidCallback onChangePassword;
  final VoidCallback onLogout;

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('我的', style: TextStyle(fontWeight: FontWeight.w700)),
      ),
      body: ListView(
        padding: const EdgeInsets.fromLTRB(16, 18, 16, 28),
        children: [
          Row(
            children: [
              Stack(
                children: [
                  _AgentAvatar(
                    radius: 36,
                    imageUrl: avatarUrl,
                    baseUrl: baseUrl,
                    name: profile.name,
                  ),
                  Positioned(
                    right: 0,
                    bottom: 0,
                    child: GestureDetector(
                      onTap: onPickAvatar,
                      child: Container(
                        padding: const EdgeInsets.all(5),
                        decoration: const BoxDecoration(
                          color: AppColors.primary,
                          shape: BoxShape.circle,
                        ),
                        child: const Icon(
                          Icons.camera_alt,
                          size: 14,
                          color: Colors.white,
                        ),
                      ),
                    ),
                  ),
                ],
              ),
              const SizedBox(width: 16),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      profile.name,
                      style: const TextStyle(
                        fontSize: 21,
                        fontWeight: FontWeight.w800,
                        color: AppColors.textPrimary,
                      ),
                    ),
                    const SizedBox(height: 4),
                    Text(
                      '${profile.group} · ${profile.account}',
                      style: const TextStyle(
                        fontSize: 13,
                        color: AppColors.textSecondary,
                      ),
                    ),
                  ],
                ),
              ),
            ],
          ),
          const SizedBox(height: 24),
          Row(
            children: [
              _MeMetric(label: '未读消息', value: '$unread'),
              const SizedBox(width: 12),
              _MeMetric(label: '今日接待', value: '$todayServed'),
              const SizedBox(width: 12),
              _MeMetric(
                label: '好评率',
                value: rating == null ? '--' : '${rating!.percent}%',
              ),
            ],
          ),
          const SizedBox(height: 22),
          Container(
            decoration: AppTheme.cardDecoration(),
            child: Column(
              children: [
                _MeMenuTile(
                  icon: Icons.account_circle_outlined,
                  title: '头像修改',
                  onTap: onPickAvatar,
                ),
                const Divider(height: 1, indent: 52),
                _MeMenuTile(
                  icon: Icons.toggle_on_outlined,
                  title: '在线状态',
                  trailing: DropdownButton<String>(
                    value: profile.status,
                    underline: const SizedBox.shrink(),
                    items: const [
                      DropdownMenuItem(value: 'online', child: Text('在线')),
                      DropdownMenuItem(value: 'busy', child: Text('忙碌')),
                      DropdownMenuItem(value: 'offline', child: Text('离线')),
                    ],
                    onChanged: (value) {
                      if (value != null) onStatusChanged(value);
                    },
                  ),
                ),
                const Divider(height: 1, indent: 52),
                _MeMenuTile(
                  icon: Icons.lock_outline,
                  title: '修改密码',
                  onTap: onChangePassword,
                ),
              ],
            ),
          ),
          const SizedBox(height: 22),
          Container(
            decoration: AppTheme.cardDecoration(),
            child: _MeMenuTile(
              icon: Icons.logout,
              title: '退出登录',
              onTap: onLogout,
              danger: true,
            ),
          ),
        ],
      ),
    );
  }
}

class _MeMetric extends StatelessWidget {
  const _MeMetric({required this.label, required this.value});
  final String label;
  final String value;

  @override
  Widget build(BuildContext context) => Expanded(
    child: Container(
      padding: const EdgeInsets.symmetric(vertical: 16),
      decoration: AppTheme.cardDecoration(),
      child: Column(
        children: [
          Text(
            value,
            style: const TextStyle(
              fontSize: 22,
              fontWeight: FontWeight.w800,
              color: AppColors.primary,
            ),
          ),
          const SizedBox(height: 2),
          Text(
            label,
            style: const TextStyle(fontSize: 12, color: AppColors.textSecondary),
          ),
        ],
      ),
    ),
  );
}

class _CustomBottomNav extends StatelessWidget {
  const _CustomBottomNav({
    required this.currentIndex,
    required this.onChanged,
  });
  final int currentIndex;
  final ValueChanged<int> onChanged;

  @override
  Widget build(BuildContext context) {
    return Container(
      decoration: BoxDecoration(
        color: AppColors.surface,
        border: const Border(top: BorderSide(color: AppColors.divider)),
        boxShadow: const [
          BoxShadow(
            color: Color(0x12000000),
            blurRadius: 12,
            offset: Offset(0, -2),
          ),
        ],
      ),
      child: SafeArea(
        top: false,
        child: Row(
          children: [
            _BottomNavItem(
              icon: Icons.chat_bubble_rounded,
              label: '聊天',
              active: currentIndex == 0,
              onTap: () => onChanged(0),
            ),
            _BottomNavItem(
              icon: Icons.person_rounded,
              label: '我的',
              active: currentIndex == 1,
              onTap: () => onChanged(1),
            ),
          ],
        ),
      ),
    );
  }
}

class _BottomNavItem extends StatelessWidget {
  const _BottomNavItem({
    required this.icon,
    required this.label,
    required this.active,
    required this.onTap,
  });
  final IconData icon;
  final String label;
  final bool active;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final color = active ? AppColors.primary : AppColors.textMuted;
    return Expanded(
      child: InkWell(
        onTap: onTap,
        child: Padding(
          padding: const EdgeInsets.symmetric(vertical: 8),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              AnimatedContainer(
                duration: const Duration(milliseconds: 180),
                width: active ? 46 : 30,
                height: 28,
                decoration: BoxDecoration(
                  color: active
                      ? AppColors.primary.withValues(alpha: .12)
                      : Colors.transparent,
                  borderRadius: BorderRadius.circular(AppRadii.input),
                ),
                child: Icon(icon, size: 22, color: color),
              ),
              const SizedBox(height: 3),
              Text(
                label,
                style: TextStyle(
                  fontSize: 11,
                  fontWeight: active ? FontWeight.w700 : FontWeight.w500,
                  color: color,
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class _MeMenuTile extends StatelessWidget {
  const _MeMenuTile({
    required this.icon,
    required this.title,
    this.trailing,
    this.onTap,
    this.danger = false,
  });
  final IconData icon;
  final String title;
  final Widget? trailing;
  final VoidCallback? onTap;
  final bool danger;

  @override
  Widget build(BuildContext context) {
    final color = danger ? AppColors.danger : AppColors.textPrimary;
    return ListTile(
      onTap: onTap,
      leading: Icon(icon, color: color),
      title: Text(
        title,
        style: TextStyle(color: color, fontWeight: FontWeight.w600),
      ),
      trailing: trailing ??
          const Icon(Icons.chevron_right, color: AppColors.textMuted),
    );
  }
}
class ChatPage extends StatefulWidget {
  const ChatPage({
    super.key,
    required this.client,
    required this.conversation,
    this.myAvatarUrl,
  });
  final ServiceBridgeClient client;
  final Conversation conversation;
  final String? myAvatarUrl;
  @override
  State<ChatPage> createState() => _ChatPageState();
}

class _ChatPageState extends State<ChatPage> {
  final _messageController = TextEditingController();
  final _scrollController = ScrollController();
  final _picker = ImagePicker();
  StreamSubscription<Map<String, dynamic>>? _events;
  List<ChatMessage> _messages = const [];
  bool _loading = true;
  bool _sendingImage = false;

  @override
  void initState() {
    super.initState();
    _events = widget.client.events.listen(_handleEvent);
    _loadMessages();
    widget.client.markRead(widget.conversation.id).catchError((_) {});
  }

  void _handleEvent(Map<String, dynamic> event) {
    final type = '${event['event'] ?? ''}';
    final data = Map<String, dynamic>.from(event['data'] as Map? ?? {});
    if ((type == 'message.receive' ||
            type == 'message.ack' ||
            type == 'message.revoked') &&
        data['conversation_id'] == widget.conversation.id) {
      _loadMessages(silent: true);
      widget.client.markRead(widget.conversation.id).catchError((_) {});
    }
    if (type == 'conversation.status_changed' &&
        data['id'] == widget.conversation.id &&
        data['status'] == 'closed') {
      if (mounted) _showMessage(context, '该会话已结束');
    }
  }

  Future<void> _loadMessages({bool silent = false}) async {
    if (!silent && mounted) setState(() => _loading = true);
    try {
      final items = await widget.client.messages(widget.conversation.id);
      if (mounted) {
        setState(() => _messages = items);
        WidgetsBinding.instance.addPostFrameCallback((_) => _scrollToBottom());
      }
    } catch (error) {
      if (mounted) _showMessage(context, _readableError(error));
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  void _sendText() {
    try {
      widget.client.sendMessage(
        widget.conversation.id,
        _messageController.text,
      );
      _messageController.clear();
    } catch (error) {
      _showMessage(context, _readableError(error));
    }
  }

  Future<void> _sendImage() async {
    final image = await _picker.pickImage(
      source: ImageSource.gallery,
      imageQuality: 82,
      maxWidth: 1800,
    );
    if (image == null) return;
    setState(() => _sendingImage = true);
    try {
      final url = await widget.client.upload(File(image.path));
      widget.client.sendMessage(
        widget.conversation.id,
        url,
        messageType: 'image',
      );
    } catch (error) {
      if (mounted) _showMessage(context, _readableError(error));
    } finally {
      if (mounted) setState(() => _sendingImage = false);
    }
  }

  void _scrollToBottom() {
    if (_scrollController.hasClients) {
      _scrollController.animateTo(
        _scrollController.position.maxScrollExtent,
        duration: const Duration(milliseconds: 220),
        curve: Curves.easeOut,
      );
    }
  }

  Future<void> _closeConversation() async {
    if (!await _confirm(context, '结束会话', '结束后访客将不能继续发送消息，确定继续吗？')) return;
    try {
      await widget.client.closeConversation(widget.conversation.id);
      if (mounted) Navigator.pop(context);
    } catch (error) {
      if (mounted) _showMessage(context, _readableError(error));
    }
  }

  @override
  void dispose() {
    _events?.cancel();
    _messageController.dispose();
    _scrollController.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final closed = widget.conversation.status == 'closed';
    return Scaffold(
      appBar: AppBar(
        centerTitle: true,
        title: Column(
          children: [
            Text(
              widget.conversation.visitorLabel,
              style: const TextStyle(fontSize: 17, fontWeight: FontWeight.w700),
            ),
            Text(
              _statusLabel(widget.conversation.status),
              style: const TextStyle(fontSize: 11, color: AppColors.textMuted),
            ),
          ],
        ),
        actions: [
          if (!closed)
            IconButton(
              onPressed: _closeConversation,
              tooltip: '结束会话',
              icon: const Icon(Icons.check_circle_outline),
            ),
        ],
      ),
      body: Column(
        children: [
          Expanded(
            child: _loading && _messages.isEmpty
                ? const Center(child: CircularProgressIndicator())
                : ListView.builder(
                    controller: _scrollController,
                    padding: const EdgeInsets.all(16),
                    itemCount: _messages.length,
                    itemBuilder: (context, index) => _MessageBubble(
                      message: _messages[index],
                      baseUrl: widget.client.baseUrl,
                      myAvatarUrl: widget.myAvatarUrl,
                    ),
                  ),
          ),
          SafeArea(
            top: false,
            child: Container(
              padding: const EdgeInsets.fromLTRB(10, 10, 10, 12),
              decoration: const BoxDecoration(
                color: AppColors.surface,
                border: Border(top: BorderSide(color: AppColors.border)),
              ),
              child: Row(
                children: [
                  IconButton(
                    onPressed: closed || _sendingImage ? null : _sendImage,
                    icon: _sendingImage
                        ? const SizedBox(
                            width: 20,
                            height: 20,
                            child: CircularProgressIndicator(strokeWidth: 2),
                          )
                        : const Icon(Icons.add_photo_alternate_outlined),
                  ),
                  Expanded(
                    child: TextField(
                      controller: _messageController,
                      minLines: 1,
                      maxLines: 5,
                      enabled: !closed,
                      onSubmitted: (_) => _sendText(),
                      decoration: InputDecoration(
                        hintText: closed ? '会话已结束' : '输入回复...',
                        contentPadding: const EdgeInsets.symmetric(
                          horizontal: 14,
                          vertical: 11,
                        ),
                      ),
                    ),
                  ),
                  const SizedBox(width: 8),
                  IconButton.filled(
                    onPressed: closed ? null : _sendText,
                    icon: const Icon(Icons.send_rounded),
                  ),
                ],
              ),
            ),
          ),
        ],
      ),
    );
  }
}

class _MessageBubble extends StatefulWidget {
  const _MessageBubble({
    required this.message,
    required this.baseUrl,
    this.myAvatarUrl,
  });
  final ChatMessage message;
  final String baseUrl;
  final String? myAvatarUrl;

  @override
  State<_MessageBubble> createState() => _MessageBubbleState();
}

class _MessageBubbleState extends State<_MessageBubble> {
  bool _showOriginal = false;

  @override
  Widget build(BuildContext context) {
    final message = widget.message;
    final myAvatarUrl = widget.myAvatarUrl;
    final isMine = message.senderType == 'agent';
    final isAI = message.senderType == 'ai';
    final isSystem = message.senderType == 'system' ||
        message.messageType == 'system';

    if (isSystem) {
      return Center(
        child: Container(
          margin: const EdgeInsets.symmetric(vertical: 8),
          padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 5),
          decoration: BoxDecoration(
            color: AppColors.border,
            borderRadius: BorderRadius.circular(AppRadii.bubble),
          ),
          child: Text(
            message.content,
            style: const TextStyle(fontSize: 12, color: AppColors.textSecondary),
          ),
        ),
      );
    }

    final Widget avatar;
    if (isAI) {
      avatar = const CircleAvatar(
        radius: 18,
        backgroundColor: AppColors.successSoft,
        foregroundColor: AppColors.success,
        child: Icon(Icons.smart_toy_outlined, size: 20),
      );
    } else if (isMine) {
      avatar = _AgentAvatar(
        radius: 18,
        imageUrl: myAvatarUrl,
        baseUrl: baseUrl,
        name: '我',
      );
    } else {
      avatar = const _VisitorAvatar();
    }

    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 6),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        mainAxisAlignment: isMine
            ? MainAxisAlignment.end
            : MainAxisAlignment.start,
        children: [
          if (!isMine) ...[
            avatar,
            const SizedBox(width: 8),
          ],
          Flexible(
            child: Column(
              crossAxisAlignment: isMine
                  ? CrossAxisAlignment.end
                  : CrossAxisAlignment.start,
              children: [
                if (isAI)
                  Container(
                    margin: const EdgeInsets.only(bottom: 4),
                    padding: const EdgeInsets.symmetric(
                      horizontal: 7,
                      vertical: 3,
                    ),
                    decoration: BoxDecoration(
                      color: AppColors.success.withValues(alpha: .12),
                      borderRadius: BorderRadius.circular(AppRadii.chip),
                    ),
                    child: const Text(
                      'AI客服',
                      style: TextStyle(
                        color: AppColors.success,
                        fontSize: 10,
                        fontWeight: FontWeight.w700,
                      ),
                    ),
                  ),
                _buildBubbleBody(context, isMine, isAI),
                const SizedBox(height: 3),
                Text(
                  _formatTime(message.createdAt),
                  style: const TextStyle(
                    fontSize: 10,
                    color: AppColors.textMuted,
                  ),
                ),
              ],
            ),
          ),
          if (isMine) ...[
            const SizedBox(width: 8),
            avatar,
          ],
        ],
      ),
    );
  }

  Widget _buildBubbleBody(BuildContext context, bool isMine, bool isAI) {
    final message = widget.message;
    final baseUrl = widget.baseUrl;
    final bubbleColor = isMine ? AppColors.chatBubbleMine : AppColors.surface;
    final textColor = isMine
        ? AppColors.textPrimary
        : AppColors.textPrimary;

    final Widget content;
    if (message.revoked) {
      content = Text(
        '该消息已撤回',
        style: TextStyle(color: textColor.withValues(alpha: .6), fontSize: 13),
      );
    } else if (message.messageType == 'image') {
      final imageUrl = message.content.startsWith('/')
          ? '$baseUrl${message.content}'
          : message.content;
      content = ClipRRect(
        borderRadius: BorderRadius.circular(AppRadii.chip),
        child: Image.network(
          imageUrl,
          width: 200,
          fit: BoxFit.cover,
          errorBuilder: (_, _, _) => Text(message.content),
        ),
      );
    } else {
      content = Text(
        message.content,
        style: TextStyle(color: textColor, fontSize: 15, height: 1.4),
      );
    }

    // 客服端默认展示中文译文，点击「查看原文」可展开访客/客服输入的原文。
    final original = message.originalContent.trim();
    final hasOriginal =
        original.isNotEmpty && original != message.content.trim();

    return Container(
      constraints: BoxConstraints(
        maxWidth: MediaQuery.sizeOf(context).width * .62,
      ),
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 9),
      decoration: BoxDecoration(
        color: bubbleColor,
        borderRadius: BorderRadius.only(
          topLeft: const Radius.circular(AppRadii.bubble),
          topRight: const Radius.circular(AppRadii.bubble),
          bottomLeft: Radius.circular(
            isMine ? AppRadii.bubble : AppRadii.bubbleTail,
          ),
          bottomRight: Radius.circular(
            isMine ? AppRadii.bubbleTail : AppRadii.bubble,
          ),
        ),
        border: isMine
            ? null
            : Border.all(
                color: isAI ? AppColors.successBorder : AppColors.border,
              ),
      ),
      child: hasOriginal
          ? Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              mainAxisSize: MainAxisSize.min,
              children: [
                content,
                const SizedBox(height: 6),
                GestureDetector(
                  onTap: () =>
                      setState(() => _showOriginal = !_showOriginal),
                  child: Text(
                    _showOriginal ? '收起原文' : '查看原文',
                    style: const TextStyle(
                      fontSize: 11,
                      color: AppColors.primary,
                    ),
                  ),
                ),
                if (_showOriginal) ...[
                  const SizedBox(height: 4),
                  Text(
                    original,
                    style: TextStyle(
                      color: textColor.withValues(alpha: .55),
                      fontSize: 12,
                      height: 1.4,
                    ),
                  ),
                ],
              ],
            )
          : content,
    );
  }
}
class ChangePasswordDialog extends StatefulWidget {
  const ChangePasswordDialog({super.key, required this.client});
  final ServiceBridgeClient client;
  @override
  State<ChangePasswordDialog> createState() => _ChangePasswordDialogState();
}

class _ChangePasswordDialogState extends State<ChangePasswordDialog> {
  final _formKey = GlobalKey<FormState>();
  final _current = TextEditingController();
  final _next = TextEditingController();
  bool _busy = false;
  @override
  void dispose() {
    _current.dispose();
    _next.dispose();
    super.dispose();
  }

  Future<void> _save() async {
    if (!_formKey.currentState!.validate()) return;
    setState(() => _busy = true);
    try {
      await widget.client.changePassword(_current.text, _next.text);
      if (mounted) {
        Navigator.pop(context);
        _showMessage(context, '密码已修改，请重新登录');
      }
    } catch (error) {
      if (mounted) _showMessage(context, _readableError(error));
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) => AlertDialog(
    title: const Text('修改密码'),
    content: Form(
      key: _formKey,
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          TextFormField(
            controller: _current,
            obscureText: true,
            decoration: const InputDecoration(labelText: '当前密码'),
            validator: _required,
          ),
          const SizedBox(height: 12),
          TextFormField(
            controller: _next,
            obscureText: true,
            decoration: const InputDecoration(labelText: '新密码（至少 10 位）'),
            validator: (value) =>
                (value ?? '').length < 10 ? '新密码至少 10 位' : null,
          ),
        ],
      ),
    ),
    actions: [
      TextButton(
        onPressed: _busy ? null : () => Navigator.pop(context),
        child: const Text('取消'),
      ),
      FilledButton(onPressed: _busy ? null : _save, child: const Text('保存')),
    ],
  );
}

String? _required(String? value) =>
    (value ?? '').trim().isEmpty ? '此项不能为空' : null;
String _readableError(Object error) => error is ServiceBridgeException
    ? error.message
    : error.toString().replaceFirst('Exception: ', '');
String _formatTime(DateTime? value) {
  if (value == null) return '';
  final now = DateTime.now();
  if (now.difference(value).inDays == 0) {
    return '${value.hour.toString().padLeft(2, '0')}:${value.minute.toString().padLeft(2, '0')}';
  }
  return '${value.month}/${value.day} ${value.hour.toString().padLeft(2, '0')}:${value.minute.toString().padLeft(2, '0')}';
}

String _statusLabel(String status) {
  switch (status) {
    case 'waiting':
    case 'human_requested':
      return '待接入';
    case 'ai_serving':
      return 'AI接待中';
    case 'closed':
      return '已结束';
    case 'assigned':
      return '服务中';
    default:
      return status;
  }
}

void _showMessage(BuildContext context, String message) {
  ScaffoldMessenger.maybeOf(
    context,
  )?.showSnackBar(SnackBar(content: Text(message)));
}

Future<bool> _confirm(
  BuildContext context,
  String title,
  String message,
) async =>
    await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: Text(title),
        content: Text(message),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, true),
            child: const Text('确定'),
          ),
        ],
      ),
    ) ??
    false;
