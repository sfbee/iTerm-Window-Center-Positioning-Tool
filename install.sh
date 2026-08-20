#!/bin/bash
# Build itermcenter and set it up either as a background LaunchAgent
# (auto-centers every new iTerm2 window, starts at login) or as a one-shot
# command you bind to a keyboard shortcut yourself (centers the frontmost
# iTerm2 window on demand) — or both.
#
# Run this from Terminal.app on your Mac (not from a remote shell) since it
# needs to trigger real macOS permission prompts.
set -euo pipefail

cd "$(dirname "$0")"

PLIST_NAME="com.stephen.itermcenter.plist"
PLIST_DEST="$HOME/Library/LaunchAgents/$PLIST_NAME"
BIN_PATH="$(pwd)/itermcenter"

usage() {
	echo "usage: $0 [--service|--shortcut|--both]"
	echo "  omit the flag to be prompted interactively"
}

MODE=""
case "${1:-}" in
	--service) MODE="service" ;;
	--shortcut) MODE="shortcut" ;;
	--both) MODE="both" ;;
	-h|--help) usage; exit 0 ;;
	"") ;;
	*) usage; exit 1 ;;
esac

if [[ -z "$MODE" ]]; then
	if [[ -t 0 ]]; then
		echo "How should itermcenter run?"
		echo "  1) Background service — auto-centers every new iTerm2 window, starts at login"
		echo "  2) Keyboard shortcut  — no background process; you press a hotkey to center the current window"
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
		echo "No terminal attached to prompt for a mode; defaulting to --service."
		echo "(Re-run with --service, --shortcut, or --both to choose explicitly.)"
		MODE="service"
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
	echo "First time only: macOS will ask you to allow itermcenter to control"
	echo "'iTerm2'. Approve it. If you miss the prompt, grant it in:"
	echo "  System Settings > Privacy & Security > Automation"
	echo ""
	echo "Test it: open a new iTerm2 window (Cmd+N) — it should snap to center"
	echo "within about a quarter second. Watch it work with:"
	echo "  tail -f $(pwd)/itermcenter.log"
}

print_shortcut_instructions() {
	echo "==> Keyboard shortcut setup (manual, ~2 minutes, one time)"
	echo ""
	echo "itermcenter is built at:"
	echo "  $BIN_PATH"
	echo ""
	echo "The command that centers the frontmost iTerm2 window is:"
	echo "  $BIN_PATH center"
	echo ""
	echo "If you already use a hotkey tool (Raycast, BetterTouchTool, Keyboard"
	echo "Maestro, Alfred, etc.) just bind that command to a shortcut there and"
	echo "you're done."
	echo ""
	echo "Otherwise, using the built-in Shortcuts app:"
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
	echo "The first time you run it, macOS will ask permission for Shortcuts"
	echo "(or the app hosting it) to control 'iTerm2' — approve it, or"
	echo "grant it later in System Settings > Privacy & Security > Automation."
}

case "$MODE" in
	service)
		install_service
		;;
	shortcut)
		print_shortcut_instructions
		;;
	both)
		install_service
		echo ""
		print_shortcut_instructions
		;;
esac
