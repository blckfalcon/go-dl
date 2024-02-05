package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/progress"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	titleStyle        = lipgloss.NewStyle().MarginLeft(2)
	itemStyle         = lipgloss.NewStyle().PaddingLeft(4)
	selectedItemStyle = lipgloss.NewStyle().PaddingLeft(2).Foreground(lipgloss.Color("027"))
	paginationStyle   = list.DefaultStyles().PaginationStyle.PaddingLeft(4)
	helpStyle         = list.DefaultStyles().HelpStyle.PaddingLeft(4).PaddingBottom(1)
	quitTextStyle     = lipgloss.NewStyle().Margin(1, 0, 1, 4)
	progressStyle     = lipgloss.NewStyle().MarginLeft(4)
)

type item string
type startDownloadMsg struct{}
type progressMsg float64
type errMsg struct{ err error }

func startDownloadCmd() tea.Msg {
	return startDownloadMsg{}
}

func errorCmd(err error) tea.Cmd {
	return func() tea.Msg {
		return errMsg{err}
	}
}

func finalPause() tea.Cmd {
	return tea.Tick(time.Millisecond*750, func(_ time.Time) tea.Msg {
		return nil
	})
}

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
	ctx      context.Context
	list     list.Model
	choice   string
	progress progress.Model
	quitting bool
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

			return m, startDownloadCmd
		}

	case errMsg:
		m.err = msg.err
		return m, tea.Quit

	case startDownloadMsg:
		var err error
		var dlf File

		f, err := os.CreateTemp("", "go-dl-tmp.tar.gz")
		if err != nil {
			return m, errorCmd(err)
		}

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
			return m, errorCmd(errors.New("did not found a matching file"))
		}

		go func() {
			err = m.repo.Download(m.ctx, dlf, f)
			defer f.Close()
			if err != nil {
				app.Send(errMsg{err})
				return
			}

			_, err = f.Seek(0, io.SeekStart)
			if err != nil {
				app.Send(errMsg{err})
				return
			}

			err = os.RemoveAll("/usr/local/go")
			if err != nil {
				app.Send(errMsg{err})
				return
			}

			err = Decompress("/usr/local", f)
			if err != nil {
				app.Send(errMsg{err})
				return
			}
		}()

		return m, nil

	case progressMsg:
		var cmds []tea.Cmd

		if msg >= 1.0 {
			cmds = append(cmds, tea.Sequence(finalPause()))
		}

		cmds = append(cmds, m.progress.SetPercent(float64(msg)))
		return m, tea.Batch(cmds...)

	case progress.FrameMsg:
		progressModel, cmd := m.progress.Update(msg)
		m.progress = progressModel.(progress.Model)
		return m, cmd

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
		return quitTextStyle.Render(fmt.Sprintf("Downloading: %s", m.choice)) + "\n\n" +
			progressStyle.Render() + m.progress.View() + "\n\n"
	}

	if m.quitting {
		return quitTextStyle.Render("Finishing..")
	}

	return "\n" + m.list.View()
}
