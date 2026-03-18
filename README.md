# epub2textzip

A CLI tool that converts EPUB files into a ZIP archive containing plain text and images.

- Extracts text from XHTML in spine order
- Image references are preserved as `<IMG SRC="filename.jpg">`
- Page breaks are represented as `<PBR>`
- Output ZIP contains `content.txt` and image files

## Install

```
go install github.com/ssig33/epub2textzip@latest
```

## Usage

```
epub2textzip input.epub
epub2textzip book1.epub book2.epub book3.epub
```

Each input `*.epub` produces a corresponding `*.zip`.
