package style

import (
	"9fans.net/go/draw"
	"fmt"
	"github.com/chris-ramon/douceur/css"
	"github.com/chris-ramon/douceur/inliner"
	"github.com/chris-ramon/douceur/parser"
	cssSel "github.com/psilva261/css"
	"github.com/mjl-/duit"
	"golang.org/x/image/colornames"
	"golang.org/x/net/html"
	"io/ioutil"
	"github.com/psilva261/opossum/logger"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

var CssFonts = true
var fontCache = make(map[string]*draw.Font)

// experimentalUseBoxBackgrounds should probably be combined with
// setting stashElements to false
var ExperimentalUseBoxBackgrounds = false
var dui *duit.DUI
var availableFontNames []string
var log *logger.Logger

var rMinWidth = regexp.MustCompile(`min-width: (\d+)px`)
var rMaxWidth = regexp.MustCompile(`max-width: (\d+)px`)

const FontBaseSize = 11.0

const AddOnCSS = `
a, span, i, tt, b {
  display: inline;
}

button, textarea, input, select {
  display: inline-block;
}

h1, h2, h3, h4. h5, h6, div, center, frame, frameset, p, ul, menu, pre, dir {
	display: block;
}

a {
  color: blue;
}
`

func Init(d *duit.DUI, l *logger.Logger) {
	dui = d
	log = l

	initFontserver()
}

func initFontserver() {
	buf, err := exec.Command("fontsrv", "-p", ".").Output()
	if err == nil {
		availableFontNames = strings.Split(string(buf), "\n")
	} else {
		log.Printf("exec fontsrv: %v", err)
	}
}

func Hrefs(doc *html.Node) (hrefs []string) {
	hrefs = make([]string, 0, 3)

	var f func(n *html.Node)
	f = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "link" {
			isStylesheet := false
			href := ""

			for _, a := range n.Attr {
				switch strings.ToLower(a.Key) {
				case "rel":
					if a.Val == "stylesheet" {
						isStylesheet = true
					}
				case "href":
					href = a.Val
				}
			}

			if isStylesheet {
				hrefs = append(hrefs, href)
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}

	f(doc)

	return
}

func MergeNodeMaps(m, addOn map[*html.Node]Map) {
	for n, mp := range addOn {
		// "zero" valued Map if it doesn't exist yet
		initial := m[n]

		m[n] = initial.ApplyChildStyle(mp)
	}
}

func FetchNodeMap(doc *html.Node, cssText string, windowWidth int) (m map[*html.Node]Map, err error) {
	mr, err := FetchNodeRules(doc, cssText, windowWidth)
	if err != nil {
		return nil, fmt.Errorf("fetch rules: %w", err)
	}
	m = make(map[*html.Node]Map)
	for n, rs := range mr {
		ds := make(map[string]css.Declaration)
		for _, r := range rs {
			for _, d := range r.Declarations {
				if exist, ok := ds[d.Property]; ok && smaller(*d, exist) {
					continue
				}
				ds[d.Property] = *d
			}
		}
		m[n] = Map{ds}
	}
	return
}

func smaller(d, dd css.Declaration) bool {
	return dd.Important
}

func FetchNodeRules(doc *html.Node, cssText string, windowWidth int) (m map[*html.Node][]*css.Rule, err error) {
	m = make(map[*html.Node][]*css.Rule)
	s, err := parser.Parse(cssText)
	if err != nil {
		return nil, fmt.Errorf("douceur parse: %w", err)
	}
	processRule := func(m map[*html.Node][]*css.Rule, r *css.Rule) (err error) {
		for _, sel := range r.Selectors {
			cs, err := cssSel.Compile(sel.Value)
			if err != nil {
				log.Printf("cssSel compile %v: %v", sel.Value, err)
				continue
			}
			for _, el := range cs.Select(doc) {
				existing, ok := m[el]
				if !ok {
					existing = make([]*css.Rule, 0, 3)
				}
				existing = append(existing, r)
				m[el] = existing
			}
		}
		return
	}
	for _, r := range s.Rules {
		if err := processRule(m, r); err != nil {
			return nil, fmt.Errorf("process rule: %w", err)
		}

		// for media queries
		if strings.Contains(r.Prelude, "print") {
			continue
		}
		if rMaxWidth.MatchString(r.Prelude) {
			maxWidth, err := strconv.Atoi(rMaxWidth.FindStringSubmatch(r.Prelude)[1])
			if err != nil {
				return nil, fmt.Errorf("atoi: %w", err)
			}
			if windowWidth > maxWidth {
				continue
			}
		}
		if rMinWidth.MatchString(r.Prelude) {
			minWidth, err := strconv.Atoi(rMinWidth.FindStringSubmatch(r.Prelude)[1])
			if err != nil {
				return nil, fmt.Errorf("atoi: %w", err)
			}
			if windowWidth < minWidth {
				continue
			}
		}
		for _, rr := range r.Rules {
			if err := processRule(m, rr); err != nil {
				return nil, fmt.Errorf("process embedded rule: %w", err)
			}
		}
	}
	return
}

func Inline(html string, csss ...string) (string, error) {
	revertCSS, err := ioutil.ReadFile("normalize.css")
	if err != nil {
		return "", fmt.Errorf("revert ffox css: %w", err)
	}
	csss = append([]string{string(revertCSS), AddOnCSS}, csss...)
	style := "<style>" + strings.Join(csss, "\n\n") + "</style>"

	var replaced string
	if strings.Contains(html, "</head>") {
		replaced = strings.Replace(html, "</head>", style+"\n</head>", 1)
	} else if strings.Contains(html, "<body") {
		replaced = strings.Replace(html, "<body", style+"\n<body", 1)
	} else {
		replaced = style + html
	}

	if replaced == html {
		panic("woot")
	}

	inlined, err := inliner.Inline(replaced)
	if err == nil {
		html = inlined
	} else {
		err = fmt.Errorf("inling failed: %w", err)
	}
	return html, err
}

type Map struct {
	Declarations map[string]css.Declaration
}

func NewMap(n *html.Node) Map {
	s := Map{
		Declarations: make(map[string]css.Declaration),
	}

	for _, a := range n.Attr {
		if a.Key == "style" {
			decls, err := parser.ParseDeclarations(a.Val)

			if err != nil {
				log.Printf("could not parse '%v'", a.Val)
				break
			}

			for _, d := range decls {
				s.Declarations[d.Property] = *d
			}
		} else if a.Key == "height" || a.Key == "width" {
			v := a.Val

			if !strings.HasSuffix(v, "%") {
				v += "px"
			}

			s.Declarations[a.Key] = css.Declaration{
				Property: a.Key,
				Value: v,
			}
		} else if a.Key == "bgcolor" {
			s.Declarations["background-color"] = css.Declaration{
				Property: "background-color",
				Value: a.Val,
			}
		}
	}

	return s
}

func (cs Map) ApplyChildStyle(ccs Map) (res Map) {
	res.Declarations = make(map[string]css.Declaration)

	for k, v := range cs.Declarations {
		res.Declarations[k] = v
	}
	// overwrite with higher prio child props
	for k, v := range ccs.Declarations {
		switch k {
		/*case "height", "width":
			parentL, ok := res.Declarations[k]
			if ok && strings.HasSuffix(v.Value, "%") && strings.HasSuffix(parentL.Value, "px") {
				parentLNum, err := strconv.Atoi(strings.TrimSuffix(parentL.Value, "px"))
				if err != nil {
					log.Errorf("atoi: %v", err)
					continue
				}
				percentNum, err := strconv.ParseFloat(strings.TrimSuffix(v.Value, "%"), 64)
				if err != nil {
					log.Errorf("atoi: %v", err)
					continue
				}
				prod := int(percentNum * float64(parentLNum) / 100.0)
				res.Declarations[k] = css.Declaration{
					Property: k,
					Value: fmt.Sprintf("%vpx", prod),
				}
				continue
			}
			fallthrough*/
		default:
			res.Declarations[k] = v
		}
	}

	return
}

func (cs Map) Font() *draw.Font {
	if !CssFonts {
		return nil
	}
	fn := cs.FontFilename()
	if dui == nil {
		return nil
	}
	font, ok := fontCache[fn]
	if ok {
		return font
	}
	font, err := dui.Display.OpenFont(fn)
	if err != nil {
		log.Printf("%v is not avail", fn)
		return nil
	}
	fontCache[fn] = font

	return font
}

func (cs Map) preferedFontName(preferences []string) string {
	avails := availableFontNames

	if len(avails) == 0 {
		return preferences[0]
	}

	for len(preferences) > 0 {
		var pref string
		pref, preferences = preferences[0], preferences[1:]

		for _, avail := range avails {
			if pref == strings.TrimSuffix(avail, "/") {
				return avail
			}
		}
	}

	return avails[0]
}

func (cs Map) FontSize() float64 {
	fs, ok := cs.Declarations["font-size"]
	if !ok || fs.Value == "" {
		return FontBaseSize
	}

	if len(fs.Value) <= 2 {
		log.Printf("error parsing font size %v", fs.Value)
		return FontBaseSize
	}
	numStr := fs.Value[0 : len(fs.Value)-2]
	f, err := strconv.ParseFloat(numStr, 64)
	if err != nil {
		log.Printf("error parsing font size %v", fs.Value)
		return FontBaseSize
	}
	if strings.HasSuffix(fs.Value, "em") {
		f *= FontBaseSize
	}
	return f
}

func (cs Map) Color() draw.Color {
	h, ok := cs.colorHex("color")
	if !ok {
		return draw.Black
	}
	c := draw.Color(h)
	return c
}

func (cs Map) colorHex(cssPropName string) (c draw.Color, ok bool) {
	propVal, ok := cs.Declarations[cssPropName]
	if ok {
		var r, g, b uint32
		if strings.HasPrefix(propVal.Value, "rgb") {
			val := propVal.Value[3:]
			val = strings.TrimPrefix(val, "(")
			val = strings.TrimSuffix(val, ")")
			vals := strings.Split(val, ",")
			rr, err := strconv.ParseInt(vals[0], 10, 32)
			if err != nil {
				goto default_value
			}
			gg, err := strconv.ParseInt(vals[1], 10, 32)
			if err != nil {
				goto default_value
			}
			bb, err := strconv.ParseInt(vals[2], 10, 32)
			if err != nil {
				goto default_value
			}
			r = uint32(rr) * 256
			g = uint32(gg) * 256
			b = uint32(bb) * 256
		} else if strings.HasPrefix(propVal.Value, "#") {
			hexColor := propVal.Value[1:]

			if len(hexColor) == 3 {
				rr, err := strconv.ParseInt(hexColor[0:1], 16, 32)
				if err != nil {
					goto default_value
				}
				gg, err := strconv.ParseInt(hexColor[1:2], 16, 32)
				if err != nil {
					goto default_value
				}
				bb, err := strconv.ParseInt(hexColor[2:3], 16, 32)
				if err != nil {
					goto default_value
				}
				r = uint32(rr) * 256 * 16
				g = uint32(gg) * 256 * 16
				b = uint32(bb) * 256 * 16
			} else if len(hexColor) == 6 {
				rr, err := strconv.ParseInt(hexColor[0:2], 16, 32)
				if err != nil {
					goto default_value
				}
				gg, err := strconv.ParseInt(hexColor[2:4], 16, 32)
				if err != nil {
					goto default_value
				}
				bb, err := strconv.ParseInt(hexColor[4:6], 16, 32)
				if err != nil {
					goto default_value
				}
				r = uint32(rr) * 256
				g = uint32(gg) * 256
				b = uint32(bb) * 256
			} else {
				goto default_value
			}
		} else if propVal.Value == "inherit" {
			// TODO: handle properly
			goto default_value
		} else {
			colorRGBA, ok := colornames.Map[propVal.Value]
			if !ok {
				goto default_value
			}
			r, g, b, _ = colorRGBA.RGBA()
		}

		x := (r / 256) << 24
		x = x | ((g / 256) << 16)
		x = x | ((b / 256) << 8)
		x = x | 0x000000ff

		return draw.Color(uint32(x)), true
	} else {
		return 0, false
	}
default_value:
	log.Printf("could not interpret %v", propVal)
	return 0, false
}

func (cs Map) IsInline() bool {
	propVal, ok := cs.Declarations["float"]
	if ok && propVal.Value == "left" {
		return true
	}
	propVal, ok = cs.Declarations["display"]
	if ok {
		return propVal.Value == "inline" ||
			propVal.Value == "inline-block"
	}
	return false
}

func (cs Map) IsDisplayNone() bool {
	propVal, ok := cs.Declarations["display"]
	if ok && propVal.Value == "none" {
		return true
	}
	/*propVal, ok = cs.Declarations["position"]
	if ok && propVal.Value == "fixed" {
		return true
	}*/
	propVal, ok = cs.Declarations["clip"]
	if ok && strings.ReplaceAll(propVal.Value, " ", "") == "rect(1px,1px,1px,1px)" {
		return true
	}
	propVal, ok = cs.Declarations["width"]
	if ok && propVal.Value == "1px" {
		propVal, ok = cs.Declarations["height"]
		if ok && propVal.Value == "1px" {
			return true
		}
	}
	return false
}

func (cs Map) IsFlex() bool {
	propVal, ok := cs.Declarations["display"]
	if ok {
		return propVal.Value == "flex"
	}
	return false
}

func (cs Map) IsFlexDirectionRow() bool {
	propVal, ok := cs.Declarations["flex-direction"]
	if ok {
		switch propVal.Value {
		case "row":
			return true
		case "column":
			return false
		}
	}
	return true // TODO: be more specific
}

func length(l string) (f float64, unit string, err error) {
	var s string

	if l == "auto" {
		return 0, "px", nil
	}

	for _, suffix := range []string{"px", "%", "rem", "em"} {
		if strings.HasSuffix(l, suffix) {
			s = strings.TrimSuffix(l, suffix)
			unit = suffix
			break
		}
	}

	switch unit {
	case "":
		return f, unit, fmt.Errorf("unknown suffix: %v", l)
	case "px", "em":
		f, err = strconv.ParseFloat(s, 64)
	}

	if unit == "em" {
		f *= FontBaseSize
	}

	f = float64(dui.Scale(int(f)))

	return
}

func (cs Map) Height() int {
	d, ok := cs.Declarations["height"]
	if ok {
		f, _, err := length(d.Value)
		if err != nil {
			log.Errorf("cannot parse height: %v", err)
		}
		return int(f)
	}
	return 0
}

func (cs Map) Width() int {
	d, ok := cs.Declarations["width"]
	if ok {
		f, _, err := length(d.Value)
		if err != nil {
			log.Errorf("cannot parse width: %v", err)
		}
		return int(f)
	}
	return 0
}

func (cs Map) Css(propName string) string {
	d, ok := cs.Declarations[propName]
	if !ok {
		return ""
	}
	return d.Value
}
