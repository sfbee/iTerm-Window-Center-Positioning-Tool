# itermcenter

Sizes and centers iTerm2 windows. By default a window occupies 72% of the
usable width and height of the display it appears on.

- **Background service** — a daemon polls iTerm2's own scripting bridge
  every 250ms for the list of window IDs; any ID that wasn't there on the
  previous poll is treated as newly created and gets resized and centered on
  whichever screen it's on (multi-monitor aware). Existing windows are left alone.
- **Keyboard shortcut** — no background process; you bind
  `itermcenter center` to a hotkey yourself and it centers the active
  (frontmost) iTerm2 window on demand. Add `--all` to center every window
  instead.

You can install one or both.

## Install

```
cd ~/REPOS/itermcenter
chmod +x install.sh
./install.sh
```

Run this in Terminal.app itself (not over SSH/remote), because the first
run needs to trigger a real macOS permission prompt. It'll ask which mode
you want:

```
1) Background service — auto-centers every new iTerm2 window, starts at login
2) Keyboard shortcut  — no background process; you press a hotkey to center the current window
3) Both
```

Pass `--service`, `--shortcut`, or `--both` instead of answering the prompt
if you want to script it non-interactively.

**Permission required:** macOS will ask you to let itermcenter (or
Shortcuts, if you go the keyboard-shortcut route) control `iTerm2` (this is
what lets it read/move windows). Approve it. If you miss the prompt, grant
it manually in **System Settings → Privacy & Security → Automation**.

The keyboard-shortcut option prints step-by-step instructions for wiring
`itermcenter center` up in the Shortcuts app (or whatever hotkey tool you
already use) — see the script's output, or run `./install.sh --shortcut`
any time to see them again.

## Try it

Open a new iTerm2 window with Cmd+N — it should resize and center within ~250ms.
Watch what it's doing:

```
tail -f ~/REPOS/itermcenter/itermcenter.log
```

## Manual commands (no daemon needed)

```
./itermcenter list         # print current iTerm2 windows as JSON
./itermcenter center       # center the active (frontmost) iTerm2 window right now
./itermcenter center --all # center every iTerm2 window right now
```

Change the proportions with `--width` and `--height` (values greater than zero
and no more than one):

```
./itermcenter center --width .8 --height .75
./itermcenter watch --width .8 --height .75
```

## Uninstall / stop

```
launchctl bootout gui/$(id -u) ~/Library/LaunchAgents/com.stephen.itermcenter.plist
rm ~/Library/LaunchAgents/com.stephen.itermcenter.plist
```

## Known limitations

- Windows opened via iTerm2's special "Hotkey Window" (the dropdown/
  Quake-style overlay) may or may not enumerate the same way as regular
  windows — regular Cmd+N / Profiles-menu windows definitely should.
- Sizing uses the display's usable area, excluding the menu bar and Dock.
- Poll interval defaults to 250ms (`itermcenter watch -interval 250ms`);
  lower it if you want centering to feel more instant, at the cost of a
  little more CPU/battery from the constant osascript calls.
