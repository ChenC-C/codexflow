import 'package:flutter/material.dart';

class Palette {
  static const canvas = Color.fromRGBO(247, 247, 245, 1);
  static const shell = Color.fromRGBO(240, 240, 235, 1);
  static const ink = Color.fromRGBO(28, 33, 38, 1);
  static const mutedInk = Color.fromRGBO(99, 107, 112, 1);
  static const accent = Color.fromRGBO(38, 148, 89, 1);
  static const accent2 = Color.fromRGBO(224, 112, 56, 1);
  static const softBlue = Color.fromRGBO(56, 115, 181, 1);
  static const success = Color.fromRGBO(31, 145, 87, 1);
  static const warning = Color.fromRGBO(219, 122, 43, 1);
  static const danger = Color.fromRGBO(186, 56, 56, 1);
  static const panelStrong = Color.fromRGBO(255, 255, 255, 0.96);
  static const line = Color.fromRGBO(0, 0, 0, 0.07);

  static const terminalBackground = Color.fromRGBO(26, 33, 38, 1);
  static const terminalText = Color.fromRGBO(219, 227, 230, 1);
  static const terminalMuted = Color.fromRGBO(158, 173, 179, 1);
  static const codeBackground = Color.fromRGBO(31, 36, 41, 1);
  static const codeText = Color.fromRGBO(230, 235, 237, 1);

  static const dashboardGradient = LinearGradient(
    colors: [
      canvas,
      Color.fromRGBO(242, 245, 240, 1),
      shell,
    ],
    begin: Alignment.topLeft,
    end: Alignment.bottomRight,
  );
}
