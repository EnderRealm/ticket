// Package tui provides the interactive terminal UI for browsing and editing tickets.
package tui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/EnderRealm/ticket/pkg/ticket"
)

type view int

const (
	viewDashboard view = iota
	viewPipeline
	viewDetail
	viewForm
)

// App is the top-level bubbletea model.
type App struct {
	store      *ticket.FileStore
	ticketsDir string
	tickets    []*ticket.Ticket
	dashboard  dashboardModel
	detail     detailModel
	form       formModel
	pipeline   pipelineModel
	current    view
	prevView   view // view to return to from detail/form
	width      int
	height     int
	status     string // transient status message
	err        error
}

// New creates a new App rooted at the given ticket directory.
func New(ticketsDir string) App {
	store := ticket.NewFileStore(ticketsDir)
	return App{
		store:      store,
		ticketsDir: ticketsDir,
		current:    viewDashboard,
	}
}

// Messages

type ticketsLoadedMsg []*ticket.Ticket
type errMsg error
type statusMsg string
type clearStatusMsg struct{}

// cyclePriorityMsg requests a priority cycle on the selected ticket.
type cyclePriorityMsg struct{ id string }

// setAssigneeMsg sets the assignee on a ticket.
type setAssigneeMsg struct {
	id       string
	assignee string
}

// addNoteMsg adds a note to a ticket.
type addNoteMsg struct {
	id   string
	text string
}

// advanceMsg advances a ticket to the next pipeline stage.
type advanceMsg struct {
	id    string
	force bool
}

// reviewMsg records a review verdict on a ticket.
type reviewMsg struct {
	id      string
	verdict ticket.ReviewState
}

// skipMsg skips a ticket ahead in the pipeline.
type skipMsg struct {
	id string
}

// deleteTicketMsg deletes a ticket permanently.
type deleteTicketMsg struct {
	id string
}

// moveTicketMsg moves a ticket to another repo.
type moveTicketMsg struct {
	id         string
	targetRepo string
}

func loadTickets(store *ticket.FileStore) tea.Cmd {
	return func() tea.Msg {
		tickets, err := store.List()
		if err != nil {
			return errMsg(err)
		}
		ticket.SortByStagePriorityID(tickets)
		return ticketsLoadedMsg(tickets)
	}
}

func clearStatusAfter(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg {
		return clearStatusMsg{}
	})
}

func (a App) Init() tea.Cmd {
	return tea.Batch(
		loadTickets(a.store),
		watchTickets(a.ticketsDir),
	)
}

func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		a.dashboard.setSize(a.width, a.height)
		a.detail.setSize(a.width, a.height)
		a.form.setSize(a.width, a.height)
		a.pipeline.setSize(a.width, a.height)
		return a, nil

	case fileChangedMsg:
		return a, tea.Batch(
			loadTickets(a.store),
			watchTickets(a.ticketsDir),
		)

	case ticketsLoadedMsg:
		a.tickets = msg
		a.dashboard.refreshTickets(a.tickets)
		a.pipeline.refreshTickets(a.tickets)
		// Refresh detail view if currently showing a ticket.
		if a.current == viewDetail && a.detail.ticket != nil {
			for _, t := range a.tickets {
				if t.ID == a.detail.ticket.ID {
					a.detail = newDetailModel(t, a.width, a.height)
					break
				}
			}
		}
		return a, nil

	case errMsg:
		a.err = msg
		return a, tea.Quit

	case statusMsg:
		a.status = string(msg)
		return a, clearStatusAfter(3 * time.Second)

	case clearStatusMsg:
		a.status = ""
		return a, nil

	// Mutation messages - handled at App level where store is accessible.
	case cyclePriorityMsg:
		return a, a.handleCyclePriority(msg.id)
	case setAssigneeMsg:
		return a, a.handleSetAssignee(msg.id, msg.assignee)
	case addNoteMsg:
		return a, a.handleAddNote(msg.id, msg.text)
	case formSubmitMsg:
		if msg.editID != "" {
			return a, a.handleEditTicket(msg)
		}
		return a, a.handleCreateTicket(msg)
	case advanceMsg:
		return a, a.handleAdvance(msg.id, msg.force)
	case reviewMsg:
		return a, a.handleReview(msg.id, msg.verdict)
	case skipMsg:
		return a, a.handleSkip(msg.id)
	case deleteTicketMsg:
		return a, a.handleDelete(msg.id)
	case moveTicketMsg:
		return a, a.handleMove(msg.id, msg.targetRepo)
	case formCancelMsg:
		a.current = a.prevView
		return a, nil

	case tea.KeyMsg:
		switch a.current {
		case viewDashboard:
			if a.dashboard.inputActive() {
				break // let dashboard handle filter input
			}
			switch msg.String() {
			case "q":
				return a, tea.Quit
			case "c":
				a.form = newFormModel(a.width, a.height)
				a.prevView = viewDashboard
				a.current = viewForm
				return a, nil
			case "enter", "o":
				if t := a.dashboard.selected(); t != nil {
					a.detail = newDetailModel(t, a.width, a.height)
					a.prevView = viewDashboard
					a.current = viewDetail
					return a, nil
				}
			case "e":
				if t := a.dashboard.selected(); t != nil {
					a.form = newEditFormModel(t, a.width, a.height)
					a.prevView = viewDashboard
					a.current = viewForm
					return a, nil
				}
			case "m":
				if t := a.dashboard.selected(); t != nil {
					a.detail = newDetailModel(t, a.width, a.height)
					a.detail.startMovePicker(a.ticketsDir)
					a.prevView = viewDashboard
					a.current = viewDetail
					return a, nil
				}
			case "d":
				if t := a.dashboard.selected(); t != nil {
					a.dashboard.confirmDelete = true
					a.dashboard.deleteTargetID = t.ID
					return a, nil
				}
			case "p":
				if t := a.dashboard.selected(); t != nil {
					return a, func() tea.Msg { return cyclePriorityMsg{id: t.ID} }
				}
			case "v":
				if a.dashboard.tab == tabVerify {
					if t := a.dashboard.selected(); t != nil {
						return a, a.handleVerify(t.ID)
					}
				}
			case "R":
				if a.dashboard.tab == tabReview {
					if t := a.dashboard.selected(); t != nil {
						return a, func() tea.Msg {
							return reviewMsg{id: t.ID, verdict: ticket.ReviewApproved}
						}
					}
				}
			}

		case viewDetail:
			if a.detail.inputActive() {
				break // let detail handle its text input
			}
			switch msg.String() {
			case "esc":
				a.current = a.prevView
				return a, nil
			case "q":
				return a, tea.Quit
			case "p":
				return a, func() tea.Msg { return cyclePriorityMsg{id: a.detail.ticket.ID} }
			case "a":
				a.detail.startInput(inputAssignee)
				return a, nil
			case "n":
				a.detail.startInput(inputNote)
				return a, nil
			case "m":
				a.detail.startMovePicker(a.ticketsDir)
				return a, nil
			case "e":
				a.form = newEditFormModel(a.detail.ticket, a.width, a.height)
				a.prevView = viewDetail
				a.current = viewForm
				return a, nil
			}

		case viewPipeline:
			if a.pipeline.inputActive() {
				break // let pipeline handle filter input
			}
			switch msg.String() {
			case "esc":
				a.current = viewDashboard
				return a, nil
			case "q":
				return a, tea.Quit
			case "c":
				a.form = newFormModel(a.width, a.height)
				a.prevView = viewPipeline
				a.current = viewForm
				return a, nil
			case "enter":
				if t := a.pipeline.selected(); t != nil {
					a.detail = newDetailModel(t, a.width, a.height)
					a.prevView = viewPipeline
					a.current = viewDetail
					return a, nil
				}
			case "A":
				if t := a.pipeline.selected(); t != nil {
					return a, func() tea.Msg { return advanceMsg{id: t.ID} }
				}
			case "F":
				if t := a.pipeline.selected(); t != nil {
					return a, func() tea.Msg { return advanceMsg{id: t.ID, force: true} }
				}
			case "R":
				if t := a.pipeline.selected(); t != nil {
					if t.Review == ticket.ReviewPending {
						return a, func() tea.Msg {
							return reviewMsg{id: t.ID, verdict: ticket.ReviewApproved}
						}
					}
					return a, func() tea.Msg {
						return reviewMsg{id: t.ID, verdict: ticket.ReviewPending}
					}
				}
			case "X":
				if t := a.pipeline.selected(); t != nil {
					return a, func() tea.Msg {
						return reviewMsg{id: t.ID, verdict: ticket.ReviewRejected}
					}
				}
			case "S":
				if t := a.pipeline.selected(); t != nil {
					return a, func() tea.Msg { return skipMsg{id: t.ID} }
				}
			case "p":
				if t := a.pipeline.selected(); t != nil {
					return a, func() tea.Msg { return cyclePriorityMsg{id: t.ID} }
				}
			}
		}
	}

	// Delegate to child models.
	switch a.current {
	case viewDashboard:
		var cmd tea.Cmd
		a.dashboard, cmd = a.dashboard.update(msg)
		return a, cmd
	case viewDetail:
		var cmd tea.Cmd
		a.detail, cmd = a.detail.update(msg)
		return a, cmd
	case viewForm:
		var cmd tea.Cmd
		a.form, cmd = a.form.update(msg)
		return a, cmd
	case viewPipeline:
		var cmd tea.Cmd
		a.pipeline, cmd = a.pipeline.update(msg)
		return a, cmd
	}

	return a, nil
}

func (a App) View() string {
	if a.err != nil {
		return fmt.Sprintf("Error: %v\n", a.err)
	}

	var content string
	switch a.current {
	case viewDashboard:
		content = a.dashboard.view()
	case viewDetail:
		content = a.detail.view()
	case viewForm:
		content = a.form.view()
	case viewPipeline:
		content = a.pipeline.view()
	}

	if a.status != "" {
		// Replace last line with status.
		lines := strings.Split(content, "\n")
		if len(lines) > 0 {
			lines[len(lines)-1] = a.status
		}
		content = strings.Join(lines, "\n")
	}

	return content
}

// Mutation handlers

func (a *App) handleCyclePriority(id string) tea.Cmd {
	t, err := a.store.Get(id)
	if err != nil {
		return func() tea.Msg { return statusMsg("error: " + err.Error()) }
	}

	t.Priority = (t.Priority + 1) % 5
	if err := a.store.Update(t); err != nil {
		return func() tea.Msg { return statusMsg("error: " + err.Error()) }
	}

	msg := fmt.Sprintf("%s -> P%d", id, t.Priority)
	return tea.Batch(
		loadTickets(a.store),
		func() tea.Msg { return statusMsg(msg) },
	)
}

func (a *App) handleSetAssignee(id, assignee string) tea.Cmd {
	t, err := a.store.Get(id)
	if err != nil {
		return func() tea.Msg { return statusMsg("error: " + err.Error()) }
	}

	t.Assignee = assignee
	if err := a.store.Update(t); err != nil {
		return func() tea.Msg { return statusMsg("error: " + err.Error()) }
	}

	msg := fmt.Sprintf("%s assignee -> %s", id, assignee)
	return tea.Batch(
		loadTickets(a.store),
		func() tea.Msg { return statusMsg(msg) },
	)
}

func (a *App) handleAddNote(id, text string) tea.Cmd {
	t, err := a.store.Get(id)
	if err != nil {
		return func() tea.Msg { return statusMsg("error: " + err.Error()) }
	}

	t.Notes = append(t.Notes, ticket.Note{
		Timestamp: time.Now().UTC(),
		Text:      text,
	})

	// Strip existing notes section from body to avoid duplication.
	if idx := strings.Index(t.Body, "\n## Notes\n"); idx >= 0 {
		t.Body = t.Body[:idx+1]
	} else if strings.HasPrefix(t.Body, "## Notes\n") {
		t.Body = "\n"
	}

	if err := a.store.Update(t); err != nil {
		return func() tea.Msg { return statusMsg("error: " + err.Error()) }
	}

	msg := fmt.Sprintf("Note added to %s", id)
	return tea.Batch(
		loadTickets(a.store),
		func() tea.Msg { return statusMsg(msg) },
	)
}

func (a *App) handleAdvance(id string, force bool) tea.Cmd {
	opts := ticket.AdvanceOptions{Force: force}
	result, err := ticket.Advance(a.store, id, opts)
	if err != nil {
		return func() tea.Msg { return statusMsg("error: " + err.Error()) }
	}

	msg := fmt.Sprintf("%s: %s → %s", id, result.From, result.To)
	if len(result.GateErrors) > 0 {
		msg += fmt.Sprintf(" (%d gates overridden)", len(result.GateErrors))
	}
	return tea.Batch(
		loadTickets(a.store),
		func() tea.Msg { return statusMsg(msg) },
	)
}

func (a *App) handleReview(id string, verdict ticket.ReviewState) tea.Cmd {
	reviewer := "tui"
	if err := ticket.SetReview(a.store, id, reviewer, verdict, ""); err != nil {
		return func() tea.Msg { return statusMsg("error: " + err.Error()) }
	}

	msg := fmt.Sprintf("%s: review → %s", id, verdict)
	return tea.Batch(
		loadTickets(a.store),
		func() tea.Msg { return statusMsg(msg) },
	)
}

func (a *App) handleSkip(id string) tea.Cmd {
	t, err := a.store.Get(id)
	if err != nil {
		return func() tea.Msg { return statusMsg("error: " + err.Error()) }
	}

	// Skip to the next-next stage (skip one stage ahead).
	nextStage, ok := ticket.NextStage(t.Type, t.Stage)
	if !ok {
		return func() tea.Msg { return statusMsg(id + ": already at final stage") }
	}
	skipTo, ok := ticket.NextStage(t.Type, nextStage)
	if !ok {
		skipTo = nextStage // Just advance one if can't skip further.
	}

	result, err := ticket.Skip(a.store, id, skipTo, "skipped via TUI")
	if err != nil {
		return func() tea.Msg { return statusMsg("error: " + err.Error()) }
	}

	msg := fmt.Sprintf("%s: %s → %s (skipped %v)", id, result.From, result.To, result.Skipped)
	return tea.Batch(
		loadTickets(a.store),
		func() tea.Msg { return statusMsg(msg) },
	)
}

func (a *App) handleCreateTicket(msg formSubmitMsg) tea.Cmd {
	t := &ticket.Ticket{
		ID:       ticket.GenerateID(msg.title),
		Title:    msg.title,
		Type:     msg.ticketType,
		Priority: msg.priority,
		Assignee: msg.assignee,
		Status:   ticket.StatusOpen,
		Stage:    ticket.StageTriage,
		Created:  time.Now().UTC(),
	}

	if msg.description != "" {
		t.Body = msg.description + "\n"
	}

	if err := a.store.Create(t); err != nil {
		return func() tea.Msg { return statusMsg("error: " + err.Error()) }
	}

	a.current = a.prevView
	status := fmt.Sprintf("Created %s: %s", t.ID, t.Title)
	return tea.Batch(
		loadTickets(a.store),
		func() tea.Msg { return statusMsg(status) },
	)
}

func (a *App) handleEditTicket(msg formSubmitMsg) tea.Cmd {
	t, err := a.store.Get(msg.editID)
	if err != nil {
		return func() tea.Msg { return statusMsg("error: " + err.Error()) }
	}

	t.Title = msg.title
	t.Type = msg.ticketType
	t.Priority = msg.priority
	t.Assignee = msg.assignee
	if msg.stage != "" {
		t.Stage = msg.stage
	}

	// Update description (text before first ## heading).
	t.Body = ticket.UpdateSection(t.Body, "", msg.description)

	// Add note if provided.
	if msg.note != "" {
		t.Notes = append(t.Notes, ticket.Note{
			Timestamp: time.Now().UTC(),
			Text:      msg.note,
		})
		// Strip existing notes section from body to avoid duplication.
		if idx := strings.Index(t.Body, "\n## Notes\n"); idx >= 0 {
			t.Body = t.Body[:idx+1]
		} else if strings.HasPrefix(t.Body, "## Notes\n") {
			t.Body = "\n"
		}
	}

	if err := a.store.Update(t); err != nil {
		return func() tea.Msg { return statusMsg("error: " + err.Error()) }
	}

	a.current = a.prevView
	status := fmt.Sprintf("Updated %s", t.ID)
	return tea.Batch(
		loadTickets(a.store),
		func() tea.Msg { return statusMsg(status) },
	)
}

func (a *App) handleDelete(id string) tea.Cmd {
	if err := a.store.Delete(id); err != nil {
		return func() tea.Msg { return statusMsg("error: " + err.Error()) }
	}

	msg := fmt.Sprintf("Deleted %s", id)
	return tea.Batch(
		loadTickets(a.store),
		func() tea.Msg { return statusMsg(msg) },
	)
}

func (a *App) handleMove(id, targetRepo string) tea.Cmd {
	targetDir := filepath.Join(targetRepo, ".tickets")
	dst := ticket.NewFileStore(targetDir)

	results, err := ticket.MoveTicket(a.store, dst, id, false)
	if err != nil {
		return func() tea.Msg { return statusMsg("error: " + err.Error()) }
	}

	var parts []string
	for _, r := range results {
		parts = append(parts, fmt.Sprintf("%s → %s", r.OldID, r.NewID))
	}
	msg := fmt.Sprintf("Moved %s to %s", strings.Join(parts, ", "), targetRepo)
	a.current = a.prevView
	return tea.Batch(
		loadTickets(a.store),
		func() tea.Msg { return statusMsg(msg) },
	)
}

func (a *App) handleVerify(id string) tea.Cmd {
	// Approve review only — ticket stays at verify until commit/push/close.
	if err := ticket.SetReview(a.store, id, "human:tui", ticket.ReviewApproved, "verified via TUI"); err != nil {
		return func() tea.Msg { return statusMsg("error: " + err.Error()) }
	}

	msg := fmt.Sprintf("%s: verified ✓", id)
	return tea.Batch(
		loadTickets(a.store),
		func() tea.Msg { return statusMsg(msg) },
	)
}
