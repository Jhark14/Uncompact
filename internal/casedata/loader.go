// Package casedata reads litigation case data from SQLite databases
// (case_registry.db and ops.db) on the local VM for the case-bomb subcommand.
package casedata

import (
	"database/sql"
	"fmt"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// CaseTimeline aggregates all data needed for a case context bomb.
type CaseTimeline struct {
	Case       CaseInfo
	Deadlines  []DeadlineEvent
	Followups  []FollowupEvent
	Workflow   WorkflowState
	Milestones []MilestoneEvent
	EService   []EServiceEvent
	AuditTrail []AuditEntry
	RecentChat []ChatMessage
}

type CaseInfo struct {
	FileNumber       string
	CaseName         string
	CaseNumber       string
	CaseType         string
	Status           string
	ClientName       string
	OpposingParty    string
	LeadAttorney     string
	AssignedParalegal string
	SOLDate          string
	DateOfLoss       string
	Court            string
	Judge            string
	DateOpened       string
}

type DeadlineEvent struct {
	DueDate      string
	Type         string
	Description  string
	Status       string
	AssignedTo   string
}

type FollowupEvent struct {
	SentDate             string
	SentTo               string
	Subject              string
	ExpectedResponseDate string
	Status               string
	FollowUpCount        int
	EscalationLevel      string
}

type WorkflowState struct {
	CurrentStage string
	Tasks        []WorkflowTask
}

type WorkflowTask struct {
	TaskCode  string
	Title     string
	StageKey  string
	Status    string
	DueDate   string
}

type MilestoneEvent struct {
	Name          string
	ScheduledDate string
	DueDate       string
	Status        string
	Role          string
	AssignedTo    string
	Deliverable   string
}

type EServiceEvent struct {
	ReceivedAt    string
	DocType       string
	PDFFilename   string
	StagingStatus string
	Subject       string
}

type AuditEntry struct {
	Timestamp string
	Action    string
	FromState string
	ToState   string
	Details   string
	MatterID  string
}

type ChatMessage struct {
	Role      string
	Content   string
	Timestamp string
	ToolsUsed string
}

// LoadTimeline reads and aggregates all case data from the SQLite databases.
func LoadTimeline(dbDir string, caseID string, lastMessages int) (*CaseTimeline, error) {
	caseDB := filepath.Join(dbDir, "case_registry.db")
	opsDB := filepath.Join(dbDir, "ops.db")

	timeline := &CaseTimeline{}

	// Load from case_registry.db
	regConn, err := sql.Open("sqlite", caseDB+"?mode=ro")
	if err != nil {
		return nil, fmt.Errorf("open case_registry.db: %w", err)
	}
	defer regConn.Close()

	if err := loadCaseInfo(regConn, caseID, timeline); err != nil {
		return nil, fmt.Errorf("load case info: %w", err)
	}
	loadDeadlines(regConn, caseID, timeline)
	loadFollowups(regConn, caseID, timeline)
	loadWorkflow(regConn, caseID, timeline)
	loadMilestones(regConn, caseID, timeline)
	loadAuditTrail(regConn, caseID, timeline)

	// Load from ops.db
	opsConn, err := sql.Open("sqlite", opsDB+"?mode=ro")
	if err != nil {
		// ops.db may not exist — not fatal
		return timeline, nil
	}
	defer opsConn.Close()

	loadEService(opsConn, caseID, timeline)
	loadRecentChat(opsConn, caseID, lastMessages, timeline)

	return timeline, nil
}

func loadCaseInfo(db *sql.DB, caseID string, t *CaseTimeline) error {
	row := db.QueryRow(`
		SELECT file_number, case_name, COALESCE(case_number,''), COALESCE(case_type,''),
		       COALESCE(status,''), COALESCE(client_name,''), COALESCE(opposing_party,''),
		       COALESCE(lead_attorney,''), COALESCE(assigned_paralegal,''),
		       COALESCE(sol_date,''), COALESCE(date_of_loss,''),
		       COALESCE(court,''), COALESCE(judge,''), COALESCE(date_opened,'')
		FROM cases WHERE file_number = ?`, caseID)

	return row.Scan(
		&t.Case.FileNumber, &t.Case.CaseName, &t.Case.CaseNumber, &t.Case.CaseType,
		&t.Case.Status, &t.Case.ClientName, &t.Case.OpposingParty,
		&t.Case.LeadAttorney, &t.Case.AssignedParalegal,
		&t.Case.SOLDate, &t.Case.DateOfLoss,
		&t.Case.Court, &t.Case.Judge, &t.Case.DateOpened,
	)
}

func loadDeadlines(db *sql.DB, caseID string, t *CaseTimeline) {
	rows, err := db.Query(`
		SELECT COALESCE(due_date,''), COALESCE(deadline_type,''), COALESCE(description,''),
		       COALESCE(status,''), COALESCE(assigned_to,'')
		FROM deadlines WHERE case_id = ? ORDER BY due_date ASC`, caseID)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var d DeadlineEvent
		if rows.Scan(&d.DueDate, &d.Type, &d.Description, &d.Status, &d.AssignedTo) == nil {
			t.Deadlines = append(t.Deadlines, d)
		}
	}
}

func loadFollowups(db *sql.DB, caseID string, t *CaseTimeline) {
	rows, err := db.Query(`
		SELECT COALESCE(sent_date,''), COALESCE(sent_to_name, sent_to, ''),
		       COALESCE(subject,''), COALESCE(expected_response_date,''),
		       COALESCE(status,''), COALESCE(follow_up_count,0), COALESCE(escalation_level,'none')
		FROM followups WHERE case_id = ? ORDER BY sent_date DESC`, caseID)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var f FollowupEvent
		if rows.Scan(&f.SentDate, &f.SentTo, &f.Subject, &f.ExpectedResponseDate,
			&f.Status, &f.FollowUpCount, &f.EscalationLevel) == nil {
			t.Followups = append(t.Followups, f)
		}
	}
}

func loadWorkflow(db *sql.DB, caseID string, t *CaseTimeline) {
	row := db.QueryRow(`SELECT COALESCE(current_stage,'') FROM ea_case_workflow WHERE case_id = ?`, caseID)
	if row.Scan(&t.Workflow.CurrentStage) != nil {
		return
	}

	rows, err := db.Query(`
		SELECT COALESCE(task_code,''), COALESCE(title,''), COALESCE(stage_key,''),
		       COALESCE(status,''), COALESCE(due_date,'')
		FROM ea_case_tasks WHERE case_id = ? ORDER BY id ASC`, caseID)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var wt WorkflowTask
		if rows.Scan(&wt.TaskCode, &wt.Title, &wt.StageKey, &wt.Status, &wt.DueDate) == nil {
			t.Workflow.Tasks = append(t.Workflow.Tasks, wt)
		}
	}
}

func loadMilestones(db *sql.DB, caseID string, t *CaseTimeline) {
	rows, err := db.Query(`
		SELECT mm.name, mm.scheduled_date, mm.due_date, mm.status, mm.role,
		       COALESCE(mm.assigned_to,''), COALESCE(mm.deliverable,'')
		FROM matter_milestones mm
		JOIN matters m ON mm.matter_id = m.matter_id
		WHERE m.case_id = ?
		ORDER BY mm.scheduled_date ASC`, caseID)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var ms MilestoneEvent
		if rows.Scan(&ms.Name, &ms.ScheduledDate, &ms.DueDate, &ms.Status,
			&ms.Role, &ms.AssignedTo, &ms.Deliverable) == nil {
			t.Milestones = append(t.Milestones, ms)
		}
	}
}

func loadEService(db *sql.DB, caseID string, t *CaseTimeline) {
	rows, err := db.Query(`
		SELECT COALESCE(received_at,''), COALESCE(doc_type,''), COALESCE(pdf_filename,''),
		       COALESCE(staging_status,''), COALESCE(subject,'')
		FROM eservice_staging
		WHERE matched_file_number = ? OR matched_case_id = ?
		ORDER BY received_at DESC LIMIT 20`, caseID, caseID)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var e EServiceEvent
		if rows.Scan(&e.ReceivedAt, &e.DocType, &e.PDFFilename, &e.StagingStatus, &e.Subject) == nil {
			t.EService = append(t.EService, e)
		}
	}
}

func loadAuditTrail(db *sql.DB, caseID string, t *CaseTimeline) {
	rows, err := db.Query(`
		SELECT COALESCE(timestamp,''), COALESCE(action,''), COALESCE(from_state,''),
		       COALESCE(to_state,''), COALESCE(details,''), COALESCE(matter_id,'')
		FROM cmso_audit_trail
		WHERE matter_id IN (SELECT matter_id FROM matters WHERE case_id = ?)
		ORDER BY timestamp DESC LIMIT 30`, caseID)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var a AuditEntry
		if rows.Scan(&a.Timestamp, &a.Action, &a.FromState, &a.ToState, &a.Details, &a.MatterID) == nil {
			t.AuditTrail = append(t.AuditTrail, a)
		}
	}
}

func loadRecentChat(db *sql.DB, caseID string, limit int, t *CaseTimeline) {
	// Chat messages are now in Cosmos DB, not SQLite.
	// This function is a no-op placeholder for when we add a local cache.
	_ = db
	_ = caseID
	_ = limit
}
