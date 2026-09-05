// itermcenter watches for new iTerm2 windows and sizes and centers them on
// whichever screen they appear on. It works by polling iTerm2's own AppleScript/JXA
// scripting bridge (via a small osascript helper) for the current list of
// iTerm2 window IDs; any ID this process has never seen before is treated as
// "just created" and gets centered.
//
// Subcommands:
//
//	iterm-center-window watch [-interval 250ms] [-width .72] [-height .72] [-hotkey]
//	                                      run forever, size and center each new window
//	iterm-center-window center [-all] [-id N] [-width .72] [-height .72]
//	                                      size and center the current iTerm2 window,
//	                                      or every iTerm2 window with -all
//	iterm-center-window list             print current iTerm2 windows as JSON
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type winInfo struct {
	ID     int     `json:"id"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	W      float64 `json:"w"`
	H      float64 `json:"h"`
	Hotkey bool    `json:"hotkey"`
}

// listResult carries the window list plus a count of windows that were present
// in iTerm2's collection but could not be read. Callers need to know a read was
// partial: silently dropping an unreadable window used to make it look closed,
// and then "new" again on the next poll.
type listResult struct {
	Windows []winInfo `json:"windows"`
	Failed  int       `json:"failed"`
}

// Windows are addressed through iTerm2's own AppleScript/JXA dictionary
// rather than System Events. iTerm2 draws its own window chrome and doesn't
// publish an AXWindowNumber to the accessibility API, so
// "System Events" process windows[i].id() throws for every iTerm2 window
// (confirmed by inspecting the window's AX attribute list: no
// AXWindowNumber is present at all). Going straight to
// Application("iTerm2") sidesteps that entirely: it gives a real, stable
// window id() and a settable bounds() property, and needs no Accessibility
// permission — only the standard one-time Automation prompt for
// iterm-center-window to control iTerm2.
const listScript = `
function run() {
  var it = Application("iTerm2");
  var wins = it.windows;
  var n = wins.length;
  var out = [];
  var failed = 0;
  for (var i = 0; i < n; i++) {
    try {
      var w = wins[i];
      var id = w.id();
      var b = w.bounds();
      var hk = false;
      try { hk = w.isHotkeyWindow(); } catch (e) { hk = false; }
      out.push({ id: id, x: b.x, y: b.y, w: b.width, h: b.height, hotkey: hk });
    } catch (e) {
      failed++;
    }
  }
  return JSON.stringify({ windows: out, failed: failed });
}
`

const currentWindowIDScript = `
function run() {
  var it = Application("iTerm2");
  var cw = it.currentWindow();
  if (!cw) return "";
  try { return String(cw.id()); } catch (e) { return ""; }
}
`

const centerScript = `
function run(argv) {
  ObjC.import("AppKit");
  var targetId = parseInt(argv[0], 10);
  var widthRatio = parseFloat(argv[1]);
  var heightRatio = parseFloat(argv[2]);

  var it = Application("iTerm2");
  var target = it.windows.byId(targetId);
  var b;
  try { b = target.bounds(); } catch (e) { return "not-found"; }

  var winW = b.width;
  var winH = b.height;

  var screens = $.NSScreen.screens;
  var primaryFrame = screens.objectAtIndex(0).frame;
  var primaryHeight = primaryFrame.size.height;

  // NSScreen frames are bottom-left origin, y-up. iTerm2 window bounds are
  // top-left origin, y-down (Quartz space). Convert each screen's frame
  // into that same space before comparing.
  function toAXRect(f) {
    var axY = primaryHeight - f.origin.y - f.size.height;
    return { x: f.origin.x, y: axY, w: f.size.width, h: f.size.height };
  }

  var winCenterX = b.x + winW / 2;
  var winCenterY = b.y + winH / 2;

  var chosenScreen = null;
  var count = screens.count;
  for (var s = 0; s < count; s++) {
    var r = toAXRect(screens.objectAtIndex(s).frame);
    if (winCenterX >= r.x && winCenterX <= r.x + r.w && winCenterY >= r.y && winCenterY <= r.y + r.h) {
      chosenScreen = screens.objectAtIndex(s);
      break;
    }
  }
  if (!chosenScreen) chosenScreen = screens.objectAtIndex(0);

  // Use visibleFrame so the resized window stays clear of the Dock and menu bar.
  var usable = toAXRect(chosenScreen.visibleFrame);
  var newW = Math.round(usable.w * widthRatio);
  var newH = Math.round(usable.h * heightRatio);
  var newX = Math.round(usable.x + (usable.w - newW) / 2);
  var newY = Math.round(usable.y + (usable.h - newH) / 2);

  target.bounds = { x: newX, y: newY, width: newW, height: newH };
  return "ok";
}
`

func runJXA(script string, args ...string) (string, error) {
	cmdArgs := append([]string{"-l", "JavaScript", "-e", script}, args...)
	cmd := exec.Command("osascript", cmdArgs...)
	var out, errOut strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	err := cmd.Run()
	if err != nil {
		msg := strings.TrimSpace(errOut.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("%s", msg)
	}
	return strings.TrimSpace(out.String()), nil
}

// errNotRunning reports whether the failure was just "iTerm2 isn't running".
// That is a completely normal state (nothing to center), not an error worth
// logging 4x a second, so watch() backs off instead of spamming.
func errNotRunning(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "-2700") ||
		strings.Contains(s, "-600") ||
		strings.Contains(s, "can't be found") ||
		strings.Contains(s, "isn't running") ||
		strings.Contains(s, "is not running")
}

func listResultOf() (listResult, error) {
	out, err := runJXA(listScript)
	if err != nil {
		return listResult{}, err
	}
	if out == "" {
		// An empty payload tells us nothing about which windows exist. Report
		// it as an error so callers hold on to the state they already have
		// rather than concluding every window closed.
		return listResult{}, fmt.Errorf("empty response from iTerm2")
	}
	var res listResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		return listResult{}, fmt.Errorf("parse windows: %w (raw=%q)", err, out)
	}
	return res, nil
}

func listWindows() ([]winInfo, error) {
	res, err := listResultOf()
	if err != nil {
		return nil, err
	}
	return res.Windows, nil
}

func centerWindowByID(id int, widthRatio, heightRatio float64) error {
	out, err := runJXA(centerScript, strconv.Itoa(id),
		strconv.FormatFloat(widthRatio, 'f', -1, 64),
		strconv.FormatFloat(heightRatio, 'f', -1, 64))
	if err != nil {
		return err
	}
	if out != "ok" {
		return fmt.Errorf("center result: %s", out)
	}
	return nil
}

func currentWindowID() (int, error) {
	out, err := runJXA(currentWindowIDScript)
	if err != nil {
		return 0, err
	}
	if out == "" {
		return 0, fmt.Errorf("no current iTerm2 window")
	}
	return strconv.Atoi(out)
}

func isPermissionError(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "not authorized") || strings.Contains(s, "-1743") || strings.Contains(s, "1002")
}

// throttle collapses a repeating error into one log line plus a periodic
// "still happening" summary, so a long outage costs a few lines instead of
// tens of thousands.
type throttle struct {
	lastSig  string
	count    int
	lastLog  time.Time
	interval time.Duration
}

func (t *throttle) report(sig string, format string, args ...any) {
	now := time.Now()
	if sig != t.lastSig {
		if t.count > 1 {
			log.Printf("(previous message repeated %d more times)", t.count-1)
		}
		t.lastSig = sig
		t.count = 1
		t.lastLog = now
		log.Printf(format, args...)
		return
	}
	t.count++
	if now.Sub(t.lastLog) >= t.interval {
		log.Printf("still: "+format+" (%d times in the last %s)",
			append(args, t.count, now.Sub(t.lastLog).Round(time.Second))...)
		t.count = 1
		t.lastLog = now
	}
}

func (t *throttle) clear() {
	if t.count > 1 {
		log.Printf("(previous message repeated %d more times)", t.count-1)
	}
	t.lastSig = ""
	t.count = 0
}

const idleInterval = 5 * time.Second

func watch(interval time.Duration, widthRatio, heightRatio float64, includeHotkey bool) {
	log.Printf("iterm-center-window: watching for new iTerm2 windows (poll every %s, size %.0f%%x%.0f%%)",
		interval, widthRatio*100, heightRatio*100)

	// seen holds every window ID this process has ever observed, and is never
	// pruned. The previous implementation kept only the last poll's IDs and
	// treated "absent last poll" as "brand new", so any transient hiccup in
	// iTerm2's scripting bridge re-centered every open window — one window was
	// re-centered 16 times in a single day. Window IDs are not reused, so
	// remembering them all is both correct and cheap (a few thousand ints a
	// year of heavy use).
	seen := map[int]bool{}
	first := true
	th := &throttle{interval: 5 * time.Minute}
	sleep := interval

	for {
		time.Sleep(sleep)
		sleep = interval

		res, err := listResultOf()
		if err != nil {
			switch {
			case errNotRunning(err):
				th.report("not-running", "iTerm2 isn't running; idling (polling every %s until it returns)", idleInterval)
				sleep = idleInterval
			case isPermissionError(err):
				th.report("perm", "PERMISSION NEEDED: grant Automation access for iterm-center-window to control "+
					"'iTerm2' (System Settings > Privacy & Security > Automation), then restart iterm-center-window.")
				sleep = idleInterval
			default:
				th.report("list:"+err.Error(), "list error: %v", err)
			}
			continue
		}
		th.clear()

		if res.Failed > 0 {
			// Not fatal, and no longer dangerous: an unreadable window just
			// isn't confirmed this round. Because seen is never pruned it
			// cannot come back as a false "new window".
			log.Printf("note: %d window(s) could not be read this poll", res.Failed)
		}

		for _, w := range res.Windows {
			if seen[w.ID] {
				continue
			}
			seen[w.ID] = true

			if first {
				continue // windows that already existed at startup are left alone
			}
			if w.Hotkey && !includeHotkey {
				log.Printf("skipping hotkey window id=%d (pass -hotkey to include)", w.ID)
				continue
			}
			log.Printf("new window id=%d, centering", w.ID)
			if err := centerWindowByID(w.ID, widthRatio, heightRatio); err != nil {
				log.Printf("center error for id=%d: %v", w.ID, err)
			}
		}

		if first {
			log.Printf("startup: ignoring %d existing window(s)", len(res.Windows))
			first = false
		}
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: iterm-center-window <watch|center|list> [flags]")
	os.Exit(2)
}

func validateRatio(name string, ratio float64) {
	if ratio <= 0 || ratio > 1 {
		log.Fatalf("-%s must be greater than 0 and no more than 1", name)
	}
}

func main() {
	log.SetFlags(log.LstdFlags)

	if len(os.Args) < 2 {
		usage()
	}

	switch os.Args[1] {
	case "watch":
		fs := flag.NewFlagSet("watch", flag.ExitOnError)
		interval := fs.Duration("interval", 250*time.Millisecond, "poll interval")
		widthRatio := fs.Float64("width", .72, "fraction of the display's usable width")
		heightRatio := fs.Float64("height", .72, "fraction of the display's usable height")
		hotkey := fs.Bool("hotkey", false, "also center iTerm2's hotkey (dropdown) window")
		fs.Parse(os.Args[2:])
		validateRatio("width", *widthRatio)
		validateRatio("height", *heightRatio)
		watch(*interval, *widthRatio, *heightRatio, *hotkey)

	case "center":
		fs := flag.NewFlagSet("center", flag.ExitOnError)
		all := fs.Bool("all", false, "center every iTerm2 window instead of just the active one")
		id := fs.Int("id", 0, "center the window with this specific id")
		widthRatio := fs.Float64("width", .72, "fraction of the display's usable width")
		heightRatio := fs.Float64("height", .72, "fraction of the display's usable height")
		fs.Parse(os.Args[2:])
		validateRatio("width", *widthRatio)
		validateRatio("height", *heightRatio)

		if *all {
			wins, err := listWindows()
			if err != nil {
				log.Fatalf("list error: %v", err)
			}
			if len(wins) == 0 {
				fmt.Println("no iTerm2 windows found")
				return
			}
			for _, w := range wins {
				if err := centerWindowByID(w.ID, *widthRatio, *heightRatio); err != nil {
					log.Printf("center error id=%d: %v", w.ID, err)
				} else {
					fmt.Printf("centered window id=%d\n", w.ID)
				}
			}
			return
		}

		target := *id
		if target == 0 {
			// No explicit id: act on whatever window is focused right now.
			// Hotkey windows are centered too — asking for it by hotkey is an
			// explicit request, unlike the automatic watch path.
			var err error
			target, err = currentWindowID()
			if err != nil {
				fmt.Println("no iTerm2 windows found")
				return
			}
		}
		if err := centerWindowByID(target, *widthRatio, *heightRatio); err != nil {
			log.Fatalf("center error id=%d: %v", target, err)
		}
		fmt.Printf("centered window id=%d\n", target)

	case "list":
		res, err := listResultOf()
		if err != nil {
			log.Fatalf("list error: %v", err)
		}
		b, _ := json.MarshalIndent(res.Windows, "", "  ")
		fmt.Println(string(b))
		if res.Failed > 0 {
			fmt.Fprintf(os.Stderr, "note: %d window(s) could not be read\n", res.Failed)
		}

	default:
		usage()
	}
}
