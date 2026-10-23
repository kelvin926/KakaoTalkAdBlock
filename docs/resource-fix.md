# Resource lifetime and polling fixes

The original watcher overwrote Toolhelp snapshot handles without closing them.
When no KakaoTalk window was found it leaked another snapshot about every two
seconds. This can exhaust system commit without a large private working set.
The issue also appears in upstream PR https://github.com/blurfx/KakaoTalkAdBlock/pull/108.
Background CPU consumption is reported in https://github.com/blurfx/KakaoTalkAdBlock/issues/65.

## Changes

- Scope every process snapshot to a single enumeration and close it on every
  successful allocation path, including failed/empty enumeration.
- Discover processes and top-level windows at startup and on relevant window
  creation, display and name changes. A 30-second health scan repairs missed
  events; if WinEvent registration fails, discovery falls back to two seconds.
  Rebuild window sets so closed/reused HWNDs cannot accumulate.
- Coalesce ad-removal work for 50 ms after window activity rather than polling
  every 100 ms. Ignore focus, selection, and accessibility-content events.
  A dedicated message-loop thread owns and releases the out-of-context WinEvent
  hook as required by [SetWinEventHook](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-setwineventhook).
- Keep window PID lookup separate from the process enumeration record.
- Replace per-HWND callback closures and permanent title/class caches with a
  single reusable child-enumeration callback and per-pass observations. The
  collector releases its reference to each returned slice.
- Enumerate descendants once: EnumChildWindows already traverses the entire
  subtree. Check custom-scroll ancestry from that snapshot without re-enumeration.
- Skip invisible/dead windows and unchanged legacy ad-area sizes. Do not issue
  unconditional UpdateWindow calls or activate/reorder windows while resizing.
- Skip redundant beta ad hiding and zero-size SetWindowPos calls.
- Start the Qt Node helper only for discovered Qt6 PIDs, stop it when none remain,
  and pass targets over stdin instead of performing duplicate process discovery.
  Use window notifications, bounded startup retries and the coarse health scan
  for layout checks. Bound layout caches and write status only when it changes.

## Verification (Windows amd64, Go 1.27.1, 2026-09-23)

`go test -v ./...` covers 64 real snapshot allocations (178 handles before and
after), nested scroll/ad classification and reused HWND metadata, cancellation,
100 repeated enumerations of test-owned hidden windows plus window destruction
and recreation, and 100 unchanged resize polls without another
WM_WINDOWPOSCHANGING message. Changed dimensions still trigger resizing.

An opt-in read-only discovery probe compared upstream b36cad8 with this change
on the same host, using GOMAXPROCS=2, a one-second warmup and a 12-second sample:

| Discovery worker only | Upstream | Fixed |
|---|---:|---:|
| Process CPU time | 390.625 ms | 78.125 ms |
| Percent of one CPU core | 3.2551% | 0.6510% |

This short sample measures an 80% CPU-time reduction in discovery, not overall
application CPU or a long-duration leak soak. Runtime handle initialization can
affect short probe counts; the focused 64-snapshot test isolates handle lifetime.
The probe does not run ad removal or modify other applications' windows.

PowerShell command for the optional probe:

```powershell
$env:KAKAOTALK_RESOURCE_PROBE = '1'
go test ./internal -run TestWatchResourceProbe -v -count=1
```

The historic 125 GiB observation is consistent with leaked snapshots but is not
a reproduced byte-for-byte attribution to the original installed executable.
Actual KakaoTalk visual behavior and multi-day stability still require a live
usage check. Tests use hidden windows owned by the test process only.

## Event-driven idle check (Windows amd64, Go 1.27.3, 2026-10-23)

A local installation still used the older upstream-based Qt layout build and
started its Node helper even with classic KakaoTalk. This update also brings
the existing fork lifetime/geometry fixes into that installed executable.
With classic KakaoTalk `26.9.0.5337` open:

| Blocker plus optional helper | Before | After |
|---|---:|---:|
| Idle CPU, percent of one core | 5.5003% | 0.0520% |
| Private committed memory | 163.83 MiB | 49.00 MiB |
| Node helper processes | 1 | 0 |

The baseline sample was 15.056 seconds and the final idle sample 30.065 seconds.
CPU percentages normalize process CPU time to elapsed time on one core. They
are not whole-machine CPU percentages. Memory includes the old helper before
optimization and only the Go blocker after optimization. Final 10/20/30-second
samples held private memory at 49.00 MiB and handles at 395
(after 396 at the initial checkpoint). This is a short check, not a multi-day
leak test or a measured battery-life gain.

A real classic main view was briefly reduced by 10 pixels. Window events
restored the original content height within 77 ms, with the main window
rectangle, style, region and KakaoTalk process preserved. No KakaoTalk files,
user data, startup path or system power settings changed.

Focused checks cover real WinEvent delivery and hook cancellation, snapshot
lifetime, legacy window resizing, lazy helper startup, runtime sidecar discovery,
Windows build targets and JavaScript syntax. `node scripts/test-qt-helper.mjs`
uses a fake device to check idle scheduling, target replacement and EOF cleanup;
it does not attach to any user application. The resource-test host no longer
has the Qt6 client, so new Qt scheduling is not claimed as live-verified.
