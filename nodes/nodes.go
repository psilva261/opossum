package nodes

import (
	"bytes"
	"fmt"
	"golang.org/x/net/html"
	"github.com/chris-ramon/douceur/css"
	"github.com/psilva261/opossum/logger"
	"github.com/psilva261/opossum/style"
	"strings"
)

// Node represents a node at the render stage. It
// represents a subTree or just a single html node.
type Node struct {
	DomSubtree *html.Node `json:"-"`
	Text string
	Wrappable bool
	Attrs []html.Attribute
	style.Map
	Children []*Node
	parent *Node `json:"-"`
}

// NewNodeTree propagates the cascading styles to the leaves
//
// First applies the parent style and at the end the local style attribute's style is attached.
func NewNodeTree(doc *html.Node, ps style.Map, nodeMap map[*html.Node]style.Map, parent *Node) (n *Node) {
	ncs := style.Map{
		Declarations: make(map[string]css.Declaration),
	}
	ncs = ps.ApplyChildStyle(ncs, false)

	// add from matching selectors
	// (keep only inheriting properties from parent node)
	if m, ok := nodeMap[doc]; ok {
		ncs = ncs.ApplyChildStyle(m, false)
	}

	// add style attribute
	// (keep all properties that already match)
	styleAttr := style.NewMap(doc)
	ncs = ncs.ApplyChildStyle(styleAttr, true)

	data := doc.Data
	if doc.Type == html.ElementNode {
		data = strings.ToLower(data)
	}
	n = &Node{
		DomSubtree: doc,
		Attrs:      doc.Attr,
		Map:        ncs,
		Children:   make([]*Node, 0, 2),
		parent:     parent,
	}
	n.Wrappable = doc.Type == html.TextNode || doc.Data == "span" // TODO: probably this list needs to be extended
	if doc.Type == html.TextNode {
		n.Text = filterText(doc.Data)
		n.Map.Declarations["display"] = css.Declaration{
			Property: "display",
			Value: "inline",
		}
	}
	i := 0
	for c := doc.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != html.CommentNode {
			cnt := NewNodeTree(c, ncs, nodeMap, n)
			n.Children = append(n.Children, cnt)
			i++
		}
	}
	n.Map.DomTree = n

	return
}

// filterText removes line break runes (TODO: add this later but handle properly) and maps runes to canonical widths
func filterText(t string) string {
	t = strings.ReplaceAll(t, "­", "")
	t = strings.ReplaceAll(t, "\t", "    ")
	return t
}

func (n Node) Type() html.NodeType {
	return n.DomSubtree.Type
}

func (n Node) Data() string {
	if n.DomSubtree == nil {
		return ""
	}
	return n.DomSubtree.Data
}

func (n *Node) Parent() (p style.DomTree, ok bool) {
	ok = n.parent != nil && n.Data() != "html" && n.Data() != "body"
	if n.parent == nil && n.Data() != "html" && n.Data() != "body" {
		log.Errorf("n.Data() = %v but n.parent=nil", n.parent)
	}
	if ok {
		p = n.parent
	}
	return
}

func (n *Node) Style() style.Map {
	return n.Map
}

// Ancestor of tag
func (n *Node) Ancestor(tag string) *Node {
	if n.DomSubtree == nil {
		return nil
	}
	log.Printf("<%v>.ParentForm()", n.DomSubtree.Data)
	if n.DomSubtree.Data == tag {
		log.Printf("  I'm a %v :-)", tag)
		return n
	}
	if n.parent != nil {
		log.Printf("  go to my parent")
		return n.parent.Ancestor(tag)
	}
	return nil
}

func (n *Node) Find(tag string) (c *Node) {
	for _, cc := range n.Children {
		if cc.Data() == tag {
			return cc
		} else if f := cc.Find(tag); f != nil {
			return f
		}
	}

	return
}

func (n *Node) FindAll(tag string) (cs []*Node) {
	for _, cc := range n.Children {
		if cc.Data() == tag {
			cs = append(cs, cc)
		} else {
			cs = append(cs, cc.FindAll(tag)...)
		}
	}

	return
}

func (n *Node) Attr(k string) string {
	for _, a := range n.Attrs {
		if a.Key == k {
			return a.Val
		}
	}
	return ""
}

func (n *Node) HasAttr(k string) bool {
	for _, a := range n.Attrs {
		if a.Key == k {
			return true
		}
	}
	return false
}

// QueryRef relative to html > body
func (n *Node) QueryRef() string {
	nRef, ok := n.queryRef()
	if ok && strings.Contains(nRef, "#") {
		return nRef
	}

	path := make([]string, 0, 5)
	if n.Type() != html.TextNode {
		if ok {
			path = append(path, nRef)
		}
	}
	for p := n.parent; p != nil; p = p.parent {
		if part := p.Data(); part != "html" && part != "body" {
			if pRef, ok := p.queryRef(); ok {
				path = append([]string{pRef}, path...)
				if strings.Contains(pRef, "#") {
					break
				}
			}
		}
	}
	return strings.TrimSpace(strings.Join(path, " > "))
}

func (n *Node) queryRef() (ref string, ok bool) {
	if n.DomSubtree == nil || n.Type() != html.ElementNode {
		return
	}

	if id := n.Attr("id"); id != "" {
		// https://stackoverflow.com/questions/605630/how-to-select-html-nodes-by-id-with-jquery-when-the-id-contains-a-dot
		id = strings.ReplaceAll(id, `.`, `\\.`)

		return "#" + id, true
	}

	ref = n.Data()

	if n.parent == nil {
		return ref, true
	}

	i := 1
	for _, c := range n.parent.Children {
		if c == n {
			break
		}
		if c.Type() == html.ElementNode {
			i++
		}
	}
	ref += fmt.Sprintf(":nth-child(%v)", i)

	return ref, true
}

func IsPureTextContent(n Node) bool {
	if n.Text != "" {
		return true
	}
	for _, c := range n.Children {
		if c.Text == "" {
			return false
		}
	}
	return true
}

func (n Node) Content(pre bool) []string {
	content := make([]string, 0, len(n.Children))

	if n.Text != "" && n.Type() == html.TextNode && !n.Map.IsDisplayNone() {
		t := n.Text
		if !pre {
			t = strings.TrimSpace(t)
		}
		if t != "" {
			content = append(content, t)
		}
	}

	for _, c := range n.Children {
		if !c.Map.IsDisplayNone() {
			content = append(content, c.Content(pre)...)
		}
	}

	return content
}

func (n Node) ContentString(pre bool) (t string) {
	ts := n.Content(pre)
	if pre {
		t = strings.Join(ts, "")
	} else {
		t = strings.Join(ts, " ")
		t = strings.TrimSpace(t)
	}

	return
}

func (n *Node) Serialized() (string, error) {
	var b bytes.Buffer

	err := html.Render(&b, n.DomSubtree)

	return b.String(), err
}

// SetText by replacing child nodes with a TextNode containing t.
func (n *Node) SetText(t string) {
	d := n.DomSubtree

	if len(n.Children) != 1 || n.Type() != html.TextNode {
		for d.FirstChild != nil {
			d.RemoveChild(d.FirstChild)
		}

		c := &html.Node{
			Parent: d,
			Type: html.TextNode,
		}
		d.FirstChild = c

		n.Children = []*Node{
			&Node{
				DomSubtree: c,
				Wrappable: true,
				parent: n,
			},
		}
	}

	n.Children[0].Text = t
	n.Children[0].DomSubtree.Data = t

	return
}
