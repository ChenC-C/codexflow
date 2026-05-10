import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:provider/provider.dart';

import '../models/app_models.dart';
import '../state/app_model.dart';
import '../theme/palette.dart';
import '../widgets/common.dart';

const MethodChannel _openUrlChannel = MethodChannel('codexflow/open_url');

class ArtifactsScreen extends StatefulWidget {
  const ArtifactsScreen({super.key});

  @override
  State<ArtifactsScreen> createState() => _ArtifactsScreenState();
}

class _ArtifactsScreenState extends State<ArtifactsScreen> {
  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      unawaited(context.read<AppModel>().refreshArtifacts());
    });
  }

  @override
  Widget build(BuildContext context) {
    final model = context.watch<AppModel>();
    return Scaffold(
      backgroundColor: Palette.canvas,
      appBar: AppBar(
        title: Text(
          '文件',
          style: roundedTextStyle(size: 17, weight: FontWeight.w600),
        ),
        centerTitle: true,
      ),
      body: PageScaffold(
        child: RefreshIndicator(
          color: Palette.accent,
          onRefresh: () => model.refreshArtifacts(forceScan: true),
          child: ListView(
            padding: const EdgeInsets.fromLTRB(16, 12, 16, 20),
            children: <Widget>[
              PanelCard(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: <Widget>[
                    Row(
                      children: <Widget>[
                        const Icon(
                          Icons.folder_open_rounded,
                          size: 18,
                          color: Palette.accent,
                        ),
                        const SizedBox(width: 8),
                        Text(
                          '手机可打开产物',
                          style: roundedTextStyle(
                            size: 16,
                            weight: FontWeight.w600,
                          ),
                        ),
                        const Spacer(),
                        Text(
                          '${model.artifacts.length}',
                          style: roundedTextStyle(
                            size: 12,
                            weight: FontWeight.w700,
                            color: Palette.mutedInk,
                          ),
                        ),
                      ],
                    ),
                    const SizedBox(height: 8),
                    Text(
                      '这里会列出 mobile-download 和本轮任务生成的 PDF、图片、APK、压缩包。点击后会用手机系统浏览器或文件查看器打开。',
                      style: roundedTextStyle(
                        size: 13,
                        weight: FontWeight.w500,
                        color: Palette.mutedInk,
                        height: 1.45,
                      ),
                    ),
                    const SizedBox(height: 12),
                    ActionButton(
                      title: model.isRefreshingArtifacts ? '刷新中…' : '重新扫描文件',
                      background: Palette.accent,
                      foreground: Colors.white,
                      icon: Icons.refresh,
                      enabled: !model.isRefreshingArtifacts,
                      onPressed: () {
                        unawaited(model.refreshArtifacts(forceScan: true));
                      },
                    ),
                  ],
                ),
              ),
              if (model.artifactError.isNotEmpty) ...<Widget>[
                const SizedBox(height: 12),
                PanelCard(
                  compact: true,
                  child: Text(
                    model.artifactError,
                    style: roundedTextStyle(
                      size: 12,
                      weight: FontWeight.w500,
                      color: Palette.danger,
                    ),
                  ),
                ),
              ],
              const SizedBox(height: 12),
              if (model.isRefreshingArtifacts && model.artifacts.isEmpty)
                const _LoadingArtifactsCard()
              else if (model.artifacts.isEmpty)
                const _EmptyArtifactsCard()
              else
                ...model.artifacts.map(
                  (artifact) => Padding(
                    padding: const EdgeInsets.only(bottom: 12),
                    child: ArtifactCard(artifact: artifact),
                  ),
                ),
            ],
          ),
        ),
      ),
    );
  }
}

class _LoadingArtifactsCard extends StatelessWidget {
  const _LoadingArtifactsCard();

  @override
  Widget build(BuildContext context) {
    return PanelCard(
      compact: true,
      child: Row(
        children: <Widget>[
          const SizedBox(
            width: 16,
            height: 16,
            child: CircularProgressIndicator(
              strokeWidth: 2,
              color: Palette.accent,
            ),
          ),
          const SizedBox(width: 12),
          Text(
            '正在扫描可打开文件…',
            style: roundedTextStyle(
              size: 13,
              weight: FontWeight.w500,
              color: Palette.mutedInk,
            ),
          ),
        ],
      ),
    );
  }
}

class _EmptyArtifactsCard extends StatelessWidget {
  const _EmptyArtifactsCard();

  @override
  Widget build(BuildContext context) {
    return PanelCard(
      compact: true,
      child: Text(
        '暂时没有发现可手机打开的文件。生成 PDF、图片、APK 或压缩包后，下拉刷新即可看到。',
        style: roundedTextStyle(
          size: 13,
          weight: FontWeight.w500,
          color: Palette.mutedInk,
          height: 1.45,
        ),
      ),
    );
  }
}

class ArtifactCard extends StatelessWidget {
  const ArtifactCard({super.key, required this.artifact});

  final ArtifactRef artifact;

  @override
  Widget build(BuildContext context) {
    final tone = _kindTone(artifact.kind);
    return PanelCard(
      compact: true,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: <Widget>[
              Container(
                width: 38,
                height: 38,
                decoration: BoxDecoration(
                  color: tone.appOpacity(0.12),
                  borderRadius: BorderRadius.circular(12),
                ),
                child: Icon(_kindIcon(artifact.kind), size: 18, color: tone),
              ),
              const SizedBox(width: 10),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: <Widget>[
                    Text(
                      artifact.name,
                      maxLines: 2,
                      overflow: TextOverflow.ellipsis,
                      style: roundedTextStyle(size: 15, weight: FontWeight.w600),
                    ),
                    const SizedBox(height: 4),
                    Text(
                      artifact.displayPath,
                      maxLines: 2,
                      overflow: TextOverflow.ellipsis,
                      style: roundedTextStyle(
                        size: 11,
                        weight: FontWeight.w500,
                        color: Palette.mutedInk,
                        fontFamily: 'monospace',
                      ),
                    ),
                  ],
                ),
              ),
            ],
          ),
          const SizedBox(height: 10),
          SingleChildScrollView(
            scrollDirection: Axis.horizontal,
            child: Row(
              children: <Widget>[
                CapsuleTag(title: '类型', value: artifact.kindLabel),
                const SizedBox(width: 8),
                CapsuleTag(title: '大小', value: artifact.sizeDisplay),
              ],
            ),
          ),
          const SizedBox(height: 12),
          ArtifactOpenButton(artifact: artifact),
        ],
      ),
    );
  }
}

class ArtifactOpenButton extends StatelessWidget {
  const ArtifactOpenButton({
    super.key,
    required this.artifact,
    this.compact = false,
  });

  final ArtifactRef artifact;
  final bool compact;

  @override
  Widget build(BuildContext context) {
    return ActionButton(
      title: artifact.prefersPreview ? '打开/预览' : '下载/打开',
      background: Palette.shell,
      foreground: Palette.ink,
      borderColor: Palette.line,
      icon: artifact.prefersPreview ? Icons.open_in_new : Icons.download_rounded,
      fontSize: compact ? 12 : 14,
      padding: EdgeInsets.symmetric(vertical: compact ? 8 : 10),
      onPressed: () {
        unawaited(openArtifact(context, artifact));
      },
    );
  }
}

Future<void> openArtifact(BuildContext context, ArtifactRef artifact) async {
  final model = context.read<AppModel>();
  final rawUrl =
      artifact.prefersPreview ? artifact.previewUrl : artifact.downloadUrl;
  bool opened = false;
  try {
    opened = await _openUrlChannel.invokeMethod<bool>(
          'openUrl',
          <String, String>{'url': model.resolveAgentUrl(rawUrl)},
        ) ??
        false;
  } catch (_) {
    opened = false;
  }
  if (!opened && context.mounted) {
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(content: Text('无法打开 ${artifact.name}')),
    );
  }
}

IconData _kindIcon(String kind) {
  switch (kind) {
    case 'pdf':
      return Icons.picture_as_pdf_rounded;
    case 'image':
      return Icons.image_rounded;
    case 'apk':
      return Icons.android_rounded;
    case 'archive':
      return Icons.archive_rounded;
    default:
      return Icons.insert_drive_file_rounded;
  }
}

Color _kindTone(String kind) {
  switch (kind) {
    case 'pdf':
      return Palette.danger;
    case 'image':
      return Palette.accent2;
    case 'apk':
      return Palette.success;
    case 'archive':
      return Palette.warning;
    default:
      return Palette.softBlue;
  }
}
