#!/bin/bash
# Build itermcenter and set it up either as a background LaunchAgent
# (auto-centers every brand-new iTerm2 window, starts at login) or as a
# keyboard shortcut that centers the focused iTerm2 window on demand — or both.
#
# Run this from Terminal.app on your Mac (not from a remote shell) since it
# needs to trigger real macOS permission prompts.
set -euo pipefail

cd "$(dirname "$0")"

PLIST_NAME="com.stephen.itermcenter.plist"
PLIST_DEST="$HOME/Library/LaunchAgents/$PLIST_NAME"
BIN_PATH="$(pwd)/itermcenter"
SKHDRC="$HOME/.skhdrc"
HOTKEY_ONE="cmd + ctrl - c"
HOTKEY_ALL="cmd + shift + ctrl - c"
SKHD_FORMULA="koekeishiya/formulae/skhd"

usage() {
	echo "usage: $0 [--service|--shortcut|--both] [--yes] [--no-install-deps]"
	echo ""
	echo "  --service           background daemon only (centers brand-new windows)"
	echo "  --shortcut          keyboard shortcut only"
	echo "  --both              both"
	echo "  --yes, -y           don't prompt; accept defaults (implies installing"
	echo "                      skhd via Homebrew if it is missing)"
	echo "  --no-install-deps   never install skhd; print manual hotkey steps instead"
	echo ""
	echo "  omit the mode to be prompted interactively"
}

MODE=""
ASSUME_YES=0
INSTALL_DEPS=1

while [[ $# -gt 0 ]]; do
	case "$1" in
		--service) MODE="service" ;;
		--shortcut) MODE="shortcut" ;;
		--both) MODE="both" ;;
		--yes|-y) ASSUME_YES=1 ;;
		--no-install-deps) INSTALL_DEPS=0 ;;
		-h|--help) usage; exit 0 ;;
		*) echo "unknown option: $1" >&2; usage; exit 1 ;;
	esac
	shift
done

# confirm PROMPT — true if the user agrees. Non-interactive or --yes: true.
confirm() {
	if [[ "$ASSUME_YES" -eq 1 ]] || [[ ! -t 0 ]]; then
		return 0
	fi
	local reply
	read -r -p "$1 [Y/n]: " reply
	[[ -z "$reply" || "$reply" =~ ^[Yy] ]]
}

if [[ -z "$MODE" ]]; then
	if [[ -t 0 ]] && [[ "$ASSUME_YES" -eq 0 ]]; then
		echo "How should itermcenter run?"
		echo "  1) Background service — auto-centers every brand-new iTerm2 window, starts at login"
		echo "  2) Keyboard shortcut  — no background process; press a hotkey to center the current window"
		echo "  3) Both"
		while true; do
			read -r -p "Choose [1/2/3]: " choice
			case "$choice" in
				1) MODE="service"; break ;;
				2) MODE="shortcut"; break ;;
				3) MODE="both"; break ;;
				*) echo "Please enter 1, 2, or 3." ;;
			esac
		done
	else
		echo "No terminal attached to prompt for a mode; defaulting to --both."
		echo "(Re-run with --service, --shortcut, or --both to choose explicitly.)"
		MODE="both"
	fi
fi

echo "==> Building itermcenter"
go build -o itermcenter .

install_service() {
	echo "==> Installing LaunchAgent"
	mkdir -p "$HOME/Library/LaunchAgents"
	cat > "$PLIST_DEST" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>com.stephen.itermcenter</string>
	<key>ProgramArguments</key>
	<array>
		<string>$BIN_PATH</string>
		<string>watch</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<true/>
	<key>ProcessType</key>
	<string>Interactive</string>
	<key>StandardOutPath</key>
	<string>$(pwd)/itermcenter.log</string>
	<key>StandardErrorPath</key>
	<string>$(pwd)/itermcenter.err.log</string>
</dict>
</plist>
PLIST

	# Unload any previous copy, then load the fresh one.
	launchctl bootout "gui/$(id -u)" "$PLIST_DEST" 2>/dev/null || true
	launchctl bootstrap "gui/$(id -u)" "$PLIST_DEST"

	echo "==> Service installed and running. It will also start at login."
	echo "    Logs: $(pwd)/itermcenter.log and itermcenter.err.log"
	echo ""
	echo "It centers a window exactly once, the first time that window is seen."
	echo "New tabs, refocusing, minimizing and moving windows are all left alone."
	echo ""
	echo "First time only: macOS will ask you to allow itermcenter to control"
	echo "'iTerm2'. Approve it. If you miss the prompt, grant it in:"
	echo "  System Settings > Privacy & Security > Automation"
	echo ""
	echo "Test it: open a new iTerm2 window (Cmd+N) — it should snap to center"
	echo "within about a quarter second. Watch it work with:"
	echo "  tail -f $(pwd)/itermcenter.log"
}

# ensure_skhd makes skhd available, installing it via Homebrew when missing.
# Returns non-zero if skhd still isn't usable afterwards, so the caller can
# fall back to printing manual instructions.
ensure_skhd() {
	if command -v skhd >/dev/null 2>&1; then
		return 0
	fi

	if [[ "$INSTALL_DEPS" -eq 0 ]]; then
		echo "==> skhd is not installed and --no-install-deps was passed."
		return 1
	fi

	echo "==> skhd is not installed."
	echo "    skhd is a small open-source hotkey daemon; it is what actually"
	echo "    binds the key combo to the itermcenter command."
	echo "    https://github.com/koekeishiya/skhd"
	echo ""

	if ! command -v brew >/dev/null 2>&1; then
		echo "    Homebrew isn't installed, so this script can't install skhd."
		echo "    Install Homebrew from https://brew.sh and re-run, or install"
		echo "    skhd yourself:  brew install $SKHD_FORMULA"
		return 1
	fi

	if ! confirm "    Install skhd now with 'brew install $SKHD_FORMULA'?"; then
		echo "    Skipping skhd install."
		return 1
	fi

	echo "==> Installing skhd via Homebrew"
	if ! brew install "$SKHD_FORMULA"; then
		echo "    brew install failed; falling back to manual instructions." >&2
		return 1
	fi

	if ! command -v skhd >/dev/null 2>&1; then
		echo "    skhd still isn't on PATH after install." >&2
		return 1
	fi

	echo "==> Registering skhd to start at login"
	skhd --start-service || true
	return 0
}

install_shortcut_skhd() {
	echo "==> Installing keyboard shortcut via skhd"
	if grep -q "itermcenter" "$SKHDRC" 2>/dev/null; then
		echo "    $SKHDRC already has an itermcenter binding; leaving it alone."
	else
		# Keep any existing bindings; just append ours.
		[[ -f "$SKHDRC" ]] && printf '\n' >> "$SKHDRC"
		cat >> "$SKHDRC" <<SKHD
# Center the focused iTerm2 window on demand (any window, new or old).
# Change the key on the left to rebind; run \`skhd --reload\` after editing.
$HOTKEY_ONE : $BIN_PATH center

# Center every open iTerm2 window at once.
$HOTKEY_ALL : $BIN_PATH center --all
SKHD
		echo "    Wrote bindings to $SKHDRC"
	fi

	if pgrep -x skhd >/dev/null 2>&1; then
		skhd --reload
		echo "    Reloaded the running skhd."
	else
		echo "==> Starting skhd as a login service"
		skhd --start-service || {
			echo "    Couldn't start skhd automatically. Start it with:"
			echo "      skhd --start-service"
		}
	fi

	echo ""
	echo "    $HOTKEY_ONE        center the focused iTerm2 window"
	echo "    $HOTKEY_ALL  center every iTerm2 window"
	echo ""
	echo "    skhd needs Accessibility permission to capture hotkeys. If the keys"
	echo "    do nothing, enable skhd in System Settings > Privacy & Security >"
	echo "    Accessibility, then run: skhd --restart-service"
}

install_shortcut_manual() {
	echo "==> Keyboard shortcut setup (manual, ~2 minutes, one time)"
	echo ""
	echo "Bind this command with whatever hotkey tool you have:"
	echo ""
	echo "  $BIN_PATH center"
	echo ""
	echo "Raycast, BetterTouchTool, Keyboard Maestro and Alfred can all bind a"
	echo "shell command to a hotkey directly."
	echo ""
	echo "Or with the built-in Shortcuts app:"
	echo "  1. Open Shortcuts.app"
	echo "  2. Click + to create a new shortcut"
	echo "  3. Search for the 'Run Shell Script' action and drag it in"
	echo "  4. Set Shell to /bin/bash, Input to 'as arguments', and paste:"
	echo "       $BIN_PATH center"
	echo "  5. Click the shortcut's name at the top, open the info/details"
	echo "     panel (the (i) icon), and click 'Add Keyboard Shortcut' to"
	echo "     record the hotkey you want (e.g. Cmd+Ctrl+C)"
	echo "  6. Name the shortcut (e.g. 'Center iTerm2 Window') and close"
	echo ""
	echo "The first time you run it, macOS will ask permission for the hosting"
	echo "app to control 'iTerm2' — approve it, or grant it later in"
	echo "System Settings > Privacy & Security > Automation."
}

install_shortcut() {
	if ensure_skhd; then
		install_shortcut_skhd
	else
		echo ""
		install_shortcut_manual
	fi
}

case "$MODE" in
	service)
		install_service
		;;
	shortcut)
		install_shortcut
		;;
	both)
		install_service
		echo ""
		install_shortcut
		;;
esac
