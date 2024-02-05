package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/progress"
	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/mod/semver"
)

type GoRepository struct {
	url        string
	client     *http.Client
	onProgress func(float64)
}

func (g *GoRepository) GetVersions(ctx context.Context) ([]Release, error) {
	var results []Release

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.url+"/?mode=json", nil)
	if err != nil {
		return results, err
	}

	resp, err := g.client.Do(req)
	if err != nil {
		return results, err
	}
	defer resp.Body.Close()

	if status := resp.StatusCode; status < 200 || status >= 300 {
		return results, fmt.Errorf("not valid response status")
	}

	err = json.NewDecoder(resp.Body).Decode(&results)
	if err != nil {
		return results, err
	}

	return results, nil
}

func (g *GoRepository) Download(ctx context.Context, dlFile File, outFile *os.File) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.url+"/"+dlFile.Filename, nil)
	if err != nil {
		return err
	}

	resp, err := g.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	downloaded := 0
	total := int(resp.ContentLength)
	if total == 0 {
		return errors.New("unable to calculate progress: ContentLength is 0")
	}
	buf := make([]byte, 32*1024)

	for {
		nr, errRead := resp.Body.Read(buf)
		if nr > 0 {
			nw, errWrite := outFile.Write(buf[0:nr])

			downloaded += nw
			g.onProgress(float64(downloaded) / float64(total))

			if errWrite != nil {
				return errWrite
			}
		}
		if errRead != nil {
			if errRead != io.EOF {
				return errRead
			}
			break
		}
	}
	return nil
}

type File struct {
	Filename string `json:"filename"`
	Os       string `json:"os"`
	Arch     string `json:"arch"`
	Version  string `json:"version"`
	Sha256   string `json:"sha256"`
	Size     int    `json:"size"`
	Kind     string `json:"kind"`
}
type Files []File

func (files Files) Filter(specs ...func(f File) bool) []File {
	var filteredFiles []File

	for i, v := range files {
		isSpecified := true
		for _, spec := range specs {
			isSpecified = isSpecified && spec(v)
		}
		if isSpecified {
			filteredFiles = append(filteredFiles, files[i])
		}
	}

	return filteredFiles
}

type Release struct {
	Version string `json:"version"`
	Stable  bool   `json:"stable"`
	Files   Files  `json:"files"`
}

type ByRelease []Release

func (a ByRelease) Len() int      { return len(a) }
func (a ByRelease) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a ByRelease) Less(i, j int) bool {
	ai := strings.ReplaceAll(a[i].Version, "go", "v")
	aj := strings.ReplaceAll(a[j].Version, "go", "v")
	return semver.Compare(ai, aj) > 0
}

func Decompress(dst string, r io.Reader) error {
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}

	gzr, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)

	for {
		header, err := tr.Next()

		switch {
		case err == io.EOF:
			return nil
		case err != nil:
			return err
		}

		target := filepath.Join(dst, header.Name)

		switch header.Typeflag {
		case tar.TypeDir:
			if _, err := os.Stat(target); err != nil {
				if err := os.MkdirAll(target, 0755); err != nil {
					return err
				}
			}
		case tar.TypeReg:
			f, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_RDWR, os.FileMode(header.Mode))
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				return err
			}
			f.Close()
		}
	}
}

var app *tea.Program

func main() {
	var err error
	ctx := context.Background()
	client := &http.Client{Timeout: time.Duration(30) * time.Second}
	repo := &GoRepository{
		client: client,
		url:    "https://go.dev/dl",
		onProgress: func(ratio float64) {
			app.Send(progressMsg(ratio))
		},
	}

	versions, err := repo.GetVersions(ctx)
	if err != nil {
		fmt.Println("Error downloading go versions list:", err)
	}

	items := []list.Item{}
	for _, v := range versions {
		items = append(items, item(v.Version))
	}

	const listHeight = 14
	const defaultWidth = 20

	l := list.New(items, itemDelegate{}, defaultWidth, listHeight)
	l.Title = "What version of Go do you to download?"
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)
	l.Styles.Title = titleStyle
	l.Styles.PaginationStyle = paginationStyle
	l.Styles.HelpStyle = helpStyle

	p := progress.New(progress.WithGradient("#000000", "#FFFFFF"))

	m := model{ctx: ctx, list: l, progress: p, repo: repo, versions: versions}

	app = tea.NewProgram(m)

	if _, err := app.Run(); err != nil {
		fmt.Println("Error running program:", err)
		os.Exit(1)
	}
}
