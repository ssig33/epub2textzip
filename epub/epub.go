package epub

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"path"
)

type Container struct {
	Rootfiles []Rootfile `xml:"rootfiles>rootfile"`
}

type Rootfile struct {
	FullPath string `xml:"full-path,attr"`
}

type Package struct {
	Metadata Metadata `xml:"metadata"`
	Manifest Manifest `xml:"manifest"`
	Spine    Spine    `xml:"spine"`
}

type Metadata struct {
	Title   []string `xml:"title"`
	Creator []string `xml:"creator"`
}

type Manifest struct {
	Items []ManifestItem `xml:"item"`
}

type ManifestItem struct {
	ID        string `xml:"id,attr"`
	Href      string `xml:"href,attr"`
	MediaType string `xml:"media-type,attr"`
}

type Spine struct {
	ItemRefs []SpineItemRef `xml:"itemref"`
}

type SpineItemRef struct {
	IDRef string `xml:"idref,attr"`
}

type EPUB struct {
	Files       map[string]*zip.File
	Package     Package
	OPFDir      string
	ManifestMap map[string]ManifestItem
	reader      *zip.ReadCloser
}

func Open(epubPath string) (*EPUB, error) {
	r, err := zip.OpenReader(epubPath)
	if err != nil {
		return nil, err
	}

	files := make(map[string]*zip.File)
	for _, f := range r.File {
		files[f.Name] = f
	}

	containerFile, ok := files["META-INF/container.xml"]
	if !ok {
		return nil, fmt.Errorf("container.xml not found")
	}
	var cont Container
	if err := readXML(containerFile, &cont); err != nil {
		return nil, err
	}
	if len(cont.Rootfiles) == 0 {
		return nil, fmt.Errorf("no rootfile in container.xml")
	}

	opfPath := cont.Rootfiles[0].FullPath
	opfFile, ok := files[opfPath]
	if !ok {
		return nil, fmt.Errorf("OPF file not found: %s", opfPath)
	}
	var pkg Package
	if err := readXML(opfFile, &pkg); err != nil {
		return nil, err
	}

	manifestMap := make(map[string]ManifestItem)
	for _, item := range pkg.Manifest.Items {
		manifestMap[item.ID] = item
	}

	return &EPUB{
		Files:       files,
		Package:     pkg,
		OPFDir:      path.Dir(opfPath),
		ManifestMap: manifestMap,
		reader:      r,
	}, nil
}

func (e *EPUB) Close() error {
	return e.reader.Close()
}

func (e *EPUB) SpineFiles() []*zip.File {
	var result []*zip.File
	for _, ref := range e.Package.Spine.ItemRefs {
		item, ok := e.ManifestMap[ref.IDRef]
		if !ok {
			continue
		}
		href := ResolvePath(e.OPFDir, item.Href)
		f, ok := e.Files[href]
		if !ok {
			continue
		}
		result = append(result, f)
	}
	return result
}

func (e *EPUB) SpineFilePaths() []string {
	var result []string
	for _, ref := range e.Package.Spine.ItemRefs {
		item, ok := e.ManifestMap[ref.IDRef]
		if !ok {
			continue
		}
		result = append(result, ResolvePath(e.OPFDir, item.Href))
	}
	return result
}

func ResolvePath(base, href string) string {
	if path.IsAbs(href) {
		return href[1:]
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
