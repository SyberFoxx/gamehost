package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
)

var (
	titleStyle    = lipgloss.NewStyle().Bold(true)
	accentStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	selectedStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("42"))
	dimStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	errorStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	okStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	warnStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
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
		glyphWidth := 0
		for _, line := range glyph {
			glyphWidth = max(glyphWidth, lipgloss.Width(line))
		}
		for i, line := range glyph {
			if rows[i] != "" {
				rows[i] += " "
			}
			rows[i] += line + strings.Repeat(" ", glyphWidth-lipgloss.Width(line))
		}
	}
	return strings.Join(rows, "\n")
}

// textWidth is the number of terminal cells a string occupies.
func textWidth(s string) int {
	return lipgloss.Width(s)
}

// spinner is drawn while a backend call is in flight.
var spinner = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

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

// padRight pads s with spaces to width cells; an already wide string is kept.
func padRight(s string, width int) string {
	if gap := width - textWidth(s); gap > 0 {
		return s + strings.Repeat(" ", gap)
	}
	return s
}

// clip cuts a line to the terminal width, marking the cut.
func clip(line string, width int) string {
	if width <= 0 || textWidth(line) <= width {
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

// humanBytes formats a byte count with a binary unit.
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for value := n / unit; value >= unit; value /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

// statusStyle picks the marker and colour of a doctor or service status.
func statusStyle(status string) (string, lipgloss.Style) {
	switch strings.ToLower(status) {
	case "ok", "pass", "running", "active", "yes":
		return "✓", okStyle
	case "warn", "warning", "limited":
		return "!", warnStyle
	default:
		return "✗", errorStyle
	}
}

// appendLines adds one streamed chunk, keeping embedded newlines.
func appendLines(lines []string, chunk string) []string {
	chunk = strings.TrimRight(chunk, "\n")
	if chunk == "" {
		return lines
	}
	return append(lines, strings.Split(chunk, "\n")...)
}

// tailWindow returns up to rows lines ending scroll lines before the end.
// Scroll counts from the bottom, so zero is the live edge.
func tailWindow(lines []string, rows, scroll int) []string {
	if rows <= 0 || len(lines) == 0 {
		return nil
	}
	scroll = min(max(0, scroll), max(0, len(lines)-1))
	end := len(lines) - scroll
	start := max(0, end-rows)
	return lines[start:end]
}

// streamRows is how many lines fit under a screen title and footer.
func streamRows(height int) int {
	return max(4, height-5)
}
