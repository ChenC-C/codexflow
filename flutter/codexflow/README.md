# CodexFlow Flutter

这是基于现有 Agent API 直接重写的一版 Flutter 客户端。

## 目录

- `lib/` Flutter UI、状态和 API 封装
- `pubspec.yaml` 依赖声明

## 启动建议

当前这台机器没有可用的 `flutter` 命令，所以这里先把 Flutter 代码和工程清单完整落好了。

如果你本机后续装好 Flutter SDK，建议在这个目录执行：

```bash
flutter create .
flutter pub get
flutter run
```

这样可以补齐平台 runner，然后直接跑现有代码。
