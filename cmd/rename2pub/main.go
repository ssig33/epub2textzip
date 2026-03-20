package main

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type container struct {
	Rootfiles []rootfile `xml:"rootfiles>rootfile"`
}

type rootfile struct {
	FullPath string `xml:"full-path,attr"`
}

type opfPackage struct {
	Metadata metadata `xml:"metadata"`
}

type metadata struct {
	Title   []string `xml:"title"`
	Creator []string `xml:"creator"`
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <file.epub> [file2.epub ...]\n", os.Args[0])
		os.Exit(1)
	}

	hasError := false
	for _, epubPath := range os.Args[1:] {
		if err := rename(epubPath); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %s: %v\n", epubPath, err)
			hasError = true
		}
	}
	if hasError {
		os.Exit(1)
	}
}

func rename(epubPath string) error {
	r, err := zip.OpenReader(epubPath)
	if err != nil {
		return fmt.Errorf("failed to open epub: %w", err)
	}
	defer r.Close()

	files := make(map[string]*zip.File)
	for _, f := range r.File {
		files[f.Name] = f
	}

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

	opfFile, ok := files[cont.Rootfiles[0].FullPath]
	if !ok {
		return fmt.Errorf("OPF file not found")
	}
	var pkg opfPackage
	if err := readXML(opfFile, &pkg); err != nil {
		return fmt.Errorf("failed to parse OPF: %w", err)
	}

	title := ""
	if len(pkg.Metadata.Title) > 0 {
		title = pkg.Metadata.Title[0]
	}
	author := ""
	if len(pkg.Metadata.Creator) > 0 {
		author = pkg.Metadata.Creator[0]
	}

	if title == "" {
		return fmt.Errorf("no title found in metadata")
	}

	// Sanitize for filename
	title = sanitize(title)
	author = strings.ReplaceAll(sanitize(author), " ", "")

	var newName string
	if author != "" {
		newName = fmt.Sprintf("[%s]%s.epub", author, title)
	} else {
		newName = fmt.Sprintf("%s.epub", title)
	}

	dir := filepath.Dir(epubPath)
	newPath := filepath.Join(dir, newName)

	if epubPath == newPath {
		fmt.Printf("Already named: %s\n", newPath)
		return nil
	}

	if err := os.Rename(epubPath, newPath); err != nil {
		return fmt.Errorf("failed to rename: %w", err)
	}

	fmt.Printf("%s -> %s\n", epubPath, newPath)
	return nil
}

func sanitize(s string) string {
	s = strings.TrimSpace(s)
	replacer := strings.NewReplacer(
		"/", "\uff0f",
		"\\", "\uff3c",
		":", "\uff1a",
		"*", "\uff0a",
		"?", "\uff1f",
		"\"", "\u201d",
		"<", "\uff1c",
		">", "\uff1e",
		"|", "\uff5c",
	)
	return replacer.Replace(s)
}

func readXML(f *zip.File, v interface{}) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	return xml.NewDecoder(rc).Decode(v)
}
