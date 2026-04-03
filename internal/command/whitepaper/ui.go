package whitepaper

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/bornholm/genai/agent"
	wppkg "github.com/bornholm/ghostwriter/pkg/whitepaper"
)

var (
	primaryColor   = lipgloss.Color("86")
	secondaryColor = lipgloss.Color("75")
	successColor   = lipgloss.Color("76")
	errorColor     = lipgloss.Color("196")
	infoColor      = lipgloss.Color("39")
	mutedColor     = lipgloss.Color("242")
	reasoningColor = lipgloss.Color("219")
	timeColor      = lipgloss.Color("245")

	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(primaryColor)

	subtleStyle = lipgloss.NewStyle().
			Foreground(mutedColor).
			Italic(true)

	successStyle = lipgloss.NewStyle().
			Foreground(successColor).
			Bold(true)

	errorStyle = lipgloss.NewStyle().
			Foreground(errorColor).
			Bold(true)

	infoStyle = lipgloss.NewStyle().
			Foreground(infoColor)

	reasoningStyle = lipgloss.NewStyle().
			Foreground(reasoningColor).
			Italic(true)

	toolNameStyle = lipgloss.NewStyle().
			Foreground(secondaryColor).
			Bold(true)

	toolResultStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("252"))

	timeStyle = lipgloss.NewStyle().
			Foreground(timeColor).
			Italic(true)
)

func RenderEvent(evt agent.Event) string {
	switch evt.Type() {
	case agent.EventTypeTextDelta:
		data := evt.Data().(*agent.TextDeltaData)
		return data.Delta
	case agent.EventTypeComplete:
		data := evt.Data().(*agent.CompleteData)
		return renderComplete(data)
	case agent.EventTypeToolCallStart:
		data := evt.Data().(*agent.ToolCallStartData)
		return renderToolCallStart(data)
	case agent.EventTypeToolCallDone:
		data := evt.Data().(*agent.ToolCallDoneData)
		return renderToolCallDone(data)
	case agent.EventTypeTodoUpdated:
		data := evt.Data().(*agent.TodoUpdatedData)
		return renderTodoUpdated(data)
	case agent.EventTypeReasoning:
		data := evt.Data().(*agent.ReasoningData)
		return renderReasoning(data)
	case agent.EventTypeError:
		data := evt.Data().(*agent.ErrorData)
		return renderError(data)
	case wppkg.EventTypePhase:
		data := evt.Data().(*wppkg.PhaseData)
		return renderPhase(data)
	case wppkg.EventTypeChapterStart:
		data := evt.Data().(*wppkg.ChapterStartData)
		return renderChapterStart(data)
	case wppkg.EventTypeChapterDone:
		data := evt.Data().(*wppkg.ChapterDoneData)
		return renderChapterDone(data)
	default:
		return ""
	}
}

func renderPhase(data *wppkg.PhaseData) string {
	if data.Done {
		check := successStyle.Render("✓")
		name := infoStyle.Render(data.Name)
		if data.Info != "" {
			return fmt.Sprintf("  %s %s — %s\n", check, name, subtleStyle.Render(data.Info))
		}
		return fmt.Sprintf("  %s %s\n", check, name)
	}
	timestamp := timeStyle.Render(formatTime(time.Now()))
	icon := infoStyle.Render("▶")
	return fmt.Sprintf("\n%s %s %s\n", timestamp, icon, titleStyle.Render(data.Name))
}

func renderChapterStart(data *wppkg.ChapterStartData) string {
	timestamp := timeStyle.Render(formatTime(time.Now()))
	chNum := fmt.Sprintf("%d/%d", data.Number, data.Total)
	title := titleStyle.Render(fmt.Sprintf("Chapitre %s : %s", chNum, data.Title))
	target := subtleStyle.Render(fmt.Sprintf("cible : %d mots", data.Target))
	return fmt.Sprintf("\n%s  ─ %s  %s\n", timestamp, title, target)
}

func renderChapterDone(data *wppkg.ChapterDoneData) string {
	check := successStyle.Render("✓")
	chNum := fmt.Sprintf("%d/%d", data.Number, data.Total)
	info := infoStyle.Render(fmt.Sprintf("Chapitre %s — %d mots", chNum, data.WordCount))
	return fmt.Sprintf("  %s %s\n", check, info)
}

func renderComplete(data *agent.CompleteData) string {
	if data.Message == "" {
		return ""
	}
	timestamp := timeStyle.Render(formatTime(time.Now()))
	header := successStyle.Render("✓ Terminé")
	return fmt.Sprintf("\n%s %s — %s\n", timestamp, header, data.Message)
}

func renderToolCallStart(data *agent.ToolCallStartData) string {
	timestamp := timeStyle.Render(formatTime(time.Now()))
	header := fmt.Sprintf("⚡ %s", toolNameStyle.Render(data.Name))
	var paramsStr string
	if data.Parameters != nil {
		paramsStr = fmt.Sprintf("%v", data.Parameters)
		if len(paramsStr) > 120 {
			paramsStr = paramsStr[:120] + "…"
		}
		paramsStr = subtleStyle.Render(paramsStr)
	}
	if paramsStr != "" {
		return fmt.Sprintf("\n%s %s  %s\n", timestamp, header, paramsStr)
	}
	return fmt.Sprintf("\n%s %s\n", timestamp, header)
}

func renderToolCallDone(data *agent.ToolCallDoneData) string {
	timestamp := timeStyle.Render(formatTime(time.Now()))
	header := fmt.Sprintf("%s %s", successStyle.Render("✓"), toolNameStyle.Render(data.Name))
	result := data.Result
	if len(result) > 200 {
		result = result[:200] + "…"
	}
	lines := strings.Split(strings.TrimSpace(result), "\n")
	// Keep only first 3 lines for conciseness
	if len(lines) > 3 {
		lines = lines[:3]
		lines = append(lines, "…")
	}
	for i, line := range lines {
		lines[i] = "  " + toolResultStyle.Render(line)
	}
	return fmt.Sprintf("%s %s\n%s\n", timestamp, header, strings.Join(lines, "\n"))
}

func renderTodoUpdated(data *agent.TodoUpdatedData) string {
	if len(data.Items) == 0 {
		return ""
	}
	var lines []string
	lines = append(lines, "\n  "+infoStyle.Render("📋 Tâches"))
	for i, item := range data.Items {
		var icon string
		var style lipgloss.Style
		switch item.Status {
		case agent.TodoStatusDone:
			icon, style = "✓", successStyle
		case agent.TodoStatusInProgress:
			icon = "◐"
			style = lipgloss.NewStyle().Foreground(lipgloss.Color("226")).Bold(true)
		default:
			icon, style = "○", subtleStyle
		}
		lines = append(lines, fmt.Sprintf("    %s %s %s",
			style.Render(icon),
			subtleStyle.Render(fmt.Sprintf("#%d", i+1)),
			item.Content,
		))
	}
	return strings.Join(lines, "\n") + "\n"
}

func renderReasoning(data *agent.ReasoningData) string {
	if data.Reasoning == "" {
		return ""
	}
	timestamp := timeStyle.Render(formatTime(time.Now()))
	header := reasoningStyle.Render("🤔 Raisonnement")
	lines := strings.Split(strings.TrimSpace(data.Reasoning), "\n")
	// Limit to first 5 lines
	if len(lines) > 5 {
		lines = lines[:5]
		lines = append(lines, "…")
	}
	for i, line := range lines {
		lines[i] = "  " + reasoningStyle.Render(line)
	}
	return fmt.Sprintf("\n%s %s\n%s\n", timestamp, header, strings.Join(lines, "\n"))
}

func renderError(data *agent.ErrorData) string {
	timestamp := timeStyle.Render(formatTime(time.Now()))
	header := errorStyle.Render("✗ Erreur")
	return fmt.Sprintf("\n%s %s — %s\n", timestamp, header, errorStyle.Render(data.Message))
}

func formatTime(t time.Time) string {
	return t.Format("15:04:05")
}
