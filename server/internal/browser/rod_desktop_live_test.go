package browser

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/go-rod/rod/lib/proto"
)

// TestPlan_g2_liveContentCoversXvfb checks the headed window on a real Xvfb:
// the window sits at 0,0 with the tab strip and address bar on screen, the
// content area fills the rest of 1920x1080 (page top bar right below the
// toolbar, no black desktop at the bottom), fullscreen and leaving fullscreen
// keep that, a viewport point hits the element under it, and inspect turns
// on and off. Skipped unless PREVIEW_DESKTOP_LIVE=1.
func TestPlan_g2_liveContentCoversXvfb(t *testing.T) {
	if os.Getenv("PREVIEW_DESKTOP_LIVE") == "" {
		t.Skip("set PREVIEW_DESKTOP_LIVE=1 to run the Xvfb coverage check")
	}
	chrome := findChromeBinary()
	if chrome == "" {
		t.Skip("no Chromium binary")
	}
	if _, err := exec.LookPath("Xvfb"); err != nil {
		t.Skip("Xvfb not installed")
	}

	display := fmt.Sprintf(":%d", 80+(os.Getpid()%80))
	xvfb := exec.Command("Xvfb", display, "-screen", "0", "1920x1080x24")
	xvfb.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := xvfb.Start(); err != nil {
		t.Fatalf("Xvfb: %v", err)
	}
	t.Cleanup(func() { _ = syscall.Kill(-xvfb.Process.Pid, syscall.SIGKILL) })
	time.Sleep(400 * time.Millisecond)

	port := freePort(t)
	userDir := t.TempDir()
	logFile, err := os.Create(filepath.Join(t.TempDir(), "chrome.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer logFile.Close()

	chromeCmd := exec.Command(chrome,
		"--no-sandbox",
		"--disable-dev-shm-usage",
		"--disable-gpu",
		"--disable-infobars",
		"--ozone-platform=x11",
		"--remote-debugging-port="+strconv.Itoa(port),
		"--remote-debugging-address=127.0.0.1",
		"--window-size=1920,1080",
		"--window-position=0,0",
		"--force-device-scale-factor=1",
		"--no-first-run",
		"--no-default-browser-check",
		"--user-data-dir="+userDir,
		"about:blank",
	)
	chromeCmd.Env = append(os.Environ(), "DISPLAY="+display)
	chromeCmd.Stdout = logFile
	chromeCmd.Stderr = logFile
	chromeCmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := chromeCmd.Start(); err != nil {
		t.Fatalf("chrome: %v", err)
	}
	t.Cleanup(func() { _ = syscall.Kill(-chromeCmd.Process.Pid, syscall.SIGKILL) })

	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	waitCDP(t, base)
	engine, err := dialRod(t.Context(), base)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = engine.Close() })

	page, err := engine.OpenDesktop(t.Context(), "data:text/html,"+coverPageQuery())
	if err != nil {
		t.Fatalf("NewTab: %v", err)
	}
	rp := page.(*rodPage)
	time.Sleep(300 * time.Millisecond)

	grab := compileGrab(t)
	assertPickHitsButton(t, rp, "non-fullscreen")
	assertContentCovers(t, rp, "non-fullscreen")
	assertFramebufferCovered(t, rp, grab, display, "non-fullscreen")
	assertInspectToggles(t, rp, "non-fullscreen")

	// Browser UI fullscreen is a state change only. The watch has to re-pin
	// the viewport once the toolbar disappears.
	if err := rp.setWindowBounds(&proto.BrowserBounds{WindowState: proto.BrowserWindowStateFullscreen}); err != nil {
		t.Fatalf("enter fullscreen: %v", err)
	}
	waitContentAndFrame(t, rp, grab, display, "fullscreen")

	// Leaving fullscreen restores a normal window with the toolbar on screen.
	if err := rp.setWindowBounds(windowStateOnlyBounds()); err != nil {
		t.Fatalf("leave fullscreen: %v", err)
	}
	waitContentAndFrame(t, rp, grab, display, "after leaving fullscreen")
	assertPickHitsButton(t, rp, "after leaving fullscreen")
}

func assertContentCovers(t *testing.T, rp *rodPage, label string) {
	t.Helper()
	if err := (proto.EmulationClearDeviceMetricsOverride{}).Call(rp.page); err != nil {
		t.Fatalf("%s clear metrics: %v", label, err)
	}
	time.Sleep(150 * time.Millisecond)
	g, err := rp.readDesktopGeom()
	if err != nil {
		t.Fatalf("%s read geometry: %v", label, err)
	}
	t.Logf("%s geom left=%d top=%d outer=%dx%d content=%dx%d", label, g.left, g.top, g.outerW, g.outerH, g.contentW, g.contentH)
	if !desktopReady(g) {
		t.Fatalf("%s: %v", label, desktopNotReadyError(g))
	}
}

func assertInspectToggles(t *testing.T, rp *rodPage, label string) {
	t.Helper()
	if err := rp.SetInspect(true); err != nil {
		t.Fatalf("%s inspect on: %v", label, err)
	}
	if err := inspectModeRequest(false).Call(rp.page); err != nil {
		t.Fatalf("%s setInspectMode none: %v", label, err)
	}
	if err := rp.SetInspect(false); err != nil {
		t.Fatalf("%s inspect off: %v", label, err)
	}
}

func assertPickHitsButton(t *testing.T, rp *rodPage, label string) {
	t.Helper()
	res, err := rp.page.Eval(`() => {
		const el = document.getElementById('pick-me');
		const r = el.getBoundingClientRect();
		const x = r.x + r.width / 2;
		const y = r.y + r.height / 2;
		const hit = document.elementFromPoint(x, y);
		return {x, y, id: hit && hit.id, w: r.width, h: r.height};
	}`)
	if err != nil {
		t.Fatalf("%s pick: %v", label, err)
	}
	id := res.Value.Get("id").Str()
	x := res.Value.Get("x").Num()
	y := res.Value.Get("y").Num()
	t.Logf("%s pick x=%.1f y=%.1f id=%s", label, x, y, id)
	if id != "pick-me" {
		t.Fatalf("%s: point %.1f,%.1f hit %q, want #pick-me", label, x, y, id)
	}
	if y < 0 || y >= ViewportHeight || x < 0 || x >= ViewportWidth {
		t.Fatalf("%s: pick point %.1f,%.1f outside the desktop", label, x, y)
	}
}

func assertFramebufferCovered(t *testing.T, rp *rodPage, grab, display, label string) {
	t.Helper()
	if err := framebufferCoverError(rp, grab, display); err != nil {
		t.Fatalf("%s: %v", label, err)
	}
}

func waitContentAndFrame(t *testing.T, rp *rodPage, grab, display, label string) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	var last error
	for time.Now().Before(deadline) {
		gerr := contentCoverError(rp)
		ferr := framebufferCoverError(rp, grab, display)
		if gerr == nil && ferr == nil {
			t.Logf("%s covered", label)
			return
		}
		if gerr != nil {
			last = gerr
		} else {
			last = ferr
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("%s not covered: %v", label, last)
}

func contentCoverError(rp *rodPage) error {
	if err := (proto.EmulationClearDeviceMetricsOverride{}).Call(rp.page); err != nil {
		return err
	}
	time.Sleep(80 * time.Millisecond)
	g, err := rp.readDesktopGeom()
	if err != nil {
		return err
	}
	if !desktopReady(g) {
		return desktopNotReadyError(g)
	}
	return nil
}

// framebufferCoverError samples the screen: the toolbar band (when the window
// has one) must be painted browser UI, the page top bar must sit right below
// it, and the bottom band must be page, not black desktop.
func framebufferCoverError(rp *rodPage, grab, display string) error {
	g, err := rp.readDesktopGeom()
	if err != nil {
		return err
	}
	contentTop := g.top + g.outerH - g.contentH
	ui, top, bottom, black, samples, err := sampleFramebuffer(grab, display, contentTop)
	if err != nil {
		return err
	}
	if contentTop > 16 {
		if nearRGB(ui, [3]int{34, 197, 94}, 48) || nearRGB(ui, [3]int{0, 0, 0}, 8) {
			return fmt.Errorf("toolbar pixel %v is page or black desktop, want browser UI", ui)
		}
	}
	if !nearRGB(top, [3]int{34, 197, 94}, 48) {
		return fmt.Errorf("pixel %v below the toolbar (content top %d) is not the page top bar", top, contentTop)
	}
	if !nearRGB(bottom, [3]int{225, 29, 72}, 48) {
		return fmt.Errorf("bottom pixel %v is not the page bottom bar", bottom)
	}
	if samples == 0 || black*5 > samples {
		return fmt.Errorf("bottom band is black desktop (%d/%d pixels)", black, samples)
	}
	return nil
}

func nearRGB(got, want [3]int, tol int) bool {
	for i := 0; i < 3; i++ {
		d := got[i] - want[i]
		if d < 0 {
			d = -d
		}
		if d > tol {
			return false
		}
	}
	return true
}

func compileGrab(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "grab.c")
	bin := filepath.Join(dir, "grab")
	if err := os.WriteFile(src, []byte(xGrabSource), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("gcc", "-O2", "-o", bin, src, "-lX11")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("gcc grab: %v\n%s", err, out)
	}
	return bin
}

func sampleFramebuffer(grab, display string, contentTop int) (ui, top, bottom [3]int, black, samples int, err error) {
	run := exec.Command(grab, strconv.Itoa(contentTop))
	run.Env = append(os.Environ(), "DISPLAY="+display)
	out, err := run.CombinedOutput()
	if err != nil {
		return [3]int{}, [3]int{}, [3]int{}, 0, 0, fmt.Errorf("grab: %v\n%s", err, out)
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		switch fields[0] {
		case "ui":
			ui, err = parseRGB(fields[1:])
		case "top":
			top, err = parseRGB(fields[1:])
		case "bottom":
			bottom, err = parseRGB(fields[1:])
		case "bottom_black":
			if len(fields) < 2 {
				err = fmt.Errorf("bottom_black missing count")
				break
			}
			_, err = fmt.Sscanf(fields[1], "%d/%d", &black, &samples)
		}
		if err != nil {
			return [3]int{}, [3]int{}, [3]int{}, 0, 0, err
		}
	}
	return ui, top, bottom, black, samples, nil
}

func parseRGB(fields []string) ([3]int, error) {
	if len(fields) != 3 {
		return [3]int{}, fmt.Errorf("rgb fields: %v", fields)
	}
	var rgb [3]int
	for i := 0; i < 3; i++ {
		n, err := strconv.Atoi(fields[i])
		if err != nil {
			return [3]int{}, err
		}
		rgb[i] = n
	}
	return rgb, nil
}

func findChromeBinary() string {
	matches, _ := filepath.Glob("/ms-playwright/chromium-*/chrome-linux64/chrome")
	if len(matches) > 0 {
		return matches[0]
	}
	for _, name := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	return ""
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func waitCDP(t *testing.T, base string) {
	t.Helper()
	deadline := time.Now().Add(25 * time.Second)
	var last error
	for time.Now().Before(deadline) {
		resp, err := http.Get(base + "/json/version")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return
			}
			last = fmt.Errorf("status %s", resp.Status)
		} else {
			last = err
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("cdp not ready: %v", last)
}

func coverPageQuery() string {
	html := "<!doctype html><style>" +
		"html,body{margin:0;height:100%;background:#1144aa}" +
		"#top{position:fixed;left:0;right:0;top:0;height:40px;background:#22c55e}" +
		"#bottom{position:fixed;left:0;right:0;bottom:0;height:40px;background:#e11d48}" +
		"#pick-me{position:fixed;left:120px;top:240px;width:160px;height:48px}" +
		"</style><div id=top></div><div id=bottom></div><button id=pick-me>Pick</button>"
	return url.PathEscape(html)
}

const xGrabSource = `
#include <X11/Xlib.h>
#include <X11/Xutil.h>
#include <stdio.h>
#include <stdlib.h>
static int chan(unsigned long p, unsigned long mask) {
  int shift = 0, bits = 0;
  unsigned long m;
  if (!mask) return 0;
  while (((mask >> shift) & 1UL) == 0) shift++;
  m = mask >> shift;
  while (m & 1UL) { bits++; m >>= 1; }
  int v = (int)((p & mask) >> shift);
  if (bits < 8) v <<= (8 - bits);
  if (bits > 8) v >>= (bits - 8);
  return v;
}
int main(int argc, char **argv) {
  int contentTop = argc > 1 ? atoi(argv[1]) : 0;
  Display *d = XOpenDisplay(NULL);
  if (!d) { fprintf(stderr, "no display\n"); return 1; }
  Window root = DefaultRootWindow(d);
  XImage *img = XGetImage(d, root, 0, 0, 1920, 1080, AllPlanes, ZPixmap);
  if (!img) { fprintf(stderr, "no image\n"); return 1; }
  int xs[3] = {16, 16, 16};
  int ys[3] = {8, contentTop + 16, 1060};
  const char *names[3] = {"ui", "top", "bottom"};
  for (int i = 0; i < 3; i++) {
    unsigned long p = XGetPixel(img, xs[i], ys[i]);
    printf("%s %d %d %d\n", names[i], chan(p, img->red_mask), chan(p, img->green_mask), chan(p, img->blue_mask));
  }
  int black = 0, n = 0;
  for (int y = 980; y < 1080; y += 4) {
    for (int x = 0; x < 1920; x += 16) {
      unsigned long p = XGetPixel(img, x, y);
      int r = chan(p, img->red_mask), g = chan(p, img->green_mask), b = chan(p, img->blue_mask);
      n++;
      if (r < 8 && g < 8 && b < 8) black++;
    }
  }
  printf("bottom_black %d/%d\n", black, n);
  XDestroyImage(img);
  XCloseDisplay(d);
  return 0;
}
`
