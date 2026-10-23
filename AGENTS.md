# Project guidance

- Keep scoped snapshots, callback lifetime fixes and unchanged-geometry guards.
- Use coalesced window events for ad removal; retain a coarse health scan.
- Start Node/Frida only when a Qt6 process exists and reuse native PID discovery.
- Keep metadata bounded and skip unchanged diagnostics; avoid permanent GUI hooks.
- Preserve classic KakaoTalk ad hiding while improving the Qt6 path.
- Qt6 ads can be owned top-level windows; distinguish owners from child windows.
- Validate ad-specific Qt/Chromium structure before hiding a native window.
- Do not crop the main window or strip its frame to remove footer space.
- Remove the validated AdBanner loader and separator from the Qt layout instead.
- Run Qt reads and writes on the GUI thread. Do not inspect message/account data.
- Keep runtime discovery portable; do not commit user-specific paths or binaries.
- Keep Node/Frida optional for legacy native hiding and document its setup.
- Own helper processes through the parent stdin pipe and detach on shutdown.
- Keep runtime/paths.json local and ignored; environment overrides take precedence.
- Reuse winapi.ChildWindows rather than allocating new syscall callbacks.
- Use focused tests and builds. The 12-second resource probe is opt-in.
- Maintain this file under 200 lines and update state.json on material changes.
