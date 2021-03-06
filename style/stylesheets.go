package style

import (
	"9fans.net/go/draw"
	"fmt"
	"github.com/andybalholm/cascadia"
	"github.com/chris-ramon/douceur/css"
	"github.com/chris-ramon/douceur/parser"
	"github.com/mjl-/duit"
	"golang.org/x/image/colornames"
	"golang.org/x/net/html"
	"github.com/psilva261/opossum/logger"
	"math"
	"regexp"
	"strconv"
	"strings"
)

var fontCache = make(map[string]*draw.Font)

var dui *duit.DUI
var availableFontNames []string

var rMinWidth = regexp.MustCompile(`min-width: (\d+)(px|em|rem)`)
var rMaxWidth = regexp.MustCompile(`max-width: (\d+)(px|em|rem)`)

const FontBaseSize = 11.0
var WindowWidth = 1280
var WindowHeight = 1080

const AddOnCSS = `
/* https://developer.mozilla.org/en-US/docs/Web/HTML/Inline_elements */
a, abbr, acronym, audio, b, bdi, bdo, big, br, button, canvas, cite, code, data, datalist, del, dfn, em, embed, i, iframe, img, input, ins, kbd, label, map, mark, meter, noscript, object, output, picture, progress, q, ruby, s, samp, script, select, slot, small, span, strong, sub, sup, svg, template, textarea, time, u, tt, var, video, wbr {
  display: inline;
}

/* non-HTML5 elements: https://www.w3schools.com/tags/ref_byfunc.asp */
font, strike, tt {
  display: inline;
}

button, textarea, input, select {
  display: inline-block;
}

/* https://developer.mozilla.org/en-US/docs/Web/HTML/Block-level_elements */
address, article, aside, blockquote, details, dialog, dd, div, dl, dt, fieldset, figcaption, figure, footer, form, h1, h2, h3, h4, h5, h6, header, hgroup, hr, li, main, nav, ol, p, pre, section, table, ul {
  display: block;
}

a {
  color: blue;
  margin-right: 2px;
}
`

func Init(d *duit.DUI) {
	dui = d

	initFontserver()
}

func Hrefs(doc *html.Node) (hrefs []string) {
	hrefs = make([]string, 0, 3)

	var f func(n *html.Node)
	f = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "link" {
			isStylesheet := false
			isPrint := false
			href := ""

			for _, a := range n.Attr {
				switch strings.ToLower(a.Key) {
				case "rel":
					if a.Val == "stylesheet" {
						isStylesheet = true
					}
				case "href":
					href = a.Val
				case "media":
					isPrint = a.Val == "print"
				}
			}

			if isStylesheet && !isPrint {
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

		m[n] = initial.ApplyChildStyle(mp, true)
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
		m[n] = Map{Declarations: ds}
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
			cs, err := cascadia.Compile(sel.Value)
			if err != nil {
				log.Printf("cssSel compile %v: %v", sel.Value, err)
				continue
			}
			for _, el := range cascadia.QueryAll(doc, cs) {
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
			m := rMaxWidth.FindStringSubmatch(r.Prelude)
			l := m[1]+m[2]
			maxWidth, _, err := length(nil, l)
			if err != nil {
				return nil, fmt.Errorf("atoi: %w", err)
			}
			if float64(windowWidth) > maxWidth {
				continue
			}
		}
		if rMinWidth.MatchString(r.Prelude) {
			m := rMinWidth.FindStringSubmatch(r.Prelude)
			l := m[1]+m[2]
			minWidth, _, err := length(nil, l)
			if err != nil {
				return nil, fmt.Errorf("atoi: %w", err)
			}
			if float64(windowWidth) < minWidth {
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

type DomTree interface {
	Parent() (p DomTree, ok bool)
	Style()  Map
}

type Map struct {
	Declarations map[string]css.Declaration
	DomTree      `json:"-"`
}

func NewMap(n *html.Node) Map {
	s := Map{
		Declarations: make(map[string]css.Declaration),
	}

	for _, a := range n.Attr {
		if a.Key == "style" {
			v := strings.TrimSpace(a.Val)
			if !strings.HasSuffix(v, ";") {
				v += ";"
			}
			decls, err := parser.ParseDeclarations(v)

			if err != nil {
				log.Printf("could not parse '%v'", a.Val)
				break
			}

			for _, d := range decls {
				s.Declarations[d.Property] = *d
			}
		} else if a.Key == "height" || a.Key == "width" {
			v := a.Val

			if !strings.HasSuffix(v, "%") && !strings.HasSuffix(v, "px") {
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

func (cs Map) ApplyChildStyle(ccs Map, copyAll bool) (res Map) {
	res.Declarations = make(map[string]css.Declaration)

	for k, v := range cs.Declarations {
		switch k {
		// https://www.w3.org/TR/CSS21/propidx.html
		case "azimuth", "border-collapse", "border-spacing", "caption-side", "color", "cursor", "direction", "elevation", "empty-cells", "font-family", "font-size", "font-style", "font-variant", "font-weight", "font", "letter-spacing", "line-height", "list-style-image", "list-style-position", "list-style-type", "list-style", "orphans", "pitch-range", "pitch", "quotes", "richness", "speak-header", "speak-numeral", "speak-punctuation", "speak", "speech-rate", "stress", "text-align", "text-indent", "text-transform", "visibility", "voice-family", "volume", "white-space", "widows", "word-spacing":
		default:
			if !copyAll {
				continue
			}
		}
		res.Declarations[k] = v
	}
	// overwrite with higher prio child props
	for k, v := range ccs.Declarations {
		if v.Value == "inherit" {
			continue
		}
		res.Declarations[k] = v
	}

	return
}

func (cs Map) Font() *draw.Font {
	fn, ok := cs.FontFilename()
	if !ok || dui == nil {
		return nil
	}
	font, ok := fontCache[fn]
	if ok {
		return font
	}
	log.Infof("call dui.Display.OpenFont(%v)", fn)
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

func matchClosestFontSize(desired float64, available []int) (closest int) {
	for _, a := range available {
		if closest == 0 || math.Abs(float64(a)-desired) < math.Abs(float64(closest)-desired) {
			closest = a
		}
	}
	return
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

// FontHeight in lowDPI pixels.
func (cs Map) FontHeight() float64 {
	return float64(cs.Font().Height) / float64(dui.Scale(1))
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
				r = uint32(rr) * 256 * 0x11
				g = uint32(gg) * 256 * 0x11
				b = uint32(bb) * 256 * 0x11
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

// tlbr parses 4-tuple of top-right-bottom-left like in margin,
// margin-top, ...-right, ...-bottom, ...-left.
func (cs *Map) Tlbr(key string) (s duit.Space, err error) {
	if all, ok := cs.Declarations[key]; ok {
		parts := strings.Split(all.Value, " ")
		nums := make([]int, len(parts))
		for i, p := range parts {
			if f, _, err := length(cs, p); err == nil {
				nums[i] = int(f)
			} else {
				return s, fmt.Errorf("length: %w", err)
			}
		}
		s.Top = nums[0]
		s.Right = s.Top
		s.Bottom = s.Top
		s.Left = s.Top
		if len(nums) >= 2 {
			s.Right = nums[1]
			s.Left = s.Right
		}
		if len(nums) >= 3 {
			s.Bottom = nums[2]
		}
		if len(nums) == 4 {
			s.Left = nums[3]
		}
	}

	if t, err := cs.CssPx(key+"-top"); err == nil {
		s.Top = t
	}
	if r, err := cs.CssPx(key+"-right"); err == nil {
		s.Right = r
	}
	if b, err := cs.CssPx(key+"-bottom"); err == nil {
		s.Bottom = b
	}
	if l, err := cs.CssPx(key+"-left"); err == nil {
		s.Left = l
	}

	return
}

func length(cs *Map, l string) (f float64, unit string, err error) {
	var s string

	if l == "auto" || l == "inherit" || l == "0" {
		return 0, "px", nil
	}

	for _, suffix := range []string{"px", "%", "rem", "em", "vw", "vh"} {
		if strings.HasSuffix(l, suffix) {
			if s = strings.TrimSuffix(l, suffix); s != "" {
				f, err = strconv.ParseFloat(s, 64)
				if err != nil {
					return 0, "", fmt.Errorf("error parsing '%v': %w", l, err)
				}
			}
			unit = suffix
			break
		}
	}

	switch unit {
	case "px":
	case "em", "rem":
		// TODO: distinguish between em and rem
		f *= FontBaseSize
	case "vw":
		f *= float64(WindowWidth) / 100.0
	case "vh":
		f *= float64(WindowHeight) / 100.0
	case "%":
		if cs == nil {
			return 0.0, "%", nil
		}
		var wp int
		if p, ok := cs.DomTree.Parent(); ok {
			wp = p.Style().baseWidth()
		} else {
			log.Printf("%% unit used in root element")
		}
		f *= 0.01 * float64(wp)
	default:
		return f, unit, fmt.Errorf("unknown suffix: %v", l)
	}

	return
}

func (cs *Map) Height() int {
	d, ok := cs.Declarations["height"]
	if ok {
		f, _, err := length(cs, d.Value)
		if err != nil {
			log.Errorf("cannot parse height: %v", err)
		}
		return int(f)
	}
	return 0
}

func (cs Map) Width() int {
	w := cs.width()
	if w > 0 {
		if d, ok := cs.Declarations["max-width"]; ok {
			f, _, err := length(&cs, d.Value)
			if err != nil {
				log.Errorf("cannot parse width: %v", err)
			}
			if mw := int(f); 0 < mw && mw < w {
				return int(mw)
			}
		}
	}
	return w
}

func (cs Map) width() int {
	d, ok := cs.Declarations["width"]
	if ok {
		f, _, err := length(&cs, d.Value)
		if err != nil {
			log.Errorf("cannot parse width: %v", err)
		}
		if f > 0 {
			return int(f)
		}
	}
	if _, ok := cs.DomTree.Parent(); !ok {
		return WindowWidth
	}
	return 0
}

// baseWidth to calculate relative widths
func (cs Map) baseWidth() int {
	if w := cs.Width(); w != 0 {
		return w
	}
	if p, ok := cs.DomTree.Parent(); !ok {
		return WindowWidth
	} else {
		return p.Style().baseWidth()
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

func (cs *Map) CssPx(propName string) (l int, err error) {
	d, ok := cs.Declarations[propName]
	if !ok {
		return 0, fmt.Errorf("property doesn't exist")
	}
	f, _, err := length(cs, d.Value)
	if err != nil {
		return 0, err
	}
	l = int(f)
	return
}

func (cs Map) SetCss(k, v string) {
	cs.Declarations[k] = css.Declaration{
		Property: k,
		Value: v,
	}
}
