package ui

import "github.com/charmbracelet/lipgloss"

// This file defines the common defaults and styles for the UI components.
var (
	_yellowColor  = lipgloss.AdaptiveColor{Light: "2", Dark: "11"}
	_redColor     = lipgloss.AdaptiveColor{Light: "1", Dark: "9"}
	_greenColor   = lipgloss.AdaptiveColor{Light: "2", Dark: "10"}
	_plainColor   = lipgloss.AdaptiveColor{Light: "0", Dark: "7"}
	_cyanColor    = lipgloss.AdaptiveColor{Light: "6", Dark: "14"}
	_magentaColor = lipgloss.AdaptiveColor{Light: "5", Dark: "13"}
	_grayColor    = lipgloss.AdaptiveColor{Light: "8", Dark: "8"}

	_titleStyle         = lipgloss.NewStyle().Foreground(_greenColor).Bold(true)
	_descriptionStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Faint(true)
	_acceptedTitleStyle = lipgloss.NewStyle().Foreground(_plainColor)
)
