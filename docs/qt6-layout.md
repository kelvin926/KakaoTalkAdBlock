# Qt6 advertisement layout

## Behavior

The Qt6 client can host its main banner in an owned top-level `KakaoTalkUI`
window. Such a window is not returned by `EnumChildWindows(mainWindow)`.
Advertisement discovery includes those owned windows and accepts the observed
`AdFit NAS Advertisement` title as well as `MOMENT 광고`. It still verifies the
specific Qt and Chromium ad-container structure before hiding a window.

Hiding the native ad window alone leaves a footer. Cropping the main window
with `SetWindowRgn` removes drawing space but leaves a transparent strip in
Alt+Tab previews. This fork preserves the complete native frame and region.

The optional layout adapter finds `MainWindowContent`'s `QQuickColumnLayout`
containing a `StackView`, `HorizontalSeperator`, and a `QQuickLoader` containing
`AdBanner`. It sets only the loader and separator invisible, on the GUI thread.
Qt then expands the StackView, chat list and lower sidebar controls naturally.
It does not inspect message text or account models, patch KakaoTalk files, or
change hosts, firewall rules, or system proxy settings.

## Prepare a portable runtime

Use Windows x64 Node.js 22 or newer and npm. From the repository root:

```powershell
npm --prefix runtime ci
node scripts/package-qt-runtime.mjs
```

The Frida dependency is pinned in `runtime/package-lock.json`; the project's
`allowScripts` entry permits its required native binding installation. The packaging script
copies the current Node executable and checks that the native Frida binding is
present. It does not download or upgrade Node.js.

Build the Go application as described in [CONTRIBUTING.md](../CONTRIBUTING.md),
or directly with the existing icon resource already generated:

```powershell
go build -o KakaoTalkAdBlock_amd64.exe -ldflags "-H windowsgui -s -w" ./cmd/main.go
```

Keep `runtime/` beside the executable when distributing the Qt6 layout build:

```text
KakaoTalkAdBlock_amd64.exe
runtime/
  node.exe
  node_modules/
    frida/
    ...
```

Exclude the generated `runtime/state/` directory from distribution. Quit the
old blocker through its tray menu before upgrading. If an older blocker was
force-killed while clipping the window, restarting KakaoTalk restores its frame.

The Go program embeds the adapter scripts, starts one hidden helper, and owns
its stdin pipe. On shutdown or parent exit the helper restores the visibility
values it changed, unloads instrumentation and detaches. The helper watches for
KakaoTalk process recreation and the adapter discovers current layout objects
afresh rather than keeping stale QObject pointers. `runtime/state/layout-status.json`
records readiness and geometry without message content.

## Existing runtime overrides

The default runtime directory is `runtime/` beside the executable. Node on
`PATH` can be used if `runtime/node.exe` is absent. Existing installations can
be selected for this process using these optional environment variables:

- `KAKAOTALK_ADBLOCK_RUNTIME_DIR`: directory containing `node.exe` and `node_modules`.
- `KAKAOTALK_ADBLOCK_NODE`: explicit Node executable.
- `KAKAOTALK_ADBLOCK_FRIDA_MODULE`: explicit `frida/build/src/frida.js` file.
- `KAKAOTALK_ADBLOCK_STATE_DIR`: writable adapter/status directory.

No personal account path, Codex installation, or REA installation is required.
If the optional runtime is absent, native ad-window hiding continues and the
footer remains until the layout runtime is configured.

## Verified scope

Live verification used Windows x64, KakaoTalk `26.9.0.420`, Qt `6.10.3`, Node
`24.19.0` and Frida `17.23.2`. The chat area initially grew from 966 to 1143
pixels when the 175-pixel banner loader and 2-pixel separator were excluded.
At a later window size, root and content heights both measured 1086 pixels.
A Qt window capture confirmed the chat list and lower sidebar icons filled
the available height. The native main-window region was unclipped, and the
KakaoTalk process and on-disk executable stayed unchanged. Restarting the
blocker closed the prior helper and reapplied the layout through a fresh helper.

These live results establish the layout mechanism used by the local prototype.
Repository checks additionally cover portable runtime discovery, the existing
resource regressions, JavaScript syntax and Windows build targets. They do not
establish support for other Qt versions or ARM64/x86 Qt clients.

The adapter validates x64, Qt 6.10.3, the installed child-list getter and the
specific component hierarchy. Unsupported layouts are left untouched; there
is no fallback to cropping. The helper adds a Node process and Frida agent, so
the discovery-only CPU numbers in [resource-fix.md](resource-fix.md) do not
describe total resource usage with this optional adapter enabled.
