package ticket

// StatusToStage maps legacy status values to their pipeline stage equivalents.
var StatusToStage = map[Status]Stage{
	StatusOpen:         StageTriage,
	StatusInProgress:   StageImplement,
	StatusNeedsTesting: StageTest,
	StatusClosed:       StageDone,
}

// NeedsMigration reports whether a ticket uses the legacy status field
// without a stage field.
func NeedsMigration(t *Ticket) bool {
	return t.Status != "" && t.Stage == ""
}

// MigrateTicket converts a status-based ticket to stage-based in memory.
// Idempotent — tickets that already have a stage are returned unchanged.
// Does not persist — caller must write the ticket back to the store.
func MigrateTicket(t *Ticket) bool {
	if !NeedsMigration(t) {
		return false
	}

	stage, ok := StatusToStage[t.Status]
	if !ok {
		// Unknown status — default to triage.
		stage = StageTriage
	}

	t.Stage = stage
	t.Status = "" // Status is deprecated; clear after migration.
	return true
}

// MigrateAll migrates all tickets in the store from status to stage.
// Returns the count of tickets migrated.
func MigrateAll(store *FileStore) (int, error) {
	tickets, err := store.List()
	if err != nil {
		return 0, err
	}

	count := 0
	for _, t := range tickets {
		if MigrateTicket(t) {
			if err := store.Update(t); err != nil {
				return count, err
			}
			count++
		}
	}
	return count, nil
}
