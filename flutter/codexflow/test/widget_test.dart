import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:provider/provider.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'package:codexflow_flutter/main.dart';
import 'package:codexflow_flutter/models/app_models.dart';
import 'package:codexflow_flutter/screens/session_detail_screen.dart';
import 'package:codexflow_flutter/screens/artifacts_screen.dart';
import 'package:codexflow_flutter/services/api_client.dart';
import 'package:codexflow_flutter/state/app_model.dart';
import 'package:codexflow_flutter/theme/palette.dart';
import 'package:codexflow_flutter/widgets/common.dart';

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
    final artifact = testArtifact();

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

  testWidgets('markdown inline code remains readable on light message cards', (
    WidgetTester tester,
  ) async {
    const expectedInlineCodeText = Color.fromRGBO(42, 91, 143, 1);
    const expectedInlineCodeBackground = Color.fromRGBO(232, 241, 249, 1);

    await tester.pumpWidget(
      const MaterialApp(
        home: Scaffold(
          body: MarkdownBodyBlock(
            raw: 'Set `baseUrl` to `/v1`.\n\n```text\nbaseUrl\n```',
          ),
        ),
      ),
    );

    final spans = tester
        .widgetList<RichText>(find.byType(RichText))
        .expand((richText) => flattenTextSpans(richText.text))
        .where((span) => span.text == 'baseUrl')
        .toList();

    expect(spans.length, greaterThanOrEqualTo(2));
    expect(spans.first.style?.color, expectedInlineCodeText);
    expect(spans.first.style?.backgroundColor, expectedInlineCodeBackground);
    expect(spans.last.style?.color, Palette.codeText);
  });

  test('startTurn sends uploaded files as file inputs', () async {
    final api = ApiClient(
      baseUrlString: 'http://127.0.0.1:4318',
      client: MockClient((http.Request request) async {
        expect(request.method, 'POST');
        expect(request.url.path, '/api/v1/sessions/thread-1/turns/start');

        final body = jsonDecode(request.body) as Map<String, dynamic>;
        expect(body['inputs'], <Map<String, String>>[
          <String, String>{'type': 'text', 'text': 'Review this file'},
          <String, String>{'type': 'image', 'uploadId': 'image-1'},
          <String, String>{'type': 'file', 'uploadId': 'file-1'},
        ]);

        return http.Response(
          jsonEncode(<String, dynamic>{
            'id': 'turn-1',
            'status': 'completed',
            'items': <dynamic>[],
          }),
          201,
          headers: <String, String>{'content-type': 'application/json'},
        );
      }),
    );

    await api.startTurn(
      sessionId: 'thread-1',
      prompt: 'Review this file',
      imageUploadIds: const <String>['image-1'],
      fileUploadIds: const <String>['file-1'],
    );
  });

  testWidgets('previewable artifacts expose preview and download actions', (
    WidgetTester tester,
  ) async {
    await pumpArtifactCard(tester, testArtifact());

    expect(find.text('预览'), findsOneWidget);
    expect(find.text('下载'), findsOneWidget);
  });

  testWidgets('download action uses native download channel', (
    WidgetTester tester,
  ) async {
    const downloadChannel = MethodChannel('codexflow/download_file');
    MethodCall? capturedCall;
    tester.binding.defaultBinaryMessenger.setMockMethodCallHandler(
      downloadChannel,
      (MethodCall call) async {
        capturedCall = call;
        return true;
      },
    );
    addTearDown(() {
      tester.binding.defaultBinaryMessenger.setMockMethodCallHandler(
        downloadChannel,
        null,
      );
    });

    await pumpArtifactCard(tester, testArtifact());
    await tester.tap(find.text('下载'));
    await tester.pump();

    expect(capturedCall?.method, 'downloadFile');
    expect(capturedCall?.arguments, <String, String>{
      'url': 'http://127.0.0.1:4318/api/v1/artifacts/artifact-1',
      'fileName': 'preview.png',
      'mimeType': 'image/png',
    });
  });

  testWidgets('download action shows saved location after native download', (
    WidgetTester tester,
  ) async {
    const downloadChannel = MethodChannel('codexflow/download_file');
    tester.binding.defaultBinaryMessenger.setMockMethodCallHandler(
      downloadChannel,
      (MethodCall call) async {
        return <String, Object>{'ok': true, 'path': 'Download/preview.png'};
      },
    );
    addTearDown(() {
      tester.binding.defaultBinaryMessenger.setMockMethodCallHandler(
        downloadChannel,
        null,
      );
    });

    await pumpArtifactCard(tester, testArtifact());
    await tester.tap(find.text('下载'));
    await tester.pump();
    await tester.pump();

    expect(find.text('已下载到 Download/preview.png'), findsOneWidget);
  });
}

ArtifactRef testArtifact() {
  return ArtifactRef(
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
}

Future<void> pumpArtifactCard(WidgetTester tester, ArtifactRef artifact) async {
  SharedPreferences.setMockInitialValues(<String, Object>{});
  final prefs = await SharedPreferences.getInstance();
  final model = AppModel(prefs);

  await tester.pumpWidget(
    ChangeNotifierProvider<AppModel>.value(
      value: model,
      child: MaterialApp(
        home: Scaffold(body: ArtifactCard(artifact: artifact)),
      ),
    ),
  );
}

Iterable<TextSpan> flattenTextSpans(InlineSpan span) sync* {
  if (span is TextSpan) {
    yield span;
    final children = span.children;
    if (children != null) {
      for (final child in children) {
        yield* flattenTextSpans(child);
      }
    }
  }
}
