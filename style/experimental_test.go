package style

import (
	"9fans.net/go/draw"
	"github.com/chris-ramon/douceur/css"
	"github.com/psilva261/opossum/logger"
	"testing"
)

func init() {
	log.Debug = true
}

func TestBackgroundImageUrl(t *testing.T) {
	suffix := ""
	for _, quote := range []string{"", "'", `"`} {
		url := "/foo.png"
		decl := css.Declaration{
			Value: "url(" + quote + url + quote + ")" + suffix,
		}
		imgUrl, ok := backgroundImageUrl(decl)
		if !ok {
			t.Fatalf("not ok")
		}
		if imgUrl != url {
			t.Fatalf("expected %+v but got %+v", url, imgUrl)
		}
	}
}

func TestBackgroundColor(t *testing.T) {
	colors := map[string]draw.Color{
		"#000000": draw.Black,
		"#ffffff": draw.White,
	}

	for _, k := range []string{"background", "background-color"} {
		m := Map{
			Declarations: make(map[string]css.Declaration),
		}
		for hex, d := range colors {
			m.Declarations[k] = css.Declaration{
				Property: k,
				Value:    hex,
			}

			if b, ok := m.backgroundColor(); !ok || b != d {
				t.Fatalf("%v", b)
			}
		}
	}
}

func TestBackgroundGradient(t *testing.T) {
	values := map[string]uint32{
		"linear-gradient(to right,rgb(10,0,50,1),rgb(200,0,50,1))":              0x690032ff,
		"linear-gradient(to right,rgb(0,60,60,1),rgba(0,180,180,1))":            0x007878ff,
		"linear-gradient(to bottom, rgba(40,40,40,1) 0%,rgba(40,40,40,1) 100%)": 0x282828ff,
	}
	for v, cc := range values {
		m := Map{
			Declarations: make(map[string]css.Declaration),
		}
		m.Declarations["background"] = css.Declaration{
			Property: "background",
			Value:    v,
		}
		c, ok := m.backgroundGradient()
		if !ok {
			t.Fail()
		}
		if uint32(c) != cc {
			t.Fail()
		}
	}
}
