// itermcenter watches for new iTerm2 windows and sizes and centers them on
// whichever screen they appear on. It works by polling iTerm2's own AppleScript/JXA
// scripting bridge (via a small osascript helper) for the current list of
// iTerm2 window IDs; any ID that wasn't present on the previous poll is
// treated as "just created" and gets centered.
//
// Subcommands:
//
//	itermcenter watch [-interval 250ms] [-width .72] [-height .72]
//	                                      run forever, size and center each new window
//	itermcenter center [-all] [-width .72] [-height .72]
//	                                      size and center the current iTerm2 window,
//	                                      or every iTerm2 window with -all
//	itermcenter list                      print current iTerm2 windows as JSON
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
	ID int     `json:"id"`
	X  float64 `json:"x"`
	Y  float64 `json:"y"`
	W  float64 `json:"w"`
	H  float64 `json:"h"`
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
// itermcenter to control iTerm2.
const listScript = `
function run() {
  var it = Application("iTerm2");
  var wins = it.windows;
  var n = wins.length;
  var out = [];
  for (var i = 0; i < n; i++) {
    try {
      var w = wins[i];
      var id = w.id();
      var b = w.bounds();
      out.push({ id: id, x: b.x, y: b.y, w: b.width, h: b.height });
    } catch (e) {}
  }
  return JSON.stringify(out);
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

func listWindows() ([]winInfo, error) {
	out, err := runJXA(listScript)
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	var wins []winInfo
	if err := json.Unmarshal([]byte(out), &wins); err != nil {
		return nil, fmt.Errorf("parse windows: %w (raw=%q)", err, out)
	}
	return wins, nil
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

func watch(interval time.Duration, widthRatio, heightRatio float64) {
	log.Printf("itermcenter: watching for new iTerm2 windows (poll every %s)", interval)
	known := map[int]bool{}
	firstRun := true
	warnedPerm := false

	for {
		wins, err := listWindows()
		if err != nil {
			if isPermissionError(err) {
				if !warnedPerm {
					log.Printf("PERMISSION NEEDED: grant Automation access for itermcenter to control " +
						"'iTerm2' (System Settings > Privacy & Security > Automation), then quit and " +
						"relaunch itermcenter.")
					warnedPerm = true
				}
			} else {
				log.Printf("list error: %v", err)
			}
			time.Sleep(interval)
			continue
		}

		current := map[int]bool{}
		for _, w := range wins {
			current[w.ID] = true
			if !firstRun && !known[w.ID] {
				log.Printf("new window id=%d detected, centering", w.ID)
				if err := centerWindowByID(w.ID, widthRatio, heightRatio); err != nil {
					log.Printf("center error for id=%d: %v", w.ID, err)
				}
			}
		}
		known = current
		firstRun = false
		time.Sleep(interval)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: itermcenter <watch|center|list> [flags]")
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
		fs.Parse(os.Args[2:])
		validateRatio("width", *widthRatio)
		validateRatio("height", *heightRatio)
		watch(*interval, *widthRatio, *heightRatio)

	case "center":
		fs := flag.NewFlagSet("center", flag.ExitOnError)
		all := fs.Bool("all", false, "center every iTerm2 window instead of just the active one")
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

		id, err := currentWindowID()
		if err != nil {
			fmt.Println("no iTerm2 windows found")
			return
		}
		if err := centerWindowByID(id, *widthRatio, *heightRatio); err != nil {
			log.Fatalf("center error id=%d: %v", id, err)
		}
		fmt.Printf("centered window id=%d\n", id)

	case "list":
		wins, err := listWindows()
		if err != nil {
			log.Fatalf("list error: %v", err)
		}
		b, _ := json.MarshalIndent(wins, "", "  ")
		fmt.Println(string(b))

	default:
		usage()
	}
}
