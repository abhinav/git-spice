package ui

import "github.com/charmbracelet/lipgloss"

// This file defines the common defaults and styles for the UI components.
var (
	Yellow  = lipgloss.AdaptiveColor{Light: "2", Dark: "11"}
	Red     = lipgloss.AdaptiveColor{Light: "1", Dark: "9"}
	Green   = lipgloss.AdaptiveColor{Light: "2", Dark: "10"}
	Plain   = lipgloss.AdaptiveColor{Light: "0", Dark: "7"}
	Cyan    = lipgloss.AdaptiveColor{Light: "6", Dark: "14"}
	Magenta = lipgloss.AdaptiveColor{Light: "5", Dark: "13"}
	Gray    = lipgloss.AdaptiveColor{Light: "8", Dark: "8"}

	_titleStyle         = lipgloss.NewStyle().Foreground(Green).Bold(true)
	_descriptionStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Faint(true)
	_acceptedTitleStyle = lipgloss.NewStyle().Foreground(Plain)
)
