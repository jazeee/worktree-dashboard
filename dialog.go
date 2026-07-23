package main

import (
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
)

// dialog.go holds the confirm / text-input dialog sub-state. Bubble Tea has no
// native overlay, so the model renders the active dialog centered over the body
// and routes keys to it while it is open. Ported from v1's ConfirmModal /
// InputModal ModalScreens (Textual class names).

// DialogKind is which dialog (if any) is currently displayed.
type DialogKind string

const (
	DialogNone    DialogKind = "None"
	DialogConfirm DialogKind = "Confirm"
	DialogInput   DialogKind = "Input"
)

// DialogIntent is what a confirmed/submitted dialog should do, so the model knows
// how to act on the result without a tangle of booleans.
type DialogIntent string

const (
	IntentNone           DialogIntent = "None"
	IntentDeleteRow      DialogIntent = "DeleteRow"
	IntentCreateWorktree DialogIntent = "CreateWorktree"
)

// DialogState carries everything the active dialog needs.
type DialogState struct {
	Kind      DialogKind
	Intent    DialogIntent
	Prompt    string
	TargetKey string // row the dialog acts on (delete)
	BaseDir   string // base directory for a new worktree (create)
	Input     textinput.Model
}

// NoDialog returns the inactive dialog state.
func NoDialog() DialogState {
	return DialogState{Kind: DialogNone, Intent: IntentNone}
}

// IsOpen reports whether a dialog is currently displayed.
func (dialog DialogState) IsOpen() bool {
	return dialog.Kind != DialogNone
}

// NewConfirmDialog builds a yes/no confirmation dialog.
func NewConfirmDialog(intent DialogIntent, prompt string, targetKey string) DialogState {
	return DialogState{
		Kind:      DialogConfirm,
		Intent:    intent,
		Prompt:    prompt,
		TargetKey: targetKey,
	}
}

// NewInputDialog builds a single-line text-input dialog with a focused field.
func NewInputDialog(intent DialogIntent, prompt string, placeholder string, baseDirectory string) DialogState {
	field := textinput.New()
	field.Placeholder = placeholder
	field.Focus()
	field.CharLimit = 120
	field.Width = 40
	return DialogState{
		Kind:    DialogInput,
		Intent:  intent,
		Prompt:  prompt,
		BaseDir: baseDirectory,
		Input:   field,
	}
}

// RenderDialog centers the dialog box within the given area.
func RenderDialog(dialog DialogState, width int, height int) string {
	var body string
	switch dialog.Kind {
	case DialogConfirm:
		body = dialog.Prompt + "\n\n" + styleDim.Render("[y / enter] confirm    [n / esc] cancel")
	case DialogInput:
		body = dialog.Prompt + "\n\n" + dialog.Input.View() + "\n\n" + styleDim.Render("[enter] create    [esc] cancel")
	default:
		return ""
	}
	box := styleDialogBox.Render(body)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}
