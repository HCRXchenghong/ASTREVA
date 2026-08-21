import 'package:flutter_test/flutter_test.dart';

import 'package:online_customer_service/main.dart';

void main() {
  testWidgets('shows the agent login screen', (tester) async {
    await tester.pumpWidget(const CustomerServiceApp());
    await tester.pumpAndSettle();

    expect(find.text('在线客服'), findsOneWidget);
    expect(find.text('客服账号'), findsOneWidget);
    expect(find.text('服务器地址'), findsOneWidget);
    expect(find.text('登录工作台'), findsOneWidget);
  });
}
