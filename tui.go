package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

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
	err      error
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
			var err error
			var dlf File

			i, ok := m.list.SelectedItem().(item)
			if ok {
				m.choice = string(i)
			}

			f, err := os.CreateTemp("", "go-dl-tmp.tar.gz")
			if err != nil {
				m.err = err
				return m, nil
			}
			defer f.Close()

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

			err = m.repo.Download(m.ctx, dlf, f)
			if err != nil {
				m.err = err
				return m, nil
			}

			_, err = f.Seek(0, io.SeekStart)
			if err != nil {
				m.err = err
				return m, nil
			}

			err = os.RemoveAll("/usr/local/go")
			if err != nil {
				m.err = err
				return m, nil
			}

			err = Decompress("/usr/local", f)
			if err != nil {
				m.err = err
				return m, nil
			}

			return m, tea.Quit
		}
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m model) View() string {
	if m.err != nil {
		return quitTextStyle.Render(fmt.Sprintf("something went wrong: %v", m.err))
	}

	if m.choice != "" {
		return quitTextStyle.Render(fmt.Sprintf("Downloading: %s", m.choice))
	}

	if m.quitting {
		return quitTextStyle.Render("Finishing..")
	}

	return "\n" + m.list.View()
}
