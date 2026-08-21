import 'dart:convert';
import 'dart:io';

import 'package:flutter/services.dart';

import 'service_bridge.dart';
import 'package:http/http.dart' as http;

class AppUpdateInfo {
  const AppUpdateInfo({
    required this.updateAvailable,
    required this.forceUpdate,
    required this.latestVersion,
    required this.latestVersionCode,
    required this.releaseNotes,
    required this.downloadUrl,
    required this.checksum,
  });

  final bool updateAvailable;
  final bool forceUpdate;
  final String latestVersion;
  final int latestVersionCode;
  final String releaseNotes;
  final String downloadUrl;
  final String checksum;
}

class AppUpdater {
  static const MethodChannel _channel = MethodChannel(
    'com.astreva.online_customer_service/updater',
  );

  static const int currentVersionCode = int.fromEnvironment(
    'APP_VERSION_CODE',
    defaultValue: 1,
  );

  static bool get isAndroid => Platform.isAndroid;
  static bool get isIOS => Platform.isIOS;

  static Future<AppUpdateInfo> checkForUpdate(String baseUrl) async {
    final uri = Uri.parse('$baseUrl/api/app-update/latest').replace(
      queryParameters: {
        'platform': isAndroid ? 'android' : 'ios',
        'version_code': '$currentVersionCode',
      },
    );
    final response = await http.get(uri).timeout(const Duration(seconds: 12));
    if (response.statusCode != 200) {
      throw HttpException('update check failed: ${response.statusCode}');
    }
    final payload = jsonDecode(response.body) as Map<String, dynamic>;
    final latest = payload['latest'] as Map<String, dynamic>?;
    if (latest == null) {
      return const AppUpdateInfo(
        updateAvailable: false,
        forceUpdate: false,
        latestVersion: '',
        latestVersionCode: 0,
        releaseNotes: '',
        downloadUrl: '',
        checksum: '',
      );
    }
    final versionCode = (latest['version_code'] as num?)?.toInt() ?? 0;
    final downloadUrl = (latest['download_url'] as String? ?? '').trim();
    return AppUpdateInfo(
      updateAvailable: payload['update_available'] == true,
      forceUpdate: payload['force_update'] == true,
      latestVersion: latest['version_name'] as String? ?? '',
      latestVersionCode: versionCode,
      releaseNotes: latest['release_notes'] as String? ?? '',
      downloadUrl: downloadUrl.isEmpty
          ? '$baseUrl/api/app-update/download/${latest['id'] ?? ''}'
          : downloadUrl,
      checksum: latest['checksum'] as String? ?? '',
    );
  }

  static Future<bool> canInstallUnknownApps() async {
    if (!isAndroid) return true;
    try {
      return await _channel.invokeMethod<bool>('canInstallUnknownApps') ??
          false;
    } catch (_) {
      return false;
    }
  }

  static Future<void> openInstallSettings() async {
    if (!isAndroid) return;
    try {
      await _channel.invokeMethod('openInstallSettings');
    } catch (_) {
      // Settings intent is optional; the user can enable it manually.
    }
  }

  static Future<String> downloadApk(
    String url, {
    void Function(int received, int? total)? onProgress,
  }) async {
    if (!isAndroid) {
      throw const ServiceBridgeException('当前设备不能直接安装更新包，请前往应用商店更新。');
    }
    final request = http.Request('GET', Uri.parse(url));
    final response = await request.send().timeout(const Duration(minutes: 10));
    if (response.statusCode != 200) {
      throw ServiceBridgeException('安装包下载失败：HTTP ${response.statusCode}');
    }
    final contentLength = response.contentLength;
    final directory = Directory('${Directory.systemTemp.path}/updates');
    if (!await directory.exists()) {
      await directory.create(recursive: true);
    }
    final file = File(
      '${directory.path}/astreva-online-service-${DateTime.now().millisecondsSinceEpoch}.apk',
    );
    final sink = file.openWrite();
    var received = 0;
    await for (final chunk in response.stream) {
      received += chunk.length;
      sink.add(chunk);
      onProgress?.call(received, contentLength);
    }
    await sink.close();
    return file.path;
  }

  static Future<void> installApk(String path) async {
    if (!isAndroid) {
      throw const ServiceBridgeException('当前设备不能直接安装更新包。');
    }
    await _channel.invokeMethod('installApk', {'path': path});
  }
}
