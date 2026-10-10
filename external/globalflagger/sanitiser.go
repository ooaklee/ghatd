package globalflagger

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// Sanitiser errors are distinct from storage errors: any sanitiser failure
// means the image was rejected, never stored.
var (
	// ErrSVGTooLarge reports input exceeding MaxSVGBytes.
	ErrSVGTooLarge = errors.New("globalflagger/svg-too-large")
	// ErrSVGMalformed reports XML that cannot be parsed strictly.
	ErrSVGMalformed = errors.New("globalflagger/svg-malformed")
	// ErrSVGUnsafe reports validated-but-disallowed SVG syntax (scripts,
	// foreign content, external references, unsafe styling, nesting).
	ErrSVGUnsafe = errors.New("globalflagger/svg-unsafe")
)

// svgLimits bound shape as well as size so pathological-but-small inputs
// cannot exhaust memory during validation.
const (
	maxSVGDepth       = 32 // element nesting depth
	maxSVGElements    = 5000
	maxSVGTextLength  = 1 << 20 // 1 MiB total decoded chardata
	maxAttributeChars = 20000   // any single attribute value (path data)
)

// allowedSVGElements is the closed set of vector-shape and paint-server
// elements accepted from flag-icons style artwork. Structural containers
// beyond the root (svg), text, scripts and foreign content are excluded.
var allowedSVGElements = map[string]bool{
	"path":           true,
	"g":              true,
	"circle":         true,
	"ellipse":        true,
	"rect":           true,
	"polygon":        true,
	"polyline":       true,
	"line":           true,
	"use":            true,
	"defs":           true,
	"clipPath":       true,
	"linearGradient": true,
	"radialGradient": true,
	"stop":           true,
	"mask":           true,
	"pattern":        true,
	"marker":         true,
	"title":          true,
}

// allowedSVGAttributes is the closed set of presentation and geometry
// attributes accepted, by element-agnostic name. Namespace declarations
// are handled separately on the root.
var allowedSVGAttributes = map[string]bool{
	// geometry
	"d": true, "x": true, "y": true, "width": true, "height": true,
	"cx": true, "cy": true, "r": true, "rx": true, "ry": true,
	"fx": true, "fy": true, "x1": true, "y1": true, "x2": true, "y2": true,
	"points": true, "offset": true, "viewBox": true,
	// paint
	"fill": true, "stroke": true, "stop-color": true, "opacity": true,
	"fill-opacity": true, "stroke-opacity": true, "stop-opacity": true,
	"fill-rule": true, "clip-rule": true,
	// stroking
	"stroke-width": true, "stroke-linecap": true, "stroke-linejoin": true,
	"stroke-miterlimit": true, "stroke-dasharray": true, "stroke-dashoffset": true,
	// transforms and referencing
	"transform": true, "gradientTransform": true, "gradientUnits": true,
	"clip-path": true, "mask": true, "xlink:href": true, "href": true,
	"marker-mid": true, "marker-start": true, "marker-end": true,
	"markerWidth": true, "markerHeight": true, "markerUnits": true,
	"orient": true, "refX": true, "refY": true,
	// paint-server structure
	"patternUnits": true, "patternContentUnits": true,
	"patternTransform": true, "spreadMethod": true,
	"clipPathUnits": true, "maskUnits": true, "maskContentUnits": true,
	"objectBoundingBox": true,
	// typography is only needed for font-hinted path artwork metadata in
	// the pinned seed set; values are validated as plain strings.
	"font-family": true, "font-size": true, "font-weight": true,
	"letter-spacing": true, "word-spacing": true, "text-anchor": true,
	// document-level only (validated separately): id, class, style, color,
	// xml:space, overflow, version, aria-label.
}

// attributesValidatedSeparately are name-space declarations and metadata
// attributes handled by dedicated checks rather than the generic allowlist.
var attributesValidatedSeparately = map[string]bool{
	"xmlns": true, "xmlns:xlink": true, "id": true, "class": true,
	"style": true, "color": true, "xml:space": true, "overflow": true,
	"version": true, "aria-label": true,
}

// urlRefPattern extracts the fragment of url(#...) references used by
// fill/stroke/clip-path/mask and gradient hrefs.
var urlRefPattern = regexp.MustCompile(`^url\(#([A-Za-z0-9_.:-]+)\)$`)

// cssDangerPattern rejects CSS constructs that can smuggle URLs, imports,
// or binding languages. Applied to the whole style value.
var cssDangerPattern = regexp.MustCompile(`(?i)(url\s*\(|@import|@charset|behavior\s*:|expression\s*\(|-moz-binding|javascript\s*:|data\s*:)`)

// colourValuePattern accepts hex colours (3/4/6/8 digits), CSS colour
// keywords, rgb()/rgba() with plain numbers/percentages, and internal
// fragment url() references. No named-function escape hatches.
var colourValuePattern = regexp.MustCompile(`^(` +
	`#[0-9a-fA-F]{3,4}|#[0-9a-fA-F]{6}|#[0-9a-fA-F]{8}` +
	`|url\(#([A-Za-z0-9_.:-]+)\)` +
	`|rgb(a)?\(\s*([0-9]{1,3}|[0-9.]+%)\s*,\s*([0-9]{1,3}|[0-9.]+%)\s*,\s*([0-9]{1,3}|[0-9.]+%)\s*(,\s*([0-9.]{1,4}%?|[01](\.[0-9]+)?)\s*)?\)` +
	`|[a-zA-Z]{3,20}` +
	`)$`)

// transformValuePattern accepts SVG transform lists with plain numeric
// arguments and internal fragment references.
var transformValuePattern = regexp.MustCompile(`^[a-zA-Z(),%.eE0-9\s+-]*$`)

// geometryValuePattern accepts numeric path data and points lists,
// including scientific notation and separators used by the pinned artwork.
var geometryValuePattern = regexp.MustCompile(`^[A-Za-z0-9,. eE+-]*$`)

// plainTokenPattern accepts simple identifier-ish values.
var plainTokenPattern = regexp.MustCompile(`^[A-Za-z0-9_.,:;%()\s-]*$`)

// numericPattern accepts plain numbers.
var numericPattern = regexp.MustCompile(`^-?[0-9]+(\.[0-9]+)?(e-?[0-9]+)?$`)

// fragmentIDPattern accepts safe internal fragment identifiers.
var fragmentIDPattern = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,128}$`)

// SanitiseSVG strictly validates raw SVG input and re-encodes only the
// validated safe tokens into canonical XML output.
//
// Rejected absolutely: scripts (script/foreignObject/animation/event
// attributes), DTD/entity declarations and processing instructions,
// external or non-fragment references (http, https, data, //, relative),
// nested svg elements, unknown elements/attributes, event-handler-looking
// attributes, unsafe inline CSS (url(), import, expression, behaviour
// bindings), non-ASCII attribute payloads (bidi/normalisation attacks),
// and excessive size, depth, element count or text volume.
//
// The output is a re-encoded document produced exclusively from validated
// tokens, never a copy of input bytes. Internal fragment references
// (url(#id), href="#id") are allowed only against identifiers actually
// declared inside the image, so dangling or forged references fail.
func SanitiseSVG(input []byte) ([]byte, error) {
	if len(input) == 0 {
		return nil, fmt.Errorf("%w: empty image", ErrSVGMalformed)
	}
	if len(input) > MaxSVGBytes {
		return nil, ErrSVGTooLarge
	}

	dec := xml.NewDecoder(bytes.NewReader(input))
	dec.Strict = true
	// No DTD: declarations are rejected outright below, so entity
	// expansion is impossible; only the built-in XML character references
	// remain, which the decoder resolves safely.

	san := &svgSanitiser{
		fragments:  map[string]bool{},
		referenced: map[string]bool{},
		depth:      0,
		textSeen:   0,
	}

	root, err := san.scan(dec)
	if err != nil {
		return nil, err
	}
	if root == nil {
		return nil, fmt.Errorf("%w: no root svg element", ErrSVGMalformed)
	}

	// Every internal reference must resolve to a declared fragment.
	for ref := range san.referenced {
		if !san.fragments[ref] {
			return nil, fmt.Errorf("%w: unresolved internal reference %q", ErrSVGUnsafe, ref)
		}
	}

	if err := validateReferenceGraph(root); err != nil {
		return nil, err
	}

	var out bytes.Buffer
	enc := xml.NewEncoder(&out)
	if err := san.encode(enc, root); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	if out.Len() > MaxSVGBytes {
		return nil, ErrSVGTooLarge
	}
	return out.Bytes(), nil
}

// svgNode is a validated node retained for re-encoding.
type svgNode struct {
	name     string
	attrs    []xml.Attr
	chardata strings.Builder
	children []*svgNode
}

// svgSanitiser carries validation state across the streaming scan.
type svgSanitiser struct {
	fragments  map[string]bool
	referenced map[string]bool
	depth      int
	maxDepth   int
	elements   int
	textSeen   int
}

// scan consumes the token stream and builds the validated tree.
func (s *svgSanitiser) scan(dec *xml.Decoder) (*svgNode, error) {
	var root *svgNode
	var stack []*svgNode

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrSVGMalformed, err)
		}

		switch t := tok.(type) {
		case xml.ProcInst:
			return nil, fmt.Errorf("%w: processing instruction not allowed", ErrSVGUnsafe)

		case xml.Directive:
			return nil, fmt.Errorf("%w: DTD or directive not allowed", ErrSVGUnsafe)

		case xml.Comment:
			continue // dropped

		case xml.CharData:
			n := len([]rune(string(t)))
			s.textSeen += n
			if s.textSeen > maxSVGTextLength {
				return nil, fmt.Errorf("%w: text volume exceeds limit", ErrSVGUnsafe)
			}
			// Only <title> carries meaningful chardata; whitespace elsewhere
			// is dropped during re-encode.
			if len(stack) > 0 && stack[len(stack)-1].name == "title" {
				if !isSafeText(string(t)) {
					return nil, fmt.Errorf("%w: unsafe text content", ErrSVGUnsafe)
				}
				stack[len(stack)-1].chardata.Write(t)
			}

		case xml.StartElement:
			s.elements++
			if s.elements > maxSVGElements {
				return nil, fmt.Errorf("%w: element count exceeds limit", ErrSVGUnsafe)
			}
			local := t.Name.Local
			if root != nil && len(stack) == 0 {
				return nil, fmt.Errorf("%w: multiple root elements", ErrSVGMalformed)
			}
			if root == nil {
				if local != "svg" || t.Name.Space != "http://www.w3.org/2000/svg" {
					return nil, fmt.Errorf("%w: root must be an svg element in the SVG namespace", ErrSVGUnsafe)
				}
			} else {
				if local == "svg" {
					return nil, fmt.Errorf("%w: nested svg element", ErrSVGUnsafe)
				}
				if !allowedSVGElements[local] {
					return nil, fmt.Errorf("%w: element %q is not allowed", ErrSVGUnsafe, local)
				}
				if t.Name.Space != "" && t.Name.Space != "http://www.w3.org/2000/svg" && t.Name.Space != "http://www.w3.org/1999/xlink" {
					return nil, fmt.Errorf("%w: element namespace %q is not allowed", ErrSVGUnsafe, t.Name.Space)
				}
			}

			s.depth++
			if s.depth > maxSVGDepth {
				return nil, fmt.Errorf("%w: nesting depth exceeds limit", ErrSVGUnsafe)
			}
			if s.depth > s.maxDepth {
				s.maxDepth = s.depth
			}

			node := &svgNode{name: local}
			seenAttributes := map[string]bool{}
			for _, attr := range t.Attr {
				attributeKey := attr.Name.Space + ":" + attr.Name.Local
				if seenAttributes[attributeKey] {
					return nil, fmt.Errorf("%w: duplicate attribute", ErrSVGMalformed)
				}
				seenAttributes[attributeKey] = true
				if attr.Name.Local == "style" && attr.Name.Space == "" {
					styles, err := s.styleAttributes(attr.Value)
					if err != nil {
						return nil, err
					}
					node.attrs = append(node.attrs, styles...)
					continue
				}
				kept, err := s.validateAttribute(local, attr)
				if err != nil {
					return nil, err
				}
				if kept != nil {
					node.attrs = append(node.attrs, *kept)
				}
			}

			// Canonicalise duplicate presentation properties with inline style priority.
			for _, attr := range t.Attr {
				if attr.Name.Local == "style" && attr.Name.Space == "" {
					styles, _ := s.styleAttributes(attr.Value)
					for _, style := range styles {
						for i := len(node.attrs) - 1; i >= 0; i-- {
							if node.attrs[i].Name == style.Name {
								node.attrs = append(node.attrs[:i], node.attrs[i+1:]...)
							}
						}
						node.attrs = append(node.attrs, style)
					}
				}
			}
			canonicalAttributes := map[xml.Name]bool{}
			for _, a := range node.attrs {
				if canonicalAttributes[a.Name] {
					return nil, fmt.Errorf("%w: duplicate canonical attribute", ErrSVGMalformed)
				}
				canonicalAttributes[a.Name] = true
			}
			if root == nil {
				root = node
			} else if len(stack) > 0 {
				parent := stack[len(stack)-1]
				parent.children = append(parent.children, node)
			}
			stack = append(stack, node)

		case xml.EndElement:
			s.depth--
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}

	if s.depth != 0 {
		return nil, fmt.Errorf("%w: unbalanced elements", ErrSVGMalformed)
	}
	return root, nil
}

// validateAttribute validates one attribute in context and returns the
// token to re-encode, or nil to drop it (e.g. class, aria-label).
func (s *svgSanitiser) validateAttribute(element string, attr xml.Attr) (*xml.Attr, error) {
	name := attr.Name.Local
	space := attr.Name.Space
	value := attr.Value

	// Namespace declarations: only SVG and xlink on the root.
	if name == "xmlns" || space == "xmlns" || strings.HasPrefix(name, "xmlns:") {
		if element != "svg" {
			return nil, fmt.Errorf("%w: namespace declaration outside root", ErrSVGUnsafe)
		}
		switch value {
		case "http://www.w3.org/2000/svg", "http://www.w3.org/1999/xlink":
			return nil, nil // output has a single canonical SVG namespace
		default:
			return nil, fmt.Errorf("%w: namespace %q is not allowed", ErrSVGUnsafe, value)
		}
	}

	// Reject namespaced attributes other than xlink:href/xml:space.
	if space != "" && space != "http://www.w3.org/1999/xlink" && space != "http://www.w3.org/XML/1998/namespace" {
		return nil, fmt.Errorf("%w: attribute namespace %q is not allowed", ErrSVGUnsafe, space)
	}
	if space == "http://www.w3.org/1999/xlink" && name != "href" {
		return nil, fmt.Errorf("%w: xlink attribute %q is not allowed", ErrSVGUnsafe, name)
	}

	if space == "http://www.w3.org/XML/1998/namespace" {
		if name != "space" {
			return nil, fmt.Errorf("%w: unexpected XML attribute", ErrSVGUnsafe)
		}
		name = "xml:space"
	}

	// Event handlers and anything on*-looking is rejected outright.
	if strings.HasPrefix(name, "on") && len(name) > 2 {
		return nil, fmt.Errorf("%w: event handler attribute %q", ErrSVGUnsafe, name)
	}

	if !isASCII(value) {
		return nil, fmt.Errorf("%w: non-ASCII attribute value on %q", ErrSVGUnsafe, name)
	}

	if attributesValidatedSeparately[name] {
		return s.validateSpecialAttribute(element, name, value)
	}

	if !allowedSVGAttributes[name] {
		return nil, fmt.Errorf("%w: attribute %q is not allowed", ErrSVGUnsafe, name)
	}

	if len(value) > maxAttributeChars {
		return nil, fmt.Errorf("%w: attribute %q exceeds length limit", ErrSVGUnsafe, name)
	}

	return s.validateValueAttribute(name, value)
}

// validateSpecialAttribute handles id/class/style/color/xml:space/overflow/
// version/aria-label with dedicated rules.
func (s *svgSanitiser) validateSpecialAttribute(element, name, value string) (*xml.Attr, error) {
	switch name {
	case "id":
		if !fragmentIDPattern.MatchString(value) {
			return nil, fmt.Errorf("%w: unsafe id value", ErrSVGUnsafe)
		}
		if _, dup := s.fragments[value]; dup {
			return nil, fmt.Errorf("%w: duplicate fragment id %q", ErrSVGUnsafe, value)
		}
		s.fragments[value] = true
		return &xml.Attr{Name: xml.Name{Local: "id"}, Value: value}, nil

	case "class":
		// Classes are dropped: with no stylesheet ever served they are
		// inert, and dropping them removes a whole attack surface.
		if !plainTokenPattern.MatchString(value) {
			return nil, fmt.Errorf("%w: unsafe class value", ErrSVGUnsafe)
		}
		return nil, nil

	case "color":
		if !colourValuePattern.MatchString(value) || strings.HasPrefix(value, "url(") {
			return nil, fmt.Errorf("%w: unsafe color value", ErrSVGUnsafe)
		}
		return &xml.Attr{Name: xml.Name{Local: "color"}, Value: value}, nil

	case "xml:space":
		if value != "preserve" {
			return nil, fmt.Errorf("%w: xml:space must be preserve", ErrSVGUnsafe)
		}
		return &xml.Attr{Name: xml.Name{Space: "http://www.w3.org/XML/1998/namespace", Local: "space"}, Value: value}, nil

	case "overflow":
		if value != "visible" && value != "hidden" {
			return nil, fmt.Errorf("%w: overflow value", ErrSVGUnsafe)
		}
		return &xml.Attr{Name: xml.Name{Local: "overflow"}, Value: value}, nil

	case "version":
		if value != "1.0" && value != "1.1" {
			return nil, fmt.Errorf("%w: svg version", ErrSVGUnsafe)
		}
		return &xml.Attr{Name: xml.Name{Local: "version"}, Value: value}, nil

	case "aria-label":
		if !isSafeText(value) {
			return nil, fmt.Errorf("%w: unsafe aria-label", ErrSVGUnsafe)
		}
		// Dropped from storage: accessible names belong to the serving
		// layer which owns the alt text contract.
		return nil, nil
	}
	return nil, fmt.Errorf("%w: attribute %q is not allowed", ErrSVGUnsafe, name)
}

// validateValueAttribute validates a value-bearing allowlisted attribute.
func (s *svgSanitiser) validateValueAttribute(name, value string) (*xml.Attr, error) {
	kept := xml.Attr{Name: xml.Name{Local: name}}

	switch name {
	case "href", "xlink:href":
		if !strings.HasPrefix(value, "#") {
			return nil, fmt.Errorf("%w: external or non-fragment href %q", ErrSVGUnsafe, redactRef(value))
		}
		ref := strings.TrimPrefix(value, "#")
		if !fragmentIDPattern.MatchString(ref) {
			return nil, fmt.Errorf("%w: unsafe fragment reference", ErrSVGUnsafe)
		}
		s.referenced[ref] = true
		if name == "xlink:href" {
			kept.Name.Space = "http://www.w3.org/1999/xlink"
		}
		kept.Value = value

	case "fill", "stroke", "stop-color":
		if err := s.validatePaint(value); err != nil {
			return nil, err
		}
		kept.Value = value

	case "clip-path", "mask", "marker-mid", "marker-start", "marker-end":
		m := urlRefPattern.FindStringSubmatch(value)
		if m == nil {
			return nil, fmt.Errorf("%w: %s must be url(#fragment)", ErrSVGUnsafe, name)
		}
		s.referenced[m[1]] = true
		kept.Value = value

	case "d", "points":
		if !geometryValuePattern.MatchString(value) {
			return nil, fmt.Errorf("%w: unsafe path data", ErrSVGUnsafe)
		}
		kept.Value = value

	case "transform", "gradientTransform", "patternTransform":
		if !transformValuePattern.MatchString(value) {
			return nil, fmt.Errorf("%w: unsafe transform", ErrSVGUnsafe)
		}
		kept.Value = value

	case "viewBox":
		if !numericPattern.MatchString(strings.TrimSpace(value)) && !viewBoxPattern.MatchString(strings.TrimSpace(value)) {
			return nil, fmt.Errorf("%w: unsafe viewBox", ErrSVGUnsafe)
		}
		kept.Value = value

	case "offset", "opacity", "fill-opacity", "stroke-opacity", "stop-opacity", "stroke-width",
		"stroke-miterlimit", "font-size", "font-weight", "letter-spacing",
		"word-spacing", "r", "rx", "ry", "cx", "cy", "fx", "fy", "x", "y",
		"width", "height", "x1", "y1", "x2", "y2", "markerWidth", "markerHeight",
		"refX", "refY":
		if !plainTokenPattern.MatchString(value) {
			return nil, fmt.Errorf("%w: unsafe numeric value on %q", ErrSVGUnsafe, name)
		}
		kept.Value = value

	case "fill-rule", "clip-rule", "stroke-linecap", "stroke-linejoin",
		"stroke-dasharray", "stroke-dashoffset", "gradientUnits",
		"patternUnits", "patternContentUnits", "spreadMethod",
		"clipPathUnits", "maskUnits", "maskContentUnits", "markerUnits",
		"orient", "text-anchor", "font-family", "objectBoundingBox":
		if !plainTokenPattern.MatchString(value) {
			return nil, fmt.Errorf("%w: unsafe token on %q", ErrSVGUnsafe, name)
		}
		kept.Value = value

	default:
		// Defensive: any future allowlist addition must wire a check.
		return nil, fmt.Errorf("%w: attribute %q lacks a validator", ErrSVGUnsafe, name)
	}

	return &kept, nil
}

// viewBoxPattern accepts four numeric components.
var viewBoxPattern = regexp.MustCompile(`^-?[0-9.]+(e-?[0-9]+)?(\s+-?[0-9.]+(e-?[0-9]+)?){3}$`)

// validatePaint accepts colours, none, currentColor and internal fragment
// url() references for fill/stroke/stop-color.
func (s *svgSanitiser) validatePaint(value string) error {
	switch value {
	case "none", "currentColor", "inherit", "transparent":
		return nil
	}
	if m := urlRefPattern.FindStringSubmatch(value); m != nil {
		s.referenced[m[1]] = true
		return nil
	}
	if colourValuePattern.MatchString(value) {
		return nil
	}
	return fmt.Errorf("%w: unsafe paint value", ErrSVGUnsafe)
}

// isSafeText accepts plain ASCII/Latin printable text for title and
// aria content.
func isSafeText(s string) bool {
	if !isASCII(s) {
		return false
	}
	for _, r := range s {
		if r < 0x20 && r != '\n' && r != '\t' && r != '\r' {
			return false
		}
	}
	return true
}

// isASCII reports whether s contains only printable ASCII characters plus
// newline, tab and carriage return.
func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] > 0x7e || s[i] < 0x20 && s[i] != '\n' && s[i] != '\t' && s[i] != '\r' {
			return false
		}
	}
	return true
}

// redactRef avoids logging potentially malicious reference payloads.
func redactRef(v string) string {
	if len(v) > 32 {
		return v[:32] + "..."
	}
	return v
}

// encode re-encodes the validated tree. Only tokens that survived
// validation appear in the output.
func (s *svgSanitiser) encode(enc *xml.Encoder, node *svgNode) error {
	attrs := node.attrs
	if node.name == "svg" {
		attrs = append([]xml.Attr{{Name: xml.Name{Local: "xmlns"}, Value: "http://www.w3.org/2000/svg"}}, attrs...)
	}
	start := xml.StartElement{Name: xml.Name{Local: node.name}, Attr: attrs}
	if err := enc.EncodeToken(start); err != nil {
		return err
	}
	if node.chardata.Len() > 0 {
		if err := enc.EncodeToken(xml.CharData([]byte(node.chardata.String()))); err != nil {
			return err
		}
	}
	for _, child := range node.children {
		if err := s.encode(enc, child); err != nil {
			return err
		}
	}
	return enc.EncodeToken(xml.EndElement{Name: xml.Name{Local: node.name}})
}

// Promote accepted inline paint to presentation attributes, so serving the SVG
// needs no style CSP exception. Font/editor hints on path outlines are inert.
func (s *svgSanitiser) styleAttributes(value string) ([]xml.Attr, error) {
	if len(value) > 4096 || !isASCII(value) || cssDangerPattern.MatchString(value) || strings.ContainsAny(value, "\\{}<>@") {
		return nil, ErrSVGUnsafe
	}
	var result []xml.Attr
	for _, declaration := range strings.Split(value, ";") {
		declaration = strings.TrimSpace(declaration)
		if declaration == "" {
			continue
		}
		prop, content, ok := strings.Cut(declaration, ":")
		prop = strings.TrimSpace(prop)
		content = strings.TrimSpace(content)
		if !ok || content == "" {
			return nil, ErrSVGUnsafe
		}
		switch prop {
		case "-inkscape-font-specification":
			content = strings.Trim(content, "\"'")
			if !regexp.MustCompile(`^[A-Za-z0-9 -]{1,100}$`).MatchString(content) {
				return nil, ErrSVGUnsafe
			}
			continue
		case "-inkscape-stroke", "line-height", "text-indent", "text-align", "text-decoration-line", "text-transform", "marker":
			if !plainTokenPattern.MatchString(content) {
				return nil, ErrSVGUnsafe
			}
			continue
		}
		if !allowedSVGAttributes[prop] {
			return nil, ErrSVGUnsafe
		}
		attr, err := s.validateValueAttribute(prop, content)
		if err != nil {
			return nil, err
		}
		result = append(result, *attr)
	}
	return result, nil
}

// Internal references are a graph: a tiny input must not expand recursively
// or exponentially when the browser renders <use>, gradients or clip paths.
func validateReferenceGraph(root *svgNode) error {
	ids := map[string]*svgNode{}
	var collect func(*svgNode)
	collect = func(n *svgNode) {
		for _, a := range n.attrs {
			if a.Name.Local == "id" {
				ids[a.Value] = n
			}
		}
		for _, c := range n.children {
			collect(c)
		}
	}
	collect(root)
	state := map[*svgNode]int{}
	costs := map[*svgNode]int{}
	var visit func(*svgNode) (int, error)
	visit = func(n *svgNode) (int, error) {
		if state[n] == 1 {
			return 0, ErrSVGUnsafe
		}
		if state[n] == 2 {
			return costs[n], nil
		}
		state[n] = 1
		cost := 1
		edges := append([]*svgNode(nil), n.children...)
		for _, a := range n.attrs {
			ref := ""
			if a.Name.Local == "href" {
				ref = strings.TrimPrefix(a.Value, "#")
			} else if match := urlRefPattern.FindStringSubmatch(a.Value); match != nil {
				ref = match[1]
			}
			if ref != "" {
				target := ids[ref]
				if target == nil {
					return 0, ErrSVGUnsafe
				}
				edges = append(edges, target)
			}
		}
		for _, child := range edges {
			count, err := visit(child)
			if err != nil {
				return 0, err
			}
			cost += count
			if cost > 20000 {
				return 0, ErrSVGUnsafe
			}
		}
		state[n] = 2
		costs[n] = cost
		return cost, nil
	}
	_, err := visit(root)
	return err
}
