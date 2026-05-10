import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'package:codexflow_flutter/main.dart';
import 'package:codexflow_flutter/models/app_models.dart';
import 'package:codexflow_flutter/screens/session_detail_screen.dart';

void main() {
  testWidgets('renders CodexFlow shell', (WidgetTester tester) async {
    SharedPreferences.setMockInitialValues(<String, Object>{});
    final prefs = await SharedPreferences.getInstance();

    await tester.pumpWidget(CodexFlowApp(prefs: prefs));
    await tester.pumpAndSettle();

    expect(find.text('会话'), findsWidgets);
    expect(find.text('审批'), findsOneWidget);
    expect(find.text('文件'), findsOneWidget);
    expect(find.text('设置'), findsOneWidget);
  });

  testWidgets('renders inline artifact action inside message', (
    WidgetTester tester,
  ) async {
    final artifact = ArtifactRef(
      id: 'artifact-1',
      name: 'preview.png',
      path: 'mobile-download/preview.png',
      kind: 'image',
      mimeType: 'image/png',
      size: 1024,
      modTime: DateTime.utc(2026, 4, 30),
      downloadUrl: '/api/v1/artifacts/artifact-1',
      previewUrl: '/api/v1/artifacts/artifact-1/preview',
      sourceText: 'mobile-download/preview.png',
    );

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: InlineArtifactActions(artifacts: <ArtifactRef>[artifact]),
        ),
      ),
    );

    expect(find.text('本条回答可直接打开'), findsOneWidget);
    expect(find.text('mobile-download/preview.png'), findsOneWidget);
  });
}
