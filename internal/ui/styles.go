package ui

import "github.com/charmbracelet/lipgloss"

var (
	styleTitle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")).
			Background(lipgloss.Color("25")).Padding(0, 1)
	styleHeader   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	styleSelected = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")).
			Background(lipgloss.Color("14"))
	styleDim     = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	styleWarn    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("11"))
	styleDanger  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("9"))
	styleSuccess = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("10"))
	styleInfo    = lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
	styleHelp    = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	styleInvalid = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("9"))
	styleCommand = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15"))
	styleExtra   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("11")) // extra args in warning color

	styleSuccessBanner = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")).
				Background(lipgloss.Color("10")).Padding(0, 2)
	styleFailBanner = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")).
			Background(lipgloss.Color("9")).Padding(0, 2)
	styleAbortBanner = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")).
				Background(lipgloss.Color("11")).Padding(0, 2)

	styleBox = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
)

func severityStyle(sev int) lipgloss.Style {
	switch sev {
	case 2:
		return styleDanger
	case 1:
		return styleWarn
	}
	return styleInfo
}
