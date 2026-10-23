# KakaoTalkAdBlock

AdBlocker for KakaoTalk Windows client.

## At a glance

![](https://raw.githubusercontent.com/blurfx/KakaoTalkAdBlock/main/kakaotalk.png)

This program runs in the background.

Ad removal responds to window creation, display and geometry changes, with
a 30-second safety scan instead of continuously scanning every 100 ms.
Classic KakaoTalk starts no Node/Frida helper. See the implementation and
measurement limits in [resource notes](docs/resource-fix.md).

Right-click the tray icon to quit, or to check for new versions.

Qt6 clients can display the main banner in a separate owned window. This fork
recognizes both `MOMENT 광고` and `AdFit NAS Advertisement`, and preserves the
main window's native frame so Alt+Tab previews are not transparently cropped.

To remove the reserved footer too, so the chat list and lower sidebar icons
extend to the bottom, prepare the optional Windows x64 Node/Frida runtime in
[Qt6 layout setup](docs/qt6-layout.md). The layout adapter is verified with
KakaoTalk `26.9.0.420` and Qt `6.10.3`; legacy native window hiding remains
available without this runtime.

*Earlier versions of 2.2.0 can be quit by double-clicking the tray icon.*

![](https://raw.githubusercontent.com/blurfx/KakaoTalkAdBlock/main/tray.png)
