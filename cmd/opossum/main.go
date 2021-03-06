package main

import (
	"9fans.net/go/draw"
	"fmt"
	"image"
	"os"
	"github.com/knusbaum/go9p"
	"github.com/psilva261/opossum/browser"
	"github.com/psilva261/opossum/js"
	"github.com/psilva261/opossum/logger"
	"github.com/psilva261/opossum/style"
	"os/signal"
	"runtime/pprof"
	"time"
	"github.com/mjl-/duit"
)

var dui *duit.DUI

var cpuprofile string
var startPage string = "http://9p.io"
var dbg bool

func init() {
	browser.EnableNoScriptTag = true
}

func mainView(b *browser.Browser) []*duit.Kid {
	return duit.NewKids(
		&duit.Grid{
			Columns: 2,
			Padding: duit.NSpace(2, duit.SpaceXY(5, 3)),
			Halign:  []duit.Halign{duit.HalignLeft, duit.HalignRight},
			Valign:  []duit.Valign{duit.ValignMiddle, duit.ValignMiddle},
			Kids: duit.NewKids(
				&duit.Button{
					Text:  "Back",
					Font:  browser.Style.Font(),
					Click: b.Back,
				},
				&duit.Box{
					Kids: duit.NewKids(
						b.LocationField,
					),
				},
			),
		},
		b.StatusBar,
		b.Website,
	)
}

func render(b *browser.Browser, kids []*duit.Kid) {
	white, err := dui.Display.AllocImage(image.Rect(0, 0, 10, 10), draw.ARGB32, true, 0xffffffff)
	if err != nil {
		log.Errorf("%v", err)
	}
	dui.Top.UI = &duit.Box{
		Kids: kids,
		Background: white,
	}
	browser.PrintTree(b.Website.UI)
	log.Printf("Render.....")
	dui.MarkLayout(dui.Top.UI)
	dui.MarkDraw(dui.Top.UI)
	dui.Render()
	log.Printf("Rendering done")
}

func confirm(b *browser.Browser, text, value string) chan string {
	res := make(chan string)

	dui.Call <- func() {
		f := &duit.Field{
			Text: value,
		}

		kids := duit.NewKids(
			&duit.Grid{
				Columns: 3,
				Padding: duit.NSpace(3, duit.SpaceXY(5, 3)),
				Halign:  []duit.Halign{duit.HalignLeft, duit.HalignLeft, duit.HalignRight},
				Valign:  []duit.Valign{duit.ValignMiddle, duit.ValignMiddle, duit.ValignMiddle},
				Kids: duit.NewKids(
					&duit.Button{
						Text:  "Ok",
						Font:  browser.Style.Font(),
						Click: func() (e duit.Event) {
							res <- f.Text
							e.Consumed = true
							return
						},
					},
					&duit.Button{
						Text:  "Abort",
						Font:  browser.Style.Font(),
						Click: func() (e duit.Event) {
							res <- ""
							e.Consumed = true
							return
						},
					},
					f,
				),
			},
			&duit.Label{
				Text: text,
			},
		)

		render(b, kids)
	}

	return res
}

func Main() (err error) {
	dui, err = duit.NewDUI("opossum", nil) // TODO: rm global var
	if err != nil {
		return fmt.Errorf("new dui: %w", err)
	}
	dui.Debug = dbg

	style.Init(dui)

	b := browser.NewBrowser(dui, startPage)
	b.Download = func(done chan int) chan string {
		go func() {
			<-done
			dui.Call <- func() {
				render(b, mainView(b))
			}
		}()
		return confirm(b, fmt.Sprintf("Download %v", b.URL()), "/download.file")
	}
	render(b, mainView(b))

	for {
		select {
		case e := <-dui.Inputs:
			dui.Input(e)

		case err, ok := <-dui.Error:
			if !ok {
				return nil
			}
			log.Printf("main: duit: %s\n", err)
		}
	}
}

func usage() {
	fmt.Printf("usage: opossum [-v|-vv] [-h] [-jsinsecure] [-cpuprofile fn] [startPage]\n")
	os.Exit(1)
}

func main() {
	quiet := true
	args := os.Args[1:]
	for len(args) > 0 {
		switch args[0] {
		case "-vv":
			quiet = false
			dbg = true
			args = args[1:]
		case "-v":
			quiet = false
			args = args[1:]
		case "-h":
			usage()
			args = args[1:]
		case "-jsinsecure":
			browser.ExperimentalJsInsecure = true
			args = args[1:]
		case "-cpuprofile":
			cpuprofile, args = args[0], args[2:]
		default:
			if len(args) > 1 {
				usage()
			}
			startPage, args = args[0], args[1:]
		}
	}

	if quiet {
		log.SetQuiet()
	}

	if cpuprofile != "" {
		f, err := os.Create(cpuprofile)
		if err != nil {
			log.Fatal(err)
		}
		pprof.StartCPUProfile(f)
		go func() {
			<-time.After(time.Minute)
			pprof.StopCPUProfile()
			os.Exit(2)
		}()
	}

	log.Debug = dbg
	go9p.Verbose = log.Debug

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, os.Kill)

	go func() {
		<-done
		js.Stop()
		os.Exit(1)
	}()

	if err := Main(); err != nil {
		log.Fatalf("Main: %v", err)
	}
}
