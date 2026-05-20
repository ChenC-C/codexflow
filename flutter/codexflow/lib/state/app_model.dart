import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../models/app_models.dart';
import '../services/api_client.dart';

enum ApprovalActionType { choice, decision, submitText }

class ApprovalAction {
  ApprovalAction.choice(this.value) : type = ApprovalActionType.choice;

  ApprovalAction.decision(this.value) : type = ApprovalActionType.decision;

  ApprovalAction.submitText(this.value) : type = ApprovalActionType.submitText;

  final ApprovalActionType type;
  final Object? value;

  String get freeformText => value is String ? value as String : '';

  String get choiceValue {
    switch (type) {
      case ApprovalActionType.choice:
        return asString(value);
      case ApprovalActionType.decision:
        return value is String ? value as String : 'accept';
      case ApprovalActionType.submitText:
        return 'accept';
    }
  }

  Object? get decisionValue => value;
}

class AppModel extends ChangeNotifier {
  AppModel(this._prefs)
    : baseUrlString = _prefs.getString(_baseUrlKey) ?? 'http://127.0.0.1:4318';

  static const _baseUrlKey = 'codexflow.baseURL';

  final SharedPreferences _prefs;

  String baseUrlString;
  DashboardResponse dashboard = DashboardResponse.placeholder();
  final Map<String, SessionDetail> sessionDetails = <String, SessionDetail>{};
  List<ArtifactRef> artifacts = const <ArtifactRef>[];
  bool isRefreshing = false;
  bool isRefreshingArtifacts = false;
  bool isBootstrapped = false;
  bool isAgentOnline = false;
  String agentConnectionError = '';
  String connectionError = '';
  String artifactError = '';
  String composerDraft = '';
  int _consecutiveDashboardFailures = 0;

  ApiClient _client() => ApiClient(baseUrlString: baseUrlString);

  Future<void> bootstrap() async {
    if (isBootstrapped) {
      return;
    }
    isBootstrapped = true;
    notifyListeners();
    await refreshDashboard();
  }

  void updateBaseUrlString(String value) {
    baseUrlString = value;
    notifyListeners();
  }

  Future<void> saveBaseUrl() async {
    await _prefs.setString(_baseUrlKey, baseUrlString);
  }

  Future<void> refreshDashboard() async {
    if (isRefreshing) {
      return;
    }
    isRefreshing = true;
    notifyListeners();

    try {
      final latestDashboard = await _client().dashboard();
      dashboard = latestDashboard;
      _consecutiveDashboardFailures = 0;
      isAgentOnline = latestDashboard.agent.connected;
      agentConnectionError = '';
    } catch (error) {
      _consecutiveDashboardFailures += 1;
      if (_consecutiveDashboardFailures >= 2 || !isAgentOnline) {
        isAgentOnline = false;
        agentConnectionError = error.toString();
      }
    } finally {
      isRefreshing = false;
      notifyListeners();
    }
  }

  Future<void> refreshArtifacts({bool forceScan = false}) async {
    if (isRefreshingArtifacts) {
      return;
    }
    isRefreshingArtifacts = true;
    notifyListeners();

    try {
      artifacts = forceScan
          ? await _client().refreshArtifacts()
          : await _client().listArtifacts();
      artifactError = '';
    } catch (error) {
      artifactError = error.toString();
    } finally {
      isRefreshingArtifacts = false;
      notifyListeners();
    }
  }

  String resolveAgentUrl(String path) {
    return _client().resolveUrl(path);
  }

  List<PendingRequestView> approvalsFor(String sessionId) {
    final approvals =
        dashboard.approvals.where((item) => item.threadId == sessionId).toList()
          ..sort((left, right) => left.createdAt.compareTo(right.createdAt));
    return approvals;
  }

  Future<void> loadSession(String id) async {
    try {
      final detail = await _client().sessionDetail(id);
      sessionDetails[id] = detail;
      connectionError = '';
      notifyListeners();
    } catch (error) {
      connectionError = error.toString();
      notifyListeners();
    }
  }

  Future<bool> startSession({
    required String cwd,
    required String prompt,
  }) async {
    try {
      await _client().startSession(cwd: cwd.trim(), prompt: prompt.trim());
      await refreshDashboard();
      return true;
    } catch (error) {
      connectionError = error.toString();
      notifyListeners();
      return false;
    }
  }

  Future<void> resumeSession(SessionSummary session) async {
    try {
      final updatedSession = await _client().resumeSession(session.id);
      _upsertSessionSummary(updatedSession);
      await refreshDashboard();
      await loadSession(session.id);
    } catch (error) {
      connectionError = error.toString();
      notifyListeners();
    }
  }

  Future<void> archiveSession(SessionSummary session) async {
    try {
      await _client().archiveSession(session.id);
      sessionDetails.remove(session.id);
      await refreshDashboard();
    } catch (error) {
      connectionError = error.toString();
      notifyListeners();
    }
  }

  Future<void> endSession(SessionSummary session) async {
    try {
      await _client().endSession(session.id);
      await refreshDashboard();
      await loadSession(session.id);
    } catch (error) {
      connectionError = error.toString();
      notifyListeners();
    }
  }

  Future<bool> submitPrompt({
    required SessionSummary session,
    required String prompt,
    List<String> imageUploadIds = const <String>[],
    List<String> fileUploadIds = const <String>[],
  }) async {
    final trimmed = prompt.trim();
    if (trimmed.isEmpty && imageUploadIds.isEmpty && fileUploadIds.isEmpty) {
      return false;
    }

    try {
      if (session.lastTurnStatus == 'inProgress' &&
          session.lastTurnId.isNotEmpty) {
        await _client().steerTurn(
          sessionId: session.id,
          turnId: session.lastTurnId,
          prompt: trimmed,
          imageUploadIds: imageUploadIds,
          fileUploadIds: fileUploadIds,
        );
      } else {
        await _client().startTurn(
          sessionId: session.id,
          prompt: trimmed,
          imageUploadIds: imageUploadIds,
          fileUploadIds: fileUploadIds,
        );
      }
      await refreshDashboard();
      await loadSession(session.id);
      return true;
    } catch (error) {
      connectionError = error.toString();
      notifyListeners();
      return false;
    }
  }

  Future<UploadedImageRef?> uploadImage({
    required Uint8List bytes,
    required String fileName,
  }) async {
    try {
      final ref = await _client().uploadImage(bytes: bytes, fileName: fileName);
      return ref;
    } catch (error) {
      connectionError = error.toString();
      notifyListeners();
      return null;
    }
  }

  Future<UploadedFileRef?> uploadFile({
    required Uint8List bytes,
    required String fileName,
  }) async {
    try {
      final ref = await _client().uploadFile(bytes: bytes, fileName: fileName);
      return ref;
    } catch (error) {
      connectionError = error.toString();
      notifyListeners();
      return null;
    }
  }

  Future<void> interrupt(SessionSummary session) async {
    if (session.lastTurnId.isEmpty) {
      return;
    }
    try {
      await _client().interruptTurn(
        sessionId: session.id,
        turnId: session.lastTurnId,
      );
      await refreshDashboard();
    } catch (error) {
      connectionError = error.toString();
      notifyListeners();
    }
  }

  Future<void> resolve({
    required PendingRequestView approval,
    required ApprovalAction action,
  }) async {
    try {
      await _client().resolveApproval(
        id: approval.id,
        result: _buildResult(approval, action),
      );
      await refreshDashboard();
      final session = dashboard.sessions.cast<SessionSummary?>().firstWhere(
        (item) => item?.id == approval.threadId,
        orElse: () => null,
      );
      if (session != null) {
        await loadSession(session.id);
      }
    } catch (error) {
      connectionError = error.toString();
      notifyListeners();
    }
  }

  Object? _buildResult(PendingRequestView approval, ApprovalAction action) {
    switch (approval.kind) {
      case 'command':
      case 'fileChange':
        return <String, dynamic>{'decision': action.decisionValue};
      case 'permissions':
        final choice = action.choiceValue;
        Object? permissions;
        switch (choice) {
          case 'session':
          case 'turn':
            permissions = approval.params['permissions'] ?? <String, dynamic>{};
            break;
          default:
            permissions = <String, dynamic>{
              'network': null,
              'fileSystem': null,
            };
        }

        Object? scope;
        switch (choice) {
          case 'session':
          case 'turn':
            scope = choice;
            break;
          default:
            scope = null;
        }

        return <String, dynamic>{'permissions': permissions, 'scope': scope};
      case 'userInput':
        final questionId = _firstQuestionId(approval.params) ?? 'reply';
        return <String, dynamic>{
          'answers': <String, dynamic>{
            questionId: <String, dynamic>{
              'answers': <String>[action.freeformText],
            },
          },
        };
      default:
        return <String, dynamic>{'decision': action.choiceValue};
    }
  }

  String? _firstQuestionId(Map<String, dynamic> params) {
    final questions = asList(params['questions']);
    for (final question in questions) {
      final object = asMap(question);
      final id = asString(object['id']);
      if (id.isNotEmpty) {
        return id;
      }
    }
    return null;
  }

  void _upsertSessionSummary(SessionSummary session) {
    final sessions = <SessionSummary>[...dashboard.sessions];
    final existingIndex = sessions.indexWhere((item) => item.id == session.id);
    if (existingIndex >= 0) {
      sessions[existingIndex] = session;
    } else {
      sessions.add(session);
    }

    sessions.sort((left, right) {
      if (left.updatedAt == right.updatedAt) {
        return left.id.compareTo(right.id);
      }
      return right.updatedAt.compareTo(left.updatedAt);
    });

    dashboard = DashboardResponse(
      agent: dashboard.agent,
      stats: dashboard.stats,
      sessions: sessions,
      approvals: dashboard.approvals,
    );
    notifyListeners();
  }
}
