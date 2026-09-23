# Resource lifetime and polling fixes

The original watcher overwrote Toolhelp snapshot handles without closing them.
When no KakaoTalk window was found it leaked another snapshot about every two
seconds. This can exhaust system commit without a large private working set.
The issue also appears in upstream PR https://github.com/blurfx/KakaoTalkAdBlock/pull/108.
Background CPU consumption is reported in https://github.com/blurfx/KakaoTalkAdBlock/issues/65.

## Changes

- Scope every process snapshot to a single enumeration and close it on every
  successful allocation path, including failed/empty enumeration.
- Discover processes and top-level windows every two seconds, including an
  immediate startup scan. Rebuild the window sets so closed/reused HWNDs cannot
  accumulate. Existing tracked windows still receive the 100 ms ad-removal poll;
  discovering newly created windows can take up to two seconds.
- Keep window PID lookup separate from the process enumeration record.
- Replace per-HWND callback closures and permanent title/class caches with a
  single reusable child-enumeration callback and per-pass observations. The
  collector releases its reference to each returned slice.
- Enumerate descendants once: EnumChildWindows already traverses the entire
  subtree. Check custom-scroll ancestry from that snapshot without re-enumeration.
- Skip invisible/dead windows and unchanged legacy ad-area sizes. Do not issue
  unconditional UpdateWindow calls or activate/reorder windows while resizing.
- Skip redundant beta ad hiding and zero-size SetWindowPos calls.

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
