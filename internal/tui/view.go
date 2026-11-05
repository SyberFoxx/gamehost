package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

var (
	titleStyle    = lipgloss.NewStyle().Bold(true)
	accentStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	selectedStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("42"))
	dimStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	errorStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	panelStyle    = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("240")).
			Padding(0, 1)
)

// wordmarkGlyphs is GAMEHOST in the ANSI Shadow figlet face, one block of six
// rows per letter. The blocks are padded and joined, so a typo in one row
// shows up as a misaligned letter instead of a silently broken banner.
var wordmarkGlyphs = [][6]string{
	{
		" ██████╗",
		"██╔════╝",
		"██║  ███╗",
		"██║   ██║",
		"╚██████╔╝",
		" ╚═════╝",
	},
	{
		" █████╗",
		"██╔══██╗",
		"███████║",
		"██╔══██║",
		"██║  ██║",
		"╚═╝  ╚═╝",
	},
	{
		"███╗   ███╗",
		"████╗ ████║",
		"██╔████╔██║",
		"██║╚██╔╝██║",
		"██║ ╚═╝ ██║",
		"╚═╝     ╚═╝",
	},
	{
		"███████╗",
		"██╔════╝",
		"█████╗",
		"██╔══╝",
		"███████╗",
		"╚══════╝",
	},
	{
		"██╗  ██╗",
		"██║  ██║",
		"███████║",
		"██╔══██║",
		"██║  ██║",
		"╚═╝  ╚═╝",
	},
	{
		" ██████╗",
		"██╔═══██╗",
		"██║   ██║",
		"██║   ██║",
		"╚██████╔╝",
		" ╚═════╝",
	},
	{
		"███████╗",
		"██╔════╝",
		"███████╗",
		"╚════██║",
		"███████║",
		"╚══════╝",
	},
	{
		"████████╗",
		"╚══██╔══╝",
		"   ██║",
		"   ██║",
		"   ██║",
		"   ╚═╝",
	},
}

// wordmark renders GAMEHOST in six rows of block letters.
func wordmark() string {
	rows := make([]string, len(wordmarkGlyphs[0]))
	for _, glyph := range wordmarkGlyphs {
		width := 0
		for _, line := range glyph {
			width = max(width, lipgloss.Width(line))
		}
		for i, line := range glyph {
			if rows[i] != "" {
				rows[i] += " "
			}
			rows[i] += line + strings.Repeat(" ", width-lipgloss.Width(line))
		}
	}
	return strings.Join(rows, "\n")
}

// View renders the current screen in the alternate buffer.
func (m Model) View() tea.View {
	view := tea.NewView(m.render())
	view.AltScreen = true
	view.WindowTitle = "gamehost"
	return view
}

// render draws the screen the model points at.
func (m Model) render() string {
	if m.screen == screenLogs {
		return m.renderLogs()
	}
	return m.renderLobby()
}

// renderLobby draws the banner, the selected instance, the menu and the footer.
func (m Model) renderLobby() string {
	banner := accentStyle.Render(wordmark())
	subtitle := dimStyle.Render("dedicated game servers without memorising flags")
	// A short or narrow terminal gets the name instead of the full banner,
	// so the menu never scrolls out of view.
	if m.width < 80 || (m.height > 0 && m.height < 26) {
		banner = accentStyle.Render("GAMEHOST")
	}

	parts := []string{
		banner,
		subtitle,
		m.renderPanel(),
		m.renderMenu(),
		m.renderFooter(),
	}
	return strings.Join(parts, "\n\n")
}

// renderPanel draws the state of the selected instance.
func (m Model) renderPanel() string {
	item, ok := m.current()
	if !ok {
		return m.panel("No instances yet.\n\nCreate one with:  gamehost create my-world")
	}

	phase := item.Phase
	if phase == "" {
		phase = "unknown"
	}
	state := "○ " + phase
	color := "245"
	if item.Phase == "running" || item.Phase == "starting" {
		state = "● " + phase
		color = "42"
	}

	head := titleStyle.Render(item.Name) + "   " + dimStyle.Render(item.Game)
	if len(m.items) > 1 {
		head += dimStyle.Render(fmt.Sprintf("   %d/%d", m.item+1, len(m.items)))
	}

	line := lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(state)
	if item.PID > 0 {
		line += dimStyle.Render(fmt.Sprintf("   pid %d", item.PID))
	}
	if up := uptime(item); up != "" {
		line += dimStyle.Render("   up " + up)
	}

	join := ""
	if item.JoinLabel != "" || item.JoinValue != "" {
		join = "\n" + dimStyle.Render(item.JoinLabel+" ") + item.JoinValue
	}
	return m.panel(head + "\n" + line + join)
}

// panel wraps content in the instance frame, sized to the terminal.
func (m Model) panel(content string) string {
	style := panelStyle
	if m.width > 12 {
		style = style.Width(min(70, m.width-6))
	}
	return style.Render(content)
}

// renderMenu draws the action list with the cursor on the selected entry.
func (m Model) renderMenu() string {
	var b strings.Builder
	for i, entry := range menu {
		if i > 0 {
			b.WriteByte('\n')
		}
		if i == m.cursor {
			b.WriteString(selectedStyle.Render("› " + entry.label))
			b.WriteString("  " + dimStyle.Render(entry.hint))
			continue
		}
		b.WriteString("  " + entry.label)
	}
	return b.String()
}

// renderFooter draws the last result and the keys that work right now.
func (m Model) renderFooter() string {
	help := "↑↓ move    ↵ select    q quit"
	if len(m.items) > 1 {
		help = "↑↓ move    ←→ instance    ↵ select    q quit"
	}

	note := m.note
	if m.busy {
		note = spinner[m.frame%len(spinner)] + " " + note
	}
	if note == "" {
		return dimStyle.Render(help)
	}

	style := dimStyle
	if m.bad {
		style = errorStyle
	}
	return style.Render(clip(note, m.width)) + "\n" + dimStyle.Render(help)
}

// renderLogs draws the newest log of the selected instance.
func (m Model) renderLogs() string {
	head := "Logs"
	if item, ok := m.current(); ok {
		head = "Logs · " + item.Name
	}
	head = titleStyle.Render(head) +
		dimStyle.Render(fmt.Sprintf("   last %d lines", DefaultLogLines))
	if m.busy {
		head += "  " + spinner[m.frame%len(spinner)]
	}

	help := "↑↓ scroll    pgup/pgdn page    r refresh    esc back"
	return head + "\n\n" + m.renderLogBody() + "\n\n" + dimStyle.Render(help)
}

// renderLogBody draws the visible slice of the log buffer.
func (m Model) renderLogBody() string {
	if len(m.logs) == 0 {
		return dimStyle.Render("no logs yet, start the server first")
	}

	rows := m.logRows()
	start := min(max(0, m.scroll), max(0, len(m.logs)-rows))
	end := min(start+rows, len(m.logs))

	visible := make([]string, 0, end-start)
	for _, line := range m.logs[start:end] {
		visible = append(visible, clip(line, m.width))
	}
	return strings.Join(visible, "\n")
}

// uptime formats how long the selected server has been up.
func uptime(item Item) string {
	if item.PID == 0 || item.StartedAt.IsZero() {
		return ""
	}
	elapsed := time.Since(item.StartedAt)
	if elapsed < 0 {
		return ""
	}
	return formatUptime(elapsed)
}

// formatUptime renders a duration at the precision a player cares about:
// seconds in the first minute, minutes after that.
func formatUptime(elapsed time.Duration) string {
	if elapsed < time.Minute {
		return elapsed.Round(time.Second).String()
	}
	minutes := int(elapsed.Minutes())
	hours, rest := minutes/60, minutes%60
	switch {
	case hours == 0:
		return fmt.Sprintf("%dm", rest)
	case rest == 0:
		return fmt.Sprintf("%dh", hours)
	default:
		return fmt.Sprintf("%dh%02dm", hours, rest)
	}
}

// clip cuts a line to the terminal width, marking the cut.
func clip(line string, width int) string {
	if width <= 0 || lipgloss.Width(line) <= width {
		return line
	}
	runes := []rune(line)
	if width <= 1 {
		return "…"
	}
	if len(runes) > width-1 {
		runes = runes[:width-1]
	}
	return string(runes) + "…"
}
