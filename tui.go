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

type State int

const (
	Choosing State = iota
	Downloading
	Extracting
	Quitting
	Completed
)

type item string
type doneMsg struct{}
type progressMsg float64
type statusMsg State
type errMsg struct{ err error }

func downloadCmd(m *model) tea.Cmd {
	return func() tea.Msg {
		var err error
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
			return errMsg{errors.New("did not found a matching file")}
		}

		m.file, err = os.CreateTemp("", "go-dl-tmp.tar.gz")
		if err != nil {
			return errMsg{err}
		}

		err = m.repo.Download(m.ctx, dlf, m.file)
		if err != nil {
			return errMsg{err}
		}
		return nil
	}
}

func extractCmd(m *model) tea.Cmd {
	return func() tea.Msg {
		var err error

		defer m.file.Close()

		_, err = m.file.Seek(0, io.SeekStart)
		if err != nil {
			return errMsg{err}
		}

		err = os.RemoveAll("/usr/local/go")
		if err != nil {
			return errMsg{err}
		}

		err = Decompress("/usr/local", m.file, m.repo.onProgress)
		if err != nil {
			return errMsg{err}
		}
		return doneMsg{}
	}
}

func statusCmd(s State) tea.Cmd {
	return func() tea.Msg {
		return statusMsg(s)
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
	repo     *GoRepository
	versions []Release
	file     *os.File
	status   State
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
			m.status = Quitting
			return m, tea.Quit

		case "enter":
			i, ok := m.list.SelectedItem().(item)
			if ok {
				m.choice = string(i)
			}

			return m, tea.Sequence(
				statusCmd(Downloading),
				downloadCmd(&m),
				statusCmd(Extracting),
				extractCmd(&m),
			)
		}

	case statusMsg:
		m.status = State(msg)
		return m, nil

	case errMsg:
		m.err = msg.err
		return m, tea.Quit

	case doneMsg:
		m.status = Completed
		return m, tea.Sequence(finalPause(), tea.Quit)

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

	if m.status == Downloading {
		return lipgloss.JoinVertical(
			lipgloss.Left,
			quitTextStyle.Render(fmt.Sprintf("Downloading: %s", m.choice)),
			progressStyle.Render(m.progress.View()),
			"",
		)
	}

	if m.status == Extracting {
		return lipgloss.JoinVertical(
			lipgloss.Left,
			quitTextStyle.Render(fmt.Sprintf("Extracting: %s", m.choice)),
			progressStyle.Render(m.progress.View()),
			"",
		)
	}

	if m.status == Completed {
		return lipgloss.JoinVertical(
			lipgloss.Left,
			quitTextStyle.Render(fmt.Sprintf("Completed download and extraction of %s !", m.choice)),
			progressStyle.Render(m.progress.View()),
			"",
		)
	}

	if m.status == Quitting {
		return quitTextStyle.Render("exiting..")
	}

	return "\n" + m.list.View()
}
