# itermcenter

Sizes and centers iTerm2 windows. 

By default, it will automatically adjust any new iTerm windows it detects when it's first created, adjusting the window to 72% of the
usable width and height of the display it appears on.

I created this as an easy alternative to managing it through a script triggered by a profile, and it gives me a consistent terminal no matter the display configuration.

It operates via:

- **Background service** - This runs as a background service, watching and adjusting any new iTerm terminal windows it detects.
  
- **Keyboard shortcut** — no background process; a hotkey centers whichever
  iTerm2 window is focused right now, new or old. `--all` does every window.

You can install one or both.

## Install

```
chmod +x install.sh
./install.sh
```

Run this in Terminal.app itself (not over SSH/remote), because the first
run needs to trigger a real macOS permission prompt. It'll ask which mode
you want:

```
1) Background service — auto-centers every brand-new iTerm2 window, starts at login
2) Keyboard shortcut  — no background process; press a hotkey to center the current window
3) Both
```

Pass `--service`, `--shortcut`, or `--both` instead of answering the prompt
if you want to script it non-interactively:

| Flag | Effect |
| --- | --- |
| `--service` | background daemon only |
| `--shortcut` | keyboard shortcut only |
| `--both` | both |
| `--yes`, `-y` | don't prompt, accept defaults |
| `--no-install-deps` | never install skhd; print manual hotkey steps instead |

**Permission required:** macOS will ask you to let itermcenter control
`iTerm2` (this is what lets it read/move windows). Approve it. If you miss
the prompt, grant it manually in **System Settings → Privacy & Security →
Automation**.

## Keyboard shortcut

`./install.sh --shortcut` wires the hotkeys up for you by appending to
`~/.skhdrc`. It uses [skhd](https://github.com/koekeishiya/skhd), a small
open-source hotkey daemon, and **installs it via Homebrew if it's missing**
(asking first, unless you pass `--yes`) then registers it to start at login:

| Shortcut | Action |
| --- | --- |
| `Cmd+Ctrl+C` | Center the focused iTerm2 window |
| `Cmd+Shift+Ctrl+C` | Center every iTerm2 window |

Rebind by editing the key on the left in `~/.skhdrc`, then `skhd --reload`.

skhd needs **Accessibility** permission to capture hotkeys — if the keys do
nothing, enable skhd in **System Settings → Privacy & Security →
Accessibility** and run `skhd --restart-service`.

If Homebrew isn't available, you decline the install, or you pass
`--no-install-deps`, the installer instead prints instructions for binding
`itermcenter center` in Raycast, BetterTouchTool, Keyboard Maestro, Alfred or
the built-in Shortcuts app. Re-running is safe: an existing `itermcenter`
binding in `~/.skhdrc` is left alone.

## Try it

Open a new iTerm2 window with Cmd+N — it should resize and center within ~250ms.
Opening a new *tab* should do nothing. Watch what it's doing:

```
tail -f ~/personal-repos/itermcenter/itermcenter.log
```

## Manual commands (no daemon needed)

```
./itermcenter list          # print current iTerm2 windows as JSON
./itermcenter center        # center the focused iTerm2 window right now
./itermcenter center --all  # center every iTerm2 window right now
./itermcenter center --id N # center one specific window by id
```

Change the proportions with `--width` and `--height` (values greater than zero
and no more than one):

```
./itermcenter center --width .8 --height .75
./itermcenter watch --width .8 --height .75
```

`watch` also takes `-hotkey` to include iTerm2's hotkey (dropdown) window,
which is skipped by default so centering doesn't fight iTerm2's own geometry
for it.

## Uninstall / stop

Stop and remove the background service:

```
launchctl bootout gui/$(id -u) ~/Library/LaunchAgents/com.stephen.itermcenter.plist
rm ~/Library/LaunchAgents/com.stephen.itermcenter.plist
```

Remove the keyboard shortcuts by deleting the `itermcenter` lines from
`~/.skhdrc`, then `skhd --reload`.

Everything else lives in this directory, so `rm -rf` on it finishes the job.
Optionally revoke itermcenter's entry in **System Settings → Privacy &
Security → Automation**.

## Notes

- **Why "first time seen" and not "appeared since the last poll":** iTerm2's
  scripting bridge intermittently fails to report a window that is definitely
  open. Diffing against only the previous poll treated that hiccup as the
  window closing and reopening, so it got re-centered — one window was
  re-centered 16 times in a single day, which looked like centering on tab
  creation and on focus changes. The daemon now remembers every window ID it
  has ever seen and never prunes that set: IDs aren't reused, so a window can
  only ever be centered once.
- Sizing uses the display's usable area, excluding the menu bar and Dock.
- Poll interval defaults to 250ms (`itermcenter watch -interval 250ms`);
  lower it if you want centering to feel more instant, at the cost of a
  little more CPU/battery from the constant osascript calls.
- When iTerm2 isn't running the daemon backs off to one poll every 5s and logs
  it once, rather than logging an error four times a second.
