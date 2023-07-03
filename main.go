package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"golang.org/x/mod/semver"
)

type GoRepository struct {
	url    string
	client *http.Client
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

	buf := make([]byte, 32*1024)
	for {
		nr, errRead := resp.Body.Read(buf)
		if nr > 0 {
			_, errWrite := outFile.Write(buf[0:nr])
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

// TUI

var (
	titleStyle        = lipgloss.NewStyle().MarginLeft(2)
	itemStyle         = lipgloss.NewStyle().PaddingLeft(4)
	selectedItemStyle = lipgloss.NewStyle().PaddingLeft(2).Foreground(lipgloss.Color("027"))
	paginationStyle   = list.DefaultStyles().PaginationStyle.PaddingLeft(4)
	helpStyle         = list.DefaultStyles().HelpStyle.PaddingLeft(4).PaddingBottom(1)
	quitTextStyle     = lipgloss.NewStyle().Margin(1, 0, 2, 4)
)

type item string

func (i item) FilterValue() string { return "" }

type itemDelegate struct{}

func (d itemDelegate) Height() int                             { return 1 }
func (d itemDelegate) Spacing() int                            { return 0 }
func (d itemDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }
func (d itemDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	i, ok := listItem.(item)
	if !ok {
		return
	}

	str := fmt.Sprintf("%d. %s", index+1, i)

	fn := itemStyle.Render
	if index == m.Index() {
		fn = func(s ...string) string {
			return selectedItemStyle.Render("> " + strings.Join(s, " "))
		}
	}

	fmt.Fprint(w, fn(str))
}

type model struct {
	list     list.Model
	choice   string
	quitting bool
	ctx      context.Context
	repo     *GoRepository
	versions []Release
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.list.SetWidth(msg.Width)
		return m, nil

	case tea.KeyMsg:
		switch keypress := msg.String(); keypress {
		case "ctrl+c":
			m.quitting = true
			return m, tea.Quit

		case "enter":
			i, ok := m.list.SelectedItem().(item)
			if ok {
				m.choice = string(i)
			}

			f, err := os.OpenFile(".tmpDownload", os.O_CREATE|os.O_WRONLY, 0666)
			if err != nil {
				fmt.Println("Error running program:", err)
			}

			var dlf File
			for _, v := range m.versions {
				if m.choice == v.Version {
					l := v.Files.Filter(
						func(f File) bool { return f.Os == "linux" },
						func(f File) bool { return f.Arch == "amd64" },
					)
					if len(l) > 0 {
						dlf = l[0]
					}
				}
			}

			if dlf == (File{}) {
				fmt.Println("Did not found a matching file", err)
			}
			m.repo.Download(m.ctx, dlf, f)
			return m, tea.Quit
		}
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m model) View() string {
	if m.choice != "" {
		return quitTextStyle.Render(fmt.Sprintf("Downloading: %s", m.choice))
	}

	if m.quitting {
		return quitTextStyle.Render("Finishing..")
	}

	return "\n" + m.list.View()
}

func main() {
	var err error
	ctx := context.Background()
	client := &http.Client{Timeout: time.Duration(30) * time.Second}
	repo := &GoRepository{client: client, url: "https://go.dev/dl"}

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

	m := model{list: l, ctx: ctx, repo: repo, versions: versions}

	if _, err := tea.NewProgram(m).Run(); err != nil {
		fmt.Println("Error running program:", err)
		os.Exit(1)
	}
}
