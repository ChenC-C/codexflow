# CodexFlow Flutter

这是基于现有 Agent API 直接重写的一版 Flutter 客户端。

## 目录

- `lib/` Flutter UI、状态和 API 封装
- `pubspec.yaml` 依赖声明

## 启动建议

这个目录已经包含 Flutter runner、Android Gradle wrapper 和应用源码。

本机装好 Flutter SDK、JDK 17 和 Android SDK 后，在这个目录执行：

```bash
flutter pub get
flutter run
```

构建 Android APK：

```bash
flutter build apk --release --target-platform android-arm64
```

输出位置：

```text
build/app/outputs/flutter-apk/app-release.apk
```

如果要发布给别人安装，建议在 `android/app/build.gradle.kts` 中改成自己的 `applicationId`，并配置自己的 release keystore；当前工程为了便于本地测试，release 构建会使用 debug signing config。
