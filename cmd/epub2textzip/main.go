package main

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"golang.org/x/net/html"
)

// container.xml
type container struct {
	Rootfiles []rootfile `xml:"rootfiles>rootfile"`
}

type rootfile struct {
	FullPath string `xml:"full-path,attr"`
}

// OPF package
type opfPackage struct {
	Manifest manifest `xml:"manifest"`
	Spine    spine    `xml:"spine"`
}

type manifest struct {
	Items []manifestItem `xml:"item"`
}

type manifestItem struct {
	ID        string `xml:"id,attr"`
	Href      string `xml:"href,attr"`
	MediaType string `xml:"media-type,attr"`
}

type spine struct {
	ItemRefs []spineItemRef `xml:"itemref"`
}

type spineItemRef struct {
	IDRef string `xml:"idref,attr"`
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <file.epub> [file2.epub ...]\n", os.Args[0])
		os.Exit(1)
	}

	hasError := false
	for _, epubPath := range os.Args[1:] {
		if err := convert(epubPath); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %s: %v\n", epubPath, err)
			hasError = true
		}
	}
	if hasError {
		os.Exit(1)
	}
}

func convert(epubPath string) error {
	r, err := zip.OpenReader(epubPath)
	if err != nil {
		return fmt.Errorf("failed to open epub: %w", err)
	}
	defer r.Close()

	files := make(map[string]*zip.File)
	for _, f := range r.File {
		files[f.Name] = f
	}

	// 1. Parse container.xml
	containerFile, ok := files["META-INF/container.xml"]
	if !ok {
		return fmt.Errorf("container.xml not found")
	}
	var cont container
	if err := readXML(containerFile, &cont); err != nil {
		return fmt.Errorf("failed to parse container.xml: %w", err)
	}
	if len(cont.Rootfiles) == 0 {
		return fmt.Errorf("no rootfile in container.xml")
	}
	opfPath := cont.Rootfiles[0].FullPath
	opfDir := path.Dir(opfPath)

	// 2. Parse OPF
	opfFile, ok := files[opfPath]
	if !ok {
		return fmt.Errorf("OPF file not found: %s", opfPath)
	}
	var pkg opfPackage
	if err := readXML(opfFile, &pkg); err != nil {
		return fmt.Errorf("failed to parse OPF: %w", err)
	}

	// Build manifest map (id -> item)
	manifestMap := make(map[string]manifestItem)
	for _, item := range pkg.Manifest.Items {
		manifestMap[item.ID] = item
	}

	// 3. Process spine order, extract text and collect images
	var textBuilder strings.Builder
	imageSet := make(map[string]bool)

	for i, ref := range pkg.Spine.ItemRefs {
		item, ok := manifestMap[ref.IDRef]
		if !ok {
			continue
		}
		href := resolvePath(opfDir, item.Href)
		f, ok := files[href]
		if !ok {
			continue
		}
		text, images, err := extractText(f, opfDir, href)
		if err != nil {
			continue
		}
		if i > 0 {
			textBuilder.WriteString("<PBR>\n")
		}
		textBuilder.WriteString(text)
		for _, img := range images {
			imageSet[img] = true
		}
	}

	// 4. Write output zip
	outPath := strings.TrimSuffix(epubPath, filepath.Ext(epubPath)) + ".zip"
	outFile, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("failed to create output: %w", err)
	}
	defer outFile.Close()

	w := zip.NewWriter(outFile)
	defer w.Close()

	// Write text file
	baseName := strings.TrimSuffix(filepath.Base(epubPath), filepath.Ext(epubPath))
	tw, err := w.Create(baseName + ".txt")
	if err != nil {
		return err
	}
	if _, err := tw.Write([]byte(textBuilder.String())); err != nil {
		return err
	}

	// Write images
	for imgPath := range imageSet {
		f, ok := files[imgPath]
		if !ok {
			continue
		}
		iw, err := w.Create(path.Base(imgPath))
		if err != nil {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			continue
		}
		io.Copy(iw, rc)
		rc.Close()
	}

	fmt.Printf("Created: %s\n", outPath)
	return nil
}

func extractText(f *zip.File, opfDir, filePath string) (string, []string, error) {
	rc, err := f.Open()
	if err != nil {
		return "", nil, err
	}
	defer rc.Close()

	doc, err := html.Parse(rc)
	if err != nil {
		return "", nil, err
	}

	var b strings.Builder
	var images []string
	fileDir := path.Dir(filePath)

	var walk func(*html.Node)
	walk = func(n *html.Node) {
		switch n.Type {
		case html.ElementNode:
			switch n.Data {
			case "img", "image":
				src := getAttr(n, "src")
				if src == "" {
					src = getAttr(n, "xlink:href")
				}
				if src == "" {
					src = getAttr(n, "href")
				}
				if src != "" {
					resolved := resolvePath(fileDir, src)
					baseName := path.Base(resolved)
					b.WriteString(fmt.Sprintf("<IMG SRC=\"%s\">\n", baseName))
					images = append(images, resolved)
				}
			case "br":
				b.WriteString("\n")
			case "ruby":
				base, rt := extractRuby(n)
				if rt != "" {
					b.WriteString(base + "\u300a" + rt + "\u300b")
				} else {
					b.WriteString(base)
				}
				return
			case "rt", "rp":
				// handled by ruby case
				return
			case "p", "div", "h1", "h2", "h3", "h4", "h5", "h6", "li", "blockquote", "tr":
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					walk(c)
				}
				b.WriteString("\n")
				return
			}
		case html.TextNode:
			text := strings.Trim(n.Data, " \t\n\r")
			if text != "" {
				b.WriteString(text)
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}

	// Find body
	var body *html.Node
	var findBody func(*html.Node)
	findBody = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "body" {
			body = n
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			findBody(c)
			if body != nil {
				return
			}
		}
	}
	findBody(doc)

	if body != nil {
		for c := body.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}

	return b.String(), images, nil
}

func extractRuby(n *html.Node) (string, string) {
	var base, rt strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.TextNode {
			base.WriteString(strings.Trim(c.Data, " \t\n\r"))
		} else if c.Type == html.ElementNode {
			switch c.Data {
			case "rt":
				for tc := c.FirstChild; tc != nil; tc = tc.NextSibling {
					if tc.Type == html.TextNode {
						rt.WriteString(strings.Trim(tc.Data, " \t\n\r"))
					}
				}
			case "rp":
				// skip
			default:
				// rb or other inline elements
				for tc := c.FirstChild; tc != nil; tc = tc.NextSibling {
					if tc.Type == html.TextNode {
						base.WriteString(strings.Trim(tc.Data, " \t\n\r"))
					}
				}
			}
		}
	}
	return base.String(), rt.String()
}

func getAttr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func resolvePath(base, href string) string {
	if path.IsAbs(href) {
		return href[1:] // remove leading /
	}
	return path.Clean(path.Join(base, href))
}

func readXML(f *zip.File, v interface{}) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	return xml.NewDecoder(rc).Decode(v)
}
