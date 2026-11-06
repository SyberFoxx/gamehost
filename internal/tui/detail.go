package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// detail shows the whole configuration and the last known state of one
// instance. It is the lobby equivalent of `gamehost show`.
type detail struct {
	item Item
	rows [][2]string
	path string
}

// newDetail returns the read-only view of one instance.
func newDetail(d Detail) *detail {
	return &detail{item: d.Item, rows: d.Rows, path: d.Path}
}

// Init has nothing to load: the data is part of the screen.
func (d *detail) Init() tea.Cmd { return nil }

// Update only handles leaving.
func (d *detail) Update(msg tea.Msg) (screen, tea.Cmd) {
	if key, ok := msg.(tea.KeyPressMsg); ok {
		switch key.String() {
		case "esc", "q", "backspace":
			return d, pop()
		}
	}
	return d, nil
}

// View draws the rows aligned, then the configuration path.
func (d *detail) View(width, _ int) string {
	labelWidth := 0
	for _, row := range d.rows {
		labelWidth = max(labelWidth, textWidth(row[0]))
	}

	lines := make([]string, 0, len(d.rows))
	for _, row := range d.rows {
		lines = append(lines, dimStyle.Render(padRight(row[0], labelWidth))+"  "+row[1])
	}

	head := titleStyle.Render("Details") + "   " + dimStyle.Render(d.item.Name)
	body := head + "\n\n" + strings.Join(lines, "\n")
	if d.path != "" {
		body += "\n\n" + dimStyle.Render(d.path)
	}
	body += "\n\n" + dimStyle.Render("esc back")
	return clipBlock(body, width)
}

// Keys describes the detail shortcuts for the help overlay.
func (d *detail) Keys() []KeyHelp {
	return []KeyHelp{{"esc", "back to the lobby", "gamehost show " + d.item.Name}}
}

// clipBlock cuts every line of a block to the terminal width.
func clipBlock(block string, width int) string {
	if width <= 0 {
		return block
	}
	lines := strings.Split(block, "\n")
	for i, line := range lines {
		lines[i] = clip(line, width)
	}
	return strings.Join(lines, "\n")
}
