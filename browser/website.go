package browser

import (
	"github.com/mjl-/duit"
	"golang.org/x/net/html"
	"io/ioutil"
	"net/url"
	"github.com/psilva261/opossum"
	"github.com/psilva261/opossum/domino"
	"github.com/psilva261/opossum/nodes"
	"github.com/psilva261/opossum/style"
	"strings"
)

const (
	InitialLayout = iota
	ClickRelayout
)

type Website struct {
	duit.UI
	html      string
	d *domino.Domino
}

func (w *Website) layout(f opossum.Fetcher, layouting int) {
	pass := func(htm string, csss ...string) (*html.Node, map[*html.Node]style.Map) {

		if debugPrintHtml {
			log.Printf("%v\n", htm)
		}

		var doc *html.Node
		var err error
		doc, err = html.ParseWithOptions(
			strings.NewReader(htm),
			html.ParseOptionEnableScripting(*ExperimentalJsInsecure),
		)
		if err != nil {
			panic(err.Error())
		}

		log.Printf("Retrieving CSS Rules...")
		var cssSize int
		nodeMap := make(map[*html.Node]style.Map)
		for i, css := range csss {

			log.Printf("CSS size %v kB", cssSize/1024)

			nm, err := style.FetchNodeMap(doc, css, 1280)
			if err == nil {
				log.Printf("[%v/%v] Fetch CSS Rules successful!", i+1, len(csss))
				if debugPrintHtml {
					log.Printf("%v", nm)
				}
				style.MergeNodeMaps(nodeMap, nm)
			} else {
				log.Errorf("Fetch CSS Rules failed: %v", err)
				if *DebugDumpCSS {
					ioutil.WriteFile("info.css", []byte(css), 0644)
				}
			}
		}

		return doc, nodeMap
	}

	log.Printf("1st pass")
	doc, _ := pass(w.html)

	log.Printf("2nd pass")
	log.Printf("Download style...")
	cssHrefs := style.Hrefs(doc)
	csss := make([]string, 0, len(cssHrefs))
	for _, href := range cssHrefs {
		url, err := f.LinkedUrl(href)
		if err != nil {
			log.Printf("error parsing %v", href)
			continue
		}
		log.Printf("Download %v", url)
		buf, contentType, err := f.Get(url)
		if err != nil {
			log.Printf("error downloading %v", url)
			continue
		}
		if contentType.IsCSS() {
			csss = append(csss, string(buf))
		} else {
			log.Printf("css: unexpected %v", contentType)
		}
	}
	csss = append([]string{style.AddOnCSS}, csss...)
	doc, nodeMap := pass(w.html, csss...)

	// 3rd pass is only needed initially to load the scripts and set the goja VM
	// state. During subsequent calls from click handlers that state is kept.
	if *ExperimentalJsInsecure && layouting != ClickRelayout {
		log.Printf("3rd pass")
		nt := nodes.NewNodeTree(doc, style.Map{}, nodeMap, nil)
		jsSrcs := domino.Srcs(nt)
		downloads := make(map[string]string)
		for _, src := range jsSrcs {
			url, err := f.LinkedUrl(src)
			if err != nil {
				log.Printf("error parsing %v", src)
				continue
			}
			log.Printf("Download %v", url)
			buf, _, err := f.Get(url)
			if err != nil {
				log.Printf("error downloading %v", url)
				continue
			}
			downloads[src] = string(buf)
		}
		codes := domino.Scripts(nt, downloads)
		log.Infof("JS pipeline start")
		if w.d != nil {
			log.Infof("Stop existing JS instance")
			w.d.Stop()
		}
		w.d = domino.NewDomino(w.html, browser, nt)
		w.d.Start()
		jsProcessed, changed, err := processJS2(w.d, codes)
		if changed && err == nil {
			w.html = jsProcessed
			if debugPrintHtml {
				log.Printf("%v\n", jsProcessed)
			}
			doc, nodeMap = pass(w.html, csss...)
		} else if err != nil {
			log.Errorf("JS error: %v", err)
		}
		log.Infof("JS pipeline end")
	}
	var countHtmlNodes func(*html.Node) int
	countHtmlNodes = func(n *html.Node) (num int) {
		num++
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			num += countHtmlNodes(c)
		}
		return
	}
	log.Printf("%v html nodes found...", countHtmlNodes(doc))

	body := grep(doc, "body")

	log.Printf("Layout website...")
	scroller = NewScroll(
		NodeToBox(0, browser, nodes.NewNodeTree(body, style.Map{}, nodeMap, &nodes.Node{})),
	)
	numElements := 0
	TraverseTree(scroller, func(ui duit.UI) {
		numElements++
	})
	w.UI = scroller
	log.Printf("Layouting done (%v elements created)", numElements)
	if numElements < 10 {
		log.Errorf("Less than 10 elements layouted, seems css processing failed. Will layout without css")
		scroller = NewScroll(
			NodeToBox(0, browser, nodes.NewNodeTree(body, style.Map{}, make(map[*html.Node]style.Map), nil)),
		)
		w.UI = scroller
	}
	log.Flush()
}

func formData(n, submitBtn *html.Node) (data url.Values) {
	data = make(url.Values)
	nm := attr(*n, "name")

	switch n.Data {
	case "input", "select":
		if attr(*n, "type") == "submit" && n != submitBtn {
			return
		}
		if nm != "" {
			data.Set(nm, attr(*n, "value"))
		}
	case "textarea":
		nn := nodes.NewNodeTree(n, style.Map{}, make(map[*html.Node]style.Map), nil)

		if nm != "" {
			data.Set(nm, nodes.ContentFrom(*nn))
		}
	}

	for c := n.FirstChild; c != nil; c = c.NextSibling {
		for k, vs := range formData(c, submitBtn) {
			data.Set(k, vs[0]) // TODO: what aboot the rest?
		}
	}

	return
}

func (b *Browser) submit(form *html.Node, submitBtn *html.Node) {
	var err error
	var buf []byte
	var contentType opossum.ContentType

	method := "GET" // TODO
	if m := attr(*form, "method"); m != "" {
		method = strings.ToUpper(m)
	}
	uri := b.URL()
	if action := attr(*form, "action"); action != "" {
		uri, err = b.LinkedUrl(action)
		if err != nil {
			log.Printf("error parsing %v", action)
			return
		}
	}

	if method == "GET" {
		q := uri.Query()
		for k, vs := range formData(form, submitBtn) {
			q.Set(k, vs[0]) // TODO: what is with the rest?
		}
		uri.RawQuery = q.Encode()
		buf, contentType, err = b.get(uri, true)
	} else {
		buf, contentType, err = b.PostForm(uri, formData(form, submitBtn))
	}

	if err != nil {
		log.Errorf("submit form: %v", err)
		return
	}

	if !contentType.IsHTML() {
		log.Errorf("post: unexpected %v", contentType)
		return
	}

	b.render(buf)
}
