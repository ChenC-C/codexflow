import 'package:flutter/material.dart';
import 'package:provider/provider.dart';

import '../state/app_model.dart';
import '../theme/palette.dart';
import '../widgets/common.dart';

class SettingsScreen extends StatefulWidget {
  const SettingsScreen({super.key});

  @override
  State<SettingsScreen> createState() => _SettingsScreenState();
}

class _SettingsScreenState extends State<SettingsScreen> {
  late final TextEditingController _controller;
  bool _didBindController = false;

  @override
  void initState() {
    super.initState();
    _controller = TextEditingController();
  }

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    if (!_didBindController) {
      _controller.text = context.read<AppModel>().baseUrlString;
      _didBindController = true;
    }
  }

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final model = context.watch<AppModel>();
    final connectionTone =
        model.isAgentOnline ? Palette.accent : Palette.danger;

    return Scaffold(
      backgroundColor: Palette.canvas,
      appBar: AppBar(
        title: Text('设置',
            style: roundedTextStyle(size: 17, weight: FontWeight.w600)),
        centerTitle: true,
      ),
      body: PageScaffold(
        child: ListView(
          padding: const EdgeInsets.fromLTRB(16, 12, 16, 20),
          children: <Widget>[
            PanelCard(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: <Widget>[
                  Text('连接设置',
                      style:
                          roundedTextStyle(size: 16, weight: FontWeight.w600)),
                  const SizedBox(height: 12),
                  CodexTextField(
                    controller: _controller,
                    hintText: 'http://192.168.1.4:4318',
                  ),
                  const SizedBox(height: 12),
                  Text(
                    '填写 Mac 可被手机访问到的局域网地址，例如 `http://192.168.1.4:4318`。不要填 `0.0.0.0`，真机上也不要填 `127.0.0.1`。',
                    style: roundedTextStyle(
                      size: 13,
                      weight: FontWeight.w500,
                      color: Palette.mutedInk,
                      height: 1.45,
                    ),
                  ),
                  const SizedBox(height: 12),
                  Row(
                    children: <Widget>[
                      Expanded(
                        child: ActionButton(
                          title: '保存并刷新',
                          background: Palette.accent,
                          foreground: Colors.white,
                          fontSize: 14,
                          onPressed: () async {
                            FocusScope.of(context).unfocus();
                            model.updateBaseUrlString(_controller.text);
                            await model.saveBaseUrl();
                            await model.refreshDashboard();
                          },
                        ),
                      ),
                      const SizedBox(width: 10),
                      Expanded(
                        child: ActionButton(
                          title: '重新连接',
                          background: Palette.softBlue.appOpacity(0.14),
                          foreground: Palette.softBlue,
                          fontSize: 14,
                          onPressed: () async {
                            FocusScope.of(context).unfocus();
                            model.updateBaseUrlString(_controller.text);
                            await model.refreshDashboard();
                          },
                        ),
                      ),
                    ],
                  ),
                  if (model.dashboard.agent.connected &&
                      model.dashboard.agent.listenAddr.isNotEmpty) ...<Widget>[
                    const SizedBox(height: 12),
                    Text(
                      'Agent 当前监听：${model.dashboard.agent.listenAddr}',
                      style: roundedTextStyle(
                          size: 12,
                          weight: FontWeight.w500,
                          color: Palette.mutedInk),
                    ),
                  ],
                ],
              ),
            ),
            const SizedBox(height: 12),
            PanelCard(
              compact: true,
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: <Widget>[
                  Row(
                    children: <Widget>[
                      Text('当前连接',
                          style: roundedTextStyle(
                              size: 16, weight: FontWeight.w600)),
                      const Spacer(),
                      Container(
                        padding: const EdgeInsets.symmetric(
                            horizontal: 10, vertical: 6),
                        decoration: BoxDecoration(
                          color: connectionTone.appOpacity(0.12),
                          borderRadius: BorderRadius.circular(999),
                        ),
                        child: Row(
                          children: <Widget>[
                            Container(
                              width: 8,
                              height: 8,
                              decoration: BoxDecoration(
                                  color: connectionTone,
                                  shape: BoxShape.circle),
                            ),
                            const SizedBox(width: 6),
                            Text(
                              model.isAgentOnline ? '在线' : '离线',
                              style: roundedTextStyle(
                                  size: 12,
                                  weight: FontWeight.w700,
                                  color: connectionTone),
                            ),
                          ],
                        ),
                      ),
                    ],
                  ),
                  const SizedBox(height: 10),
                  _SettingsInfoRow(title: '入口地址', value: _controller.text),
                  const SizedBox(height: 8),
                  _SettingsInfoRow(
                    title: '监听地址',
                    value: model.dashboard.agent.listenAddr.isEmpty
                        ? '未发现'
                        : model.dashboard.agent.listenAddr,
                  ),
                  const SizedBox(height: 8),
                  _SettingsInfoRow(
                      title: 'Codex 路径',
                      value: model.dashboard.agent.codexBinaryPath),
                  if (model.agentConnectionError.isNotEmpty) ...<Widget>[
                    const SizedBox(height: 10),
                    Container(
                      width: double.infinity,
                      padding: const EdgeInsets.all(10),
                      decoration: BoxDecoration(
                        color: Palette.danger.appOpacity(0.08),
                        borderRadius: BorderRadius.circular(12),
                      ),
                      child: Text(
                        model.agentConnectionError,
                        style: roundedTextStyle(
                            size: 12,
                            weight: FontWeight.w500,
                            color: Palette.danger),
                      ),
                    ),
                  ],
                ],
              ),
            ),
            const SizedBox(height: 12),
            const PanelCard(
              compact: true,
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: <Widget>[
                  Text(
                    '使用说明',
                    style: TextStyle(
                        fontSize: 16,
                        fontWeight: FontWeight.w600,
                        color: Palette.ink),
                  ),
                  SizedBox(height: 8),
                  Text(
                    '1. Mac 上先启动 Agent。',
                    style: TextStyle(
                        fontSize: 13,
                        fontWeight: FontWeight.w500,
                        color: Palette.mutedInk),
                  ),
                  SizedBox(height: 4),
                  Text(
                    '2. 手机填 Mac 的局域网地址。',
                    style: TextStyle(
                        fontSize: 13,
                        fontWeight: FontWeight.w500,
                        color: Palette.mutedInk),
                  ),
                  SizedBox(height: 4),
                  Text(
                    '3. 首页看会话，审批页处理授权。',
                    style: TextStyle(
                        fontSize: 13,
                        fontWeight: FontWeight.w500,
                        color: Palette.mutedInk),
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

class _SettingsInfoRow extends StatelessWidget {
  const _SettingsInfoRow({
    required this.title,
    required this.value,
  });

  final String title;
  final String value;

  @override
  Widget build(BuildContext context) {
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 10),
      decoration: BoxDecoration(
        color: Palette.shell,
        borderRadius: BorderRadius.circular(12),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          Text(
            title,
            style: roundedTextStyle(
                size: 11, weight: FontWeight.w700, color: Palette.mutedInk),
          ),
          const SizedBox(height: 4),
          Text(
            value,
            style: roundedTextStyle(
              size: 12,
              weight: FontWeight.w500,
              color: Palette.ink,
              fontFamily: 'monospace',
            ),
          ),
        ],
      ),
    );
  }
}
