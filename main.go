package main

import (
	"archive/zip"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/timuzkas/tgtk4"
)

type TConvApp struct {
	*tgtk4.App
	InputPaths   []string
	OutputPath   string
	Converting   bool

	MainOverlay  *gtk.Overlay
	MainConvArea *gtk.Box
	MidBox       *gtk.Box
	InputBox     *gtk.Box
	OutputBox    *gtk.Box
	ConvBtn      *gtk.Button
	ArrowIcon    *gtk.Image
	FormatCombo  *gtk.DropDown
	Lightbox     *tgtk4.Lightbox
	
	ProgressBar  *gtk.ProgressBar

	// Settings
	QualityScale    *gtk.Scale
	TargetQuality   float64
	QualityVelocity float64
	StripMeta       *gtk.CheckButton

	// Scrolling state
	ScrollVelocity     float64
	ScrollOvershoot    float64
	ScrollInertiaTimer glib.SourceHandle

	TargetProgress     float64
	DisplayedProgress  float64
	ProgressVelocity   float64

	ImageFormats []string
	VideoFormats []string
}

func main() {
	app := &TConvApp{
		App: tgtk4.NewApp("dev.timuzkas.tconv", "tconv"),
		ImageFormats: []string{"png", "jpg", "webp", "gif", "avif", "bmp", "tiff"},
		VideoFormats: []string{"mp4", "gif", "webm", "mov", "mkv"},
	}
	app.Run(func() { app.BuildUI() })
}

func (a *TConvApp) BuildUI() {
	tgtk4.SetupTheme(a.Config, fmt.Sprintf(`
		.main-conv { 
			padding: 64px; 
			background-color: %s;
		}
		.drop-zone { 
			background-color: rgba(255, 255, 255, 0.002); 
			border: 1px solid %s; 
			min-width: 380px; 
			min-height: 380px; 
			transition: all 0.3s cubic-bezier(0.2, 0.8, 0.2, 1);
		}
		.drop-zone.active { 
			background-color: %s05; 
			border-color: %s; 
		}

		.drop-zone-hover {
			background-color: %s1a;
			opacity: 0;
			transition: all 0.4s cubic-bezier(0.2, 0.8, 0.2, 1);
		}
		.drop-zone-container:hover .drop-zone-hover {
			opacity: 1;
		}
		.hover-box {
			background-color: %s;
			border: 1px solid %s;
			padding: 24px 32px;
			transition: all 0.4s cubic-bezier(0.2, 0.8, 0.2, 1);
			transform: scale(0.92);
		}
		.drop-zone-container:hover .hover-box {
			transform: scale(1);
		}
		.hover-label {
			font-family: "JetBrains Mono", monospace;
			font-size: 10px;
			font-weight: 600;
			color: %s;
			letter-spacing: 0.1em;
			text-align: center;
		}
		
		.conv-btn { 
			background-color: #0c0c0c;
			border: 1px solid %s; 
			border-radius: 0; 
			padding: 12px 36px; 
			transition: all 0.2s cubic-bezier(0.2, 0.8, 0.2, 1);
		}
		.conv-btn label { 
			font-family: "JetBrains Mono", monospace; 
			font-size: 10px; 
			font-weight: 600; 
			letter-spacing: 0.15em; 
			color: %s; 
		}
		.conv-btn:hover { 
			background-color: %s; 
			border-color: %s;
		}
		.conv-btn:hover label { color: %s; }
		.conv-btn:active { transform: scale(0.97); }
		.conv-btn.processing { border-color: %s; animation: processing-blink 1s infinite alternate; }
		.conv-btn.processing label { color: #ffffff; }

		.drop-zone:hover { border-color: %s; }
		.drop-zone.active:hover { border-color: %s; }

		@keyframes processing-blink {
			from { opacity: 1; }
			to { opacity: 0.5; }
		}

		.arrow-icon { color: %s; transition: all 0.5s cubic-bezier(0.2, 0.8, 0.2, 1); }
		.arrow-icon.ready { color: %s; transform: scale(1.3); }
		
		.header-label { font-family: "JetBrains Mono", monospace; font-size: 9px; font-weight: 700; color: %s; }
		.header-group { margin: 0 12px; }

		.title { font-size: 10px; letter-spacing: 0.05em; font-weight: 700; }
	`, 
	a.Config.Bg, a.Config.Border, a.Config.Accent, a.Config.Accent, a.Config.Accent,
	a.Config.Bg2, a.Config.Border, a.Config.Accent, a.Config.Border, a.Config.Accent,
	a.Config.Accent, a.Config.Accent, a.Config.Bg, a.Config.Accent, a.Config.Muted,
	a.Config.Accent, a.Config.Border, a.Config.Accent, a.Config.Muted))

	a.MainOverlay = gtk.NewOverlay()
	a.Win.SetChild(a.MainOverlay)
	a.Lightbox = tgtk4.NewLightbox(a.MainOverlay)

	mainBox := gtk.NewBox(gtk.OrientationVertical, 0)
	a.MainOverlay.SetChild(mainBox)

	header := gtk.NewHeaderBar()
	titleLabel := gtk.NewLabel("// tconv"); titleLabel.AddCSSClass("title")
	header.SetTitleWidget(titleLabel); a.Win.SetTitlebar(header)

	fGroup := gtk.NewBox(gtk.OrientationHorizontal, 0); fGroup.AddCSSClass("header-group"); fGroup.SetVAlign(gtk.AlignCenter)
	flbl := gtk.NewLabel("output"); flbl.AddCSSClass("header-label"); flbl.SetMarginEnd(8)
	fGroup.Append(flbl); a.FormatCombo = gtk.NewDropDownFromStrings(a.ImageFormats); fGroup.Append(a.FormatCombo)
	header.PackStart(fGroup)

	qBox, qScale := tgtk4.NewLabeledSlider("quality", 1, 100, 1)
	a.TargetQuality = 90; qScale.SetValue(a.TargetQuality); a.QualityScale = qScale
	qBox.AddCSSClass("header-group"); qBox.SetVAlign(gtk.AlignCenter); qBox.SetSizeRequest(130, -1)
	header.PackStart(qBox)
	
	a.UpdateQualityTooltip()
	
	qScale.Connect("value-changed", func() {
		a.UpdateQualityTooltip()
		val := qScale.Value()
		if math.Abs(val - a.TargetQuality) > 1.0 {
			a.TargetQuality = val
			a.QualityVelocity = 0
		}
	})

	var sliderTimer glib.SourceHandle
	scrollEvent := gtk.NewEventControllerScroll(gtk.EventControllerScrollVertical)
	scrollEvent.ConnectScroll(func(dx, dy float64) bool {
		a.TargetQuality -= dy * 5
		if a.TargetQuality < 1 { a.TargetQuality = 1 }
		if a.TargetQuality > 100 { a.TargetQuality = 100 }
		
		if sliderTimer == 0 {
			sliderTimer = glib.TimeoutAdd(16, func() bool {
				curr := qScale.Value()
				
				stiffness := 0.15
				damping := 0.75
				
				force := (a.TargetQuality - curr) * stiffness
				a.QualityVelocity = (a.QualityVelocity + force) * damping
				next := curr + a.QualityVelocity
				
				if math.Abs(a.TargetQuality - next) < 0.01 && math.Abs(a.QualityVelocity) < 0.01 {
					qScale.SetValue(a.TargetQuality)
					sliderTimer = 0
					a.QualityVelocity = 0
					return false
				}
				
				qScale.SetValue(next)
				return true
			})
		}
		return true
	})
	qScale.AddController(scrollEvent)

	a.StripMeta = gtk.NewCheckButtonWithLabel("strip metadata"); a.StripMeta.SetVAlign(gtk.AlignCenter); a.StripMeta.AddCSSClass("header-group")
	header.PackStart(a.StripMeta)

	btnClear := tgtk4.IconBtn("edit-clear-all-symbolic", ""); btnClear.ConnectClicked(a.ClearSession); header.PackEnd(btnClear)
	btnFolder := tgtk4.IconBtn("folder-open-symbolic", ""); btnFolder.ConnectClicked(a.OpenOutputFolder); header.PackEnd(btnFolder)

	a.ProgressBar = tgtk4.NewProgressBar(); a.ProgressBar.SetVisible(false); mainBox.Append(a.ProgressBar)

	scroll := gtk.NewScrolledWindow(); scroll.SetVExpand(true)
	scroll.SetPolicy(gtk.PolicyNever, gtk.PolicyAutomatic)
	scroll.SetKineticScrolling(true)
	scroll.SetOverlayScrolling(true)

	scrollCtrl := gtk.NewEventControllerScroll(gtk.EventControllerScrollVertical)
	scrollCtrl.ConnectScroll(func(dx, dy float64) bool {
		a.ScrollVelocity += dy * 14
		if a.ScrollInertiaTimer == 0 {
			a.ScrollInertiaTimer = glib.TimeoutAdd(16, func() bool {
				adj := scroll.VAdjustment()
				min := adj.Lower()
				max := adj.Upper() - adj.PageSize()
				current := adj.Value()

				if (current <= min && a.ScrollVelocity < 0) || (current >= max && a.ScrollVelocity > 0) {
					a.ScrollOvershoot -= a.ScrollVelocity * 0.4
					a.ScrollVelocity *= 0.6
				}

				if math.Abs(a.ScrollVelocity) < 0.1 && math.Abs(a.ScrollOvershoot) < 0.5 {
					a.ScrollInertiaTimer = 0
					a.ScrollVelocity = 0
					a.ScrollOvershoot = 0
					a.MainConvArea.SetMarginTop(0)
					a.MainConvArea.SetMarginBottom(0)
					return false
				}

				adj.SetValue(current + a.ScrollVelocity)
				a.ScrollVelocity *= 0.90

				if math.Abs(a.ScrollOvershoot) > 0 {
					if a.ScrollOvershoot > 0 {
						a.MainConvArea.SetMarginTop(int(a.ScrollOvershoot))
						a.MainConvArea.SetMarginBottom(0)
					} else {
						a.MainConvArea.SetMarginTop(0)
						a.MainConvArea.SetMarginBottom(int(-a.ScrollOvershoot))
					}
					a.ScrollOvershoot *= 0.8
				}

				return true
			})
		}
		return true
	})
	scroll.AddController(scrollCtrl)

	a.MainConvArea = gtk.NewBox(gtk.OrientationHorizontal, 0)
	a.MainConvArea.AddCSSClass("main-conv"); a.MainConvArea.SetHAlign(gtk.AlignCenter); a.MainConvArea.SetVAlign(gtk.AlignCenter)
	a.MainConvArea.SetHExpand(true); a.MainConvArea.SetVExpand(true)
	scroll.SetChild(a.MainConvArea); mainBox.Append(scroll)

	a.InputBox = a.CreateDropZone("source", true); a.MainConvArea.Append(a.InputBox)

	a.MidBox = gtk.NewBox(gtk.OrientationVertical, 48); a.MidBox.SetVAlign(gtk.AlignCenter); a.MidBox.SetHAlign(gtk.AlignCenter)
	a.MidBox.SetMarginStart(64); a.MidBox.SetMarginEnd(64)
	a.ArrowIcon = gtk.NewImageFromIconName("go-next-symbolic"); a.ArrowIcon.SetPixelSize(32); a.ArrowIcon.AddCSSClass("arrow-icon")
	a.MidBox.Append(a.ArrowIcon)

	a.ConvBtn = gtk.NewButton(); a.ConvBtn.SetChild(gtk.NewLabel("convert"))
	a.ConvBtn.AddCSSClass("conv-btn"); a.ConvBtn.ConnectClicked(a.OnConvert); a.MidBox.Append(a.ConvBtn); a.MainConvArea.Append(a.MidBox)

	a.OutputBox = a.CreateDropZone("result", false); a.MainConvArea.Append(a.OutputBox)

	a.Win.Connect("size-allocate", func() {
		w := a.Win.Width()
		if w < 1100 {
			if a.MainConvArea.Orientation() == gtk.OrientationHorizontal {
				a.MainConvArea.SetOrientation(gtk.OrientationVertical)
				a.ArrowIcon.SetFromIconName("go-down-symbolic")
				a.MidBox.SetMarginTop(48); a.MidBox.SetMarginBottom(48); a.MidBox.SetMarginStart(0); a.MidBox.SetMarginEnd(0)
			}
		} else {
			if a.MainConvArea.Orientation() == gtk.OrientationVertical {
				a.MainConvArea.SetOrientation(gtk.OrientationHorizontal)
				a.ArrowIcon.SetFromIconName("go-next-symbolic")
				a.MidBox.SetMarginTop(0); a.MidBox.SetMarginBottom(0); a.MidBox.SetMarginStart(64); a.MidBox.SetMarginEnd(64)
			}
		}
	})

	keyCtrl := gtk.NewEventControllerKey()
	keyCtrl.ConnectKeyPressed(func(val, code uint, state gdk.ModifierType) bool {
		if val == gdk.KEY_Return || val == gdk.KEY_KP_Enter { a.OnConvert(); return true }
		if state.Has(gdk.ControlMask) {
			if val == gdk.KEY_l || val == gdk.KEY_L { a.ClearSession(); return true }
			if val == gdk.KEY_o || val == gdk.KEY_O { a.OpenOutputFolder(); return true }
		}
		if val == gdk.KEY_Escape && a.Lightbox.Overlay.Visible() {
			a.Lightbox.Overlay.RemoveCSSClass("active")
			glib.TimeoutAdd(250, func() bool { a.Lightbox.Overlay.SetVisible(false); return false }); return true
		}
		return false
	})
	a.Win.AddController(keyCtrl)

	mainBox.Append(a.NewStatusBar()); a.SetStatus("ready", false); a.Win.SetSizeRequest(450, 600)
}

func (a *TConvApp) UpdateQualityTooltip() {
	val := a.QualityScale.Value()
	totalSize := int64(0)
	for _, p := range a.InputPaths {
		fi, _ := os.Stat(p)
		if fi != nil { totalSize += fi.Size() }
	}
	if totalSize == 0 {
		a.QualityScale.SetTooltipText(fmt.Sprintf("quality: %d%%", int(val)))
		return
	}
	// TODO REMOVE heuristic for estimation: 0.25 factor for PNG to WebP/JPG
	estSize := float64(totalSize) * (val / 100.0) * 0.25
	saved := totalSize - int64(estSize)
	
	if saved > 0 {
		a.QualityScale.SetTooltipText(fmt.Sprintf("quality: %d%% | %s (saves %s)", int(val), formatSize(int64(estSize)), formatSize(saved)))
	} else {
		a.QualityScale.SetTooltipText(fmt.Sprintf("quality: %d%% | %s", int(val), formatSize(int64(estSize))))
	}
}

func formatSize(b int64) string {
	const unit = 1024
	if b < unit { return fmt.Sprintf("%d B", b) }
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit { div *= unit; exp++ }
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

func (a *TConvApp) CreateDropZone(text string, isInput bool) *gtk.Box {
	overlay := gtk.NewOverlay(); overlay.SetHExpand(false); overlay.SetVExpand(false)
	overlay.AddCSSClass("drop-zone-container")
	innerBox := gtk.NewBox(gtk.OrientationVertical, 0); innerBox.AddCSSClass("drop-zone")
	innerBox.SetSizeRequest(400, 400); innerBox.SetHAlign(gtk.AlignCenter); innerBox.SetVAlign(gtk.AlignCenter)
	hint := gtk.NewLabel(text); hint.AddCSSClass("hint"); hint.SetVExpand(true); hint.SetHAlign(gtk.AlignCenter)
	innerBox.Append(hint); overlay.SetChild(innerBox)

	if isInput {
		hoverOverlay := gtk.NewBox(gtk.OrientationVertical, 0); hoverOverlay.AddCSSClass("drop-zone-hover")
		hoverOverlay.SetHAlign(gtk.AlignFill); hoverOverlay.SetVAlign(gtk.AlignFill)
		
		centerBox := gtk.NewBox(gtk.OrientationVertical, 0); centerBox.SetHAlign(gtk.AlignCenter); centerBox.SetVAlign(gtk.AlignCenter)
		centerBox.AddCSSClass("hover-box"); centerBox.SetVExpand(true)
		
		icon := gtk.NewImageFromIconName("document-open-symbolic"); icon.SetPixelSize(32); icon.SetMarginBottom(16)
		centerBox.Append(icon)
		
		hoverLbl := gtk.NewLabel("select or drop files"); hoverLbl.AddCSSClass("hover-label")
		centerBox.Append(hoverLbl)
		
		hoverOverlay.Append(centerBox)
		overlay.AddOverlay(hoverOverlay)

		target := gtk.NewDropTarget(glib.TypeString, gdk.ActionCopy)
		target.ConnectDrop(func(val *glib.Value, x, y float64) bool {
			str := val.String(); lines := strings.Split(str, "\r\n")
			var paths []string
			for _, l := range lines {
				l = strings.Trim(l, "\"' \r\n")
				if l == "" { continue }
				if strings.HasPrefix(l, "file://") {
					if u, err := url.Parse(l); err == nil { l = u.Path } else { l = strings.TrimPrefix(l, "file://") }
				}
				if _, err := os.Stat(l); err == nil { paths = append(paths, l) }
			}
			if len(paths) > 0 { a.HandleBatchInput(paths); return true }
			return false
		})
		overlay.AddController(target)
		click := gtk.NewGestureClick(); click.ConnectPressed(func(n int, x, y float64) { a.OpenFileDialog() })
		overlay.AddController(click)
	}
	wrapped := gtk.NewBox(gtk.OrientationVertical, 0); wrapped.Append(overlay)
	wrapped.SetMarginStart(12); wrapped.SetMarginEnd(12); wrapped.SetMarginTop(12); wrapped.SetMarginBottom(12)
	wrapped.SetHAlign(gtk.AlignCenter); wrapped.SetVAlign(gtk.AlignCenter)
	return wrapped
}

func (a *TConvApp) OpenFileDialog() {
	dialog := gtk.NewFileChooserNative("select media", &a.Win.Window, gtk.FileChooserActionOpen, "open", "cancel")
	dialog.ConnectResponse(func(resp int) {
		if resp == int(gtk.ResponseAccept) {
			files := dialog.Files()
			var paths []string
			for i := 0; i < int(files.NItems()); i++ {
				paths = append(paths, files.Item(uint(i)).Cast().(*gio.File).Path())
			}
			a.HandleBatchInput(paths)
		}
	})
	dialog.Show()
}

func (a *TConvApp) HandleBatchInput(paths []string) {
	a.InputPaths = paths
	a.SetStatus(fmt.Sprintf("loaded %d file(s)", len(paths)), false)
	if len(paths) > 0 {
		mime := a.DetectMime(paths[0])
		if strings.HasPrefix(mime, "video/") { a.FormatCombo.SetModel(gtk.NewStringList(a.VideoFormats)) } else { a.FormatCombo.SetModel(gtk.NewStringList(a.ImageFormats)) }
	}
	inBox := a.InputBox.FirstChild().(*gtk.Overlay).Child().(*gtk.Box)
	inBox.AddCSSClass("active"); a.ArrowIcon.AddCSSClass("ready")
	for child := inBox.FirstChild(); child != nil; child = inBox.FirstChild() { inBox.Remove(child) }
	if len(paths) == 1 {
		path := paths[0]
		if !strings.HasPrefix(a.DetectMime(path), "video/") {
			pic := tgtk4.NewPicture(path, 380, 380); pic.AddCSSClass("pop-in")
			pic.SetHAlign(gtk.AlignCenter); pic.SetVAlign(gtk.AlignCenter); pic.SetVExpand(true)
			inBox.Append(pic)
		} else {
			lbl := gtk.NewLabel(filepath.Base(path)); lbl.AddCSSClass("title"); lbl.SetVExpand(true); inBox.Append(lbl)
		}
	} else {
		lbl := gtk.NewLabel(fmt.Sprintf("%d files selected", len(paths))); lbl.AddCSSClass("title"); lbl.SetVExpand(true); inBox.Append(lbl)
	}
	a.UpdateQualityTooltip()
}

func (a *TConvApp) DetectMime(path string) string {
	f, _ := os.Open(path); defer f.Close(); buf := make([]byte, 512); n, _ := f.Read(buf); return http.DetectContentType(buf[:n])
}

func (a *TConvApp) OnConvert() {
	if len(a.InputPaths) == 0 || a.Converting { return }
	selected := a.FormatCombo.SelectedItem(); if selected == nil { return }
	targetExt := selected.Cast().(*gtk.StringObject).String()
	a.Converting = true; a.ConvBtn.AddCSSClass("processing")
	
	fmt.Printf("[tconv] starting conversion of %d files to %s\n", len(a.InputPaths), targetExt)

	a.TargetProgress = 0
	a.DisplayedProgress = 0
	a.ProgressVelocity = 0

	var animTimer glib.SourceHandle
	setBtnProgress := func(fraction float64) {
		a.TargetProgress = fraction
		if animTimer == 0 {
			animTimer = glib.TimeoutAdd(16, func() bool {
				// dampening to add cinetics but uhhm might remove later
				stiffness := 0.12
				damping := 0.8
				
				force := (a.TargetProgress - a.DisplayedProgress) * stiffness
				a.ProgressVelocity = (a.ProgressVelocity + force) * damping
				a.DisplayedProgress += a.ProgressVelocity
				
				percent := int(a.DisplayedProgress * 100)
				if percent < 0 { percent = 0 }
				if percent > 100 { percent = 100 }

				glib.IdleAdd(func() {
					css := fmt.Sprintf(".conv-btn { background-image: linear-gradient(to right, %s %d%%, #0c0c0c %d%%); }", 
						a.Config.Accent, percent, percent)
					provider := gtk.NewCSSProvider()
					provider.LoadFromData(css)
					a.ConvBtn.StyleContext().AddProvider(provider, gtk.STYLE_PROVIDER_PRIORITY_APPLICATION)
				})

				if math.Abs(a.TargetProgress - a.DisplayedProgress) < 0.001 && math.Abs(a.ProgressVelocity) < 0.001 {
					animTimer = 0
					return false
				}
				return true
			})
		}
	}

	go func() {
		var convertedPaths []string
		totalFiles := float64(len(a.InputPaths))
		
		for i, path := range a.InputPaths {
			baseProgress := float64(i) / totalFiles
			fileWeight := 1.0 / totalFiles
			
			glib.IdleAdd(func() {
				a.SetStatus(fmt.Sprintf("converting %d/%d...", i+1, len(a.InputPaths)), false)
			})
			
			outPath := filepath.Join(filepath.Dir(path), strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))+"_conv."+targetExt)
			quality := int(a.QualityScale.Value())
			
			isVid := strings.Contains("mp4 webm mov mkv gif", targetExt) || strings.HasPrefix(a.DetectMime(path), "video/")
			
			if !isVid {
				// image conversion (usually fast lol, so no progress)
				args := []string{path}
				if a.StripMeta.Active() { args = append(args, "-strip") }
				args = append(args, "-quality", fmt.Sprintf("%d", quality), outPath)
				cmd := exec.Command("convert", args...)
				cmd.Run()
				setBtnProgress(baseProgress + fileWeight)
			} else {
				// video conversion with real-time progress
				var args []string
				if targetExt == "webm" {
					args = []string{"-y", "-i", path, "-c:v", "libvpx-vp9", "-crf", fmt.Sprintf("%d", (100-quality)*63/100), "-b:v", "0", "-c:a", "libopus", outPath}
				} else if targetExt == "gif" {
					palette := filepath.Join(os.TempDir(), "tconv_palette.png")
					exec.Command("ffmpeg", "-y", "-i", path, "-vf", "palettegen", palette).Run()
					args = []string{"-y", "-i", path, "-i", palette, "-filter_complex", "paletteuse", outPath}
					defer os.Remove(palette)
				} else {
					args = []string{"-y", "-i", path, "-c:v", "libx264", "-crf", fmt.Sprintf("%d", (100-quality)*51/100), "-pix_fmt", "yuv420p", outPath}
				}

				// get duration for progress calculation
				durCmd := exec.Command("ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "default=noprint_wrappers=1:nokey=1", path)
				durOut, _ := durCmd.Output()
				var duration float64
				fmt.Sscanf(string(durOut), "%f", &duration)

				cmd := exec.Command("ffmpeg", args...)
				stderr, _ := cmd.StderrPipe()
				cmd.Start()

				// parse ffmpeg output for "time=HH:MM:SS.ms"
				buf := make([]byte, 1024)
				for {
					n, err := stderr.Read(buf)
					if n > 0 {
						out := string(buf[:n])
						if idx := strings.Index(out, "time="); idx != -1 && duration > 0 {
							timeStr := out[idx+5:]
							if spaceIdx := strings.Index(timeStr, " "); spaceIdx != -1 {
								timeStr = timeStr[:spaceIdx]
							}
							// simple HH:MM:SS parsing
							parts := strings.Split(timeStr, ":")
							if len(parts) == 3 {
								var h, m, s float64
								fmt.Sscanf(parts[0], "%f", &h)
								fmt.Sscanf(parts[1], "%f", &m)
								fmt.Sscanf(parts[2], "%f", &s)
								currentSecs := h*3600 + m*60 + s
								fileProgress := currentSecs / duration
								if fileProgress > 1.0 { fileProgress = 1.0 }
								setBtnProgress(baseProgress + (fileProgress * fileWeight))
							}
						}
					}
					if err != nil { break }
				}
				cmd.Wait()
			}
			
			if _, err := os.Stat(outPath); err == nil {
				convertedPaths = append(convertedPaths, outPath)
			}
		}
		
		glib.IdleAdd(func() {
			a.Converting = false; a.ConvBtn.RemoveCSSClass("processing")
			a.SetStatus(fmt.Sprintf("finished %d files", len(convertedPaths)), false)
			if len(convertedPaths) == 1 { a.OutputPath = convertedPaths[0]; a.ShowOutput(a.OutputPath) } else { a.ShowBatchOutput(convertedPaths) }
			
			glib.TimeoutAdd(2000, func() bool {
				provider := gtk.NewCSSProvider()
				provider.LoadFromData(".conv-btn { background-image: none; }")
				a.ConvBtn.StyleContext().AddProvider(provider, gtk.STYLE_PROVIDER_PRIORITY_APPLICATION)
				return false
			})
		})
	}()
}

func (a *TConvApp) ShowOutput(path string) {
	overlay := a.OutputBox.FirstChild().(*gtk.Overlay); innerBox := overlay.Child().(*gtk.Box)
	innerBox.AddCSSClass("active"); innerBox.AddCSSClass("overlay-container")
	for child := innerBox.FirstChild(); child != nil; child = innerBox.FirstChild() { innerBox.Remove(child) }
	ext := strings.ToLower(filepath.Ext(path)); isImg := strings.Contains(".png.jpg.jpeg.webp.gif.avif.bmp.tiff", ext)
	if isImg {
		anim := tgtk4.NewAnimatedPicture(path, 380, 380, a.Config.Accent)
		anim.SetHAlign(gtk.AlignCenter); anim.SetVAlign(gtk.AlignCenter)
		innerBox.Append(anim)
		click := gtk.NewGestureClick(); click.ConnectPressed(func(n int, x, y float64) { a.Lightbox.Show(path) })
		innerBox.AddController(click)
	} else {
		lbl := gtk.NewLabel(filepath.Base(path)); lbl.AddCSSClass("title"); lbl.SetVExpand(true); lbl.AddCSSClass("pop-in")
		innerBox.Append(lbl)
	}
	actionBox := gtk.NewBox(gtk.OrientationHorizontal, 0); actionBox.AddCSSClass("overlay-actions"); actionBox.SetHAlign(gtk.AlignEnd); actionBox.SetVAlign(gtk.AlignEnd)
	actionBox.Append(tgtk4.MiniActionBtn("edit-copy-symbolic", "copy", func() { a.CopyFile(path) }))
	actionBox.Append(tgtk4.MiniActionBtn("folder-open-symbolic", "open", func() { exec.Command("xdg-open", filepath.Dir(path)).Start() }))
	actionBox.Append(tgtk4.MiniActionBtn("document-save-as-symbolic", "save as", func() { a.SaveAsDialog(path) }))
	overlay.AddOverlay(actionBox)
}

func (a *TConvApp) CopyFile(path string) {
	abs, _ := filepath.Abs(path)
	uri := "file://" + abs
	
	exec.Command("sh", "-c", fmt.Sprintf("echo -n '%s' | wl-copy --type text/uri-list", uri)).Run()
	
	mime := a.DetectMime(path)
	if strings.HasPrefix(mime, "image/") {
		go func() {
			f, err := os.Open(path)
			if err == nil {
				defer f.Close()
				cmd := exec.Command("wl-copy", "--type", mime)
				cmd.Stdin = f
				cmd.Run()
			}
		}()
	}
	
	a.SetStatus("file copied", false)
}

func (a *TConvApp) ShowBatchOutput(paths []string) {
	overlay := a.OutputBox.FirstChild().(*gtk.Overlay); innerBox := overlay.Child().(*gtk.Box)
	innerBox.AddCSSClass("active"); innerBox.AddCSSClass("overlay-container")
	for child := innerBox.FirstChild(); child != nil; child = innerBox.FirstChild() { innerBox.Remove(child) }
	lbl := gtk.NewLabel(fmt.Sprintf("%d files ready", len(paths))); lbl.AddCSSClass("title"); lbl.SetVExpand(true); innerBox.Append(lbl)
	actionBox := gtk.NewBox(gtk.OrientationHorizontal, 0); actionBox.AddCSSClass("overlay-actions"); actionBox.SetHAlign(gtk.AlignEnd); actionBox.SetVAlign(gtk.AlignEnd)
	actionBox.Append(tgtk4.MiniActionBtn("archive-insert-symbolic", "download zip", func() { a.DownloadZip(paths) }))
	overlay.AddOverlay(actionBox)
}

func (a *TConvApp) DownloadZip(paths []string) {
	dialog := gtk.NewFileChooserNative("save zip", &a.Win.Window, gtk.FileChooserActionSave, "save", "cancel")
	dialog.SetCurrentName("converted_files.zip")
	dialog.ConnectResponse(func(resp int) {
		if resp == int(gtk.ResponseAccept) {
			f, _ := os.Create(dialog.File().Path()); defer f.Close(); zw := zip.NewWriter(f); defer zw.Close()
			for _, p := range paths {
				fr, _ := os.Open(p); fi, _ := fr.Stat(); zh, _ := zip.FileInfoHeader(fi); zh.Name = filepath.Base(p); zh.Method = zip.Deflate
				w, _ := zw.CreateHeader(zh); io.Copy(w, fr); fr.Close()
			}
			a.SetStatus("zip saved", false)
		}
	})
	dialog.Show()
}

func (a *TConvApp) ClearSession() {
	a.InputPaths = nil; a.OutputPath = ""; a.ProgressBar.SetVisible(false)
	a.ArrowIcon.RemoveCSSClass("ready")
	
	clearZone := func(zone *gtk.Box, text string) {
		overlay := zone.FirstChild().(*gtk.Overlay)
		innerBox := overlay.Child().(*gtk.Box)
		innerBox.RemoveCSSClass("active")
		innerBox.RemoveCSSClass("overlay-container")
		
		for child := innerBox.FirstChild(); child != nil; child = innerBox.FirstChild() {
			innerBox.Remove(child)
		}
		
		// Properly centered label
		lbl := gtk.NewLabel(text)
		lbl.AddCSSClass("hint")
		lbl.SetVExpand(true)
		lbl.SetHAlign(gtk.AlignCenter)
		innerBox.Append(lbl)

		// Remove any overlays (like action buttons) using children observer
		mainChild := overlay.Child()
		children := overlay.ObserveChildren()
		var toRemove []gtk.Widgetter
		for i := uint(0); i < children.NItems(); i++ {
			c := children.Item(i).Cast().(gtk.Widgetter)
			if c != mainChild {
				toRemove = append(toRemove, c)
			}
		}
		for _, c := range toRemove {
			overlay.RemoveOverlay(c)
		}
	}

	clearZone(a.InputBox, "source")
	clearZone(a.OutputBox, "result")
}

func (a *TConvApp) OpenOutputFolder() {
	dir := "."
	if len(a.InputPaths) > 0 { dir = filepath.Dir(a.InputPaths[0]) }
	exec.Command("xdg-open", dir).Start()
}

func (a *TConvApp) SaveAsDialog(path string) {
	dialog := gtk.NewFileChooserNative("save as", &a.Win.Window, gtk.FileChooserActionSave, "save", "cancel")
	dialog.SetCurrentName(filepath.Base(path))
	dialog.ConnectResponse(func(resp int) {
		if resp == int(gtk.ResponseAccept) {
			data, _ := os.ReadFile(path); os.WriteFile(dialog.File().Path(), data, 0644); a.SetStatus("saved", false)
		}
	})
	dialog.Show()
}
