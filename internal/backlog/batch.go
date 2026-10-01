// Package backlog is the Backlog tool's bulk writes: a selection of
// tickets and one action, previewed row by row and applied through the
// Jira client's gate.
//
// It follows the sprint report's close-out. A preview is held in memory
// under a random id for fifteen minutes with a digest of what it will
// write; an apply takes it once, re-reads every ticket first, and leaves
// one audit row per ticket attempted; a reversal is a new preview built
// from those rows. The shapes are spelled out again here rather than
// shared with the sprint package because the two propose from different
// things - a report there, a selection here - and a change set built for
// one would carry fields that mean nothing to the other.
package backlog

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Tzomily-Anvar/argus/internal/config"
	"github.com/Tzomily-Anvar/argus/internal/jira"
	"github.com/Tzomily-Anvar/argus/internal/store"
)

// The actions a batch performs. They are what the preview names, what
// the audit row records as its operation, and what the bulk bar offers.
const (
	ActionSprintAssign      = "sprint.assign"
	ActionEpicSet           = "epic.set"
	ActionStoryLink         = "story.link"
	ActionLabelsAdd         = "labels.add"
	ActionLabelsRemove      = "labels.remove"
	ActionOperationsLabel   = "operations.label"
	ActionOperationsMigrate = "operations.migrate"
	ActionIssueDelete       = "issue.delete"
)

// PreviewLifetime is how long a preview stays valid, the same fifteen
// minutes as a sprint change set and for the same reason.
const PreviewLifetime = 15 * time.Minute

// The refusals a preview or an apply makes. Each leaves the preview held,
// except that one already taken is gone by design.
var (
	ErrWritesDisabled  = errors.New("writes to Jira are off for this deployment; set ARGUS_SPRINT_ALLOW_WRITES to switch them on")
	ErrDeletesDisabled = errors.New("deleting tickets is off for this deployment; set ARGUS_BACKLOG_ALLOW_DELETE, on top of ARGUS_SPRINT_ALLOW_WRITES, to switch it on")
	ErrExpired         = errors.New("that preview has expired or was already applied; build it again")
	ErrDigestMismatch  = errors.New("that is not the preview the server built; build it again")
	ErrConfirm         = errors.New("type the number of tickets the preview says will be deleted to confirm")
	ErrIrreversible    = errors.New("a delete cannot be reversed; the tickets are gone from Jira")
	ErrIsReversal      = errors.New("that batch is itself a reversal; run the original action again instead")
	ErrNoWrites        = errors.New("no applied writes are recorded for that batch")
	ErrUnknownAction   = errors.New("that is not an action the backlog tool performs")
	ErrInvalid         = errors.New("the request cannot be previewed")
)

// BatchConfig is what the batch service needs beyond a client and a store.
type BatchConfig struct {
	// Actor is the account any write is made as, named on every audit row.
	Actor string

	// EnableWrites is ARGUS_SPRINT_ALLOW_WRITES and AllowDelete is
	// ARGUS_BACKLOG_ALLOW_DELETE, each read once by the caller. NewBatch
	// opens the client's gate from them, so nothing consults a setting
	// per request.
	EnableWrites bool
	AllowDelete  bool

	// OperationsLabel is the one label ever added for the operations
	// team, and LegacyLabels the ones a migration removes. Left empty,
	// the label is Operations and there is nothing to migrate.
	OperationsLabel string
	LegacyLabels    []string

	// StoryLinkTypes are the link types that tie a Story to its work; the
	// first is the one written. StoryLinkChild says which end the child
	// sits at. ContainerTypes are the types that are never linked as a
	// child; an Epic never is, whatever this says. Left empty, NewBatch
	// reads the settings.
	StoryLinkTypes []string
	StoryLinkChild string
	ContainerTypes []string

	// SprintField pins the sprint custom field id; left empty it is
	// resolved by SprintFieldName on first use. PointsField is handed to
	// the gate when writes are opened, for the sprint report's benefit;
	// this tool never writes points.
	SprintField     string
	SprintFieldName string
	PointsField     string
}

// BatchRequest is what the bulk bar sends: one action over a selection,
// with the parameters that action takes.
type BatchRequest struct {
	Action string         `json:"action"`
	Keys   []string       `json:"keys"`
	Params map[string]any `json:"params,omitempty"`
}

// Guard is what makes an apply provably the thing that was previewed:
// the issue's id, and its updated timestamp at preview time. Any edit by
// anybody moves the latter, and the row is skipped rather than written.
type Guard struct {
	IssueID string    `json:"issue_id"`
	Updated time.Time `json:"updated"`
}

// BatchRow is one ticket in a preview: what it holds, what it will hold, or
// why it is left alone.
type BatchRow struct {
	Key     string `json:"key"`
	Summary string `json:"summary"`
	Type    string `json:"type"`
	Before  string `json:"before"`
	After   string `json:"after"`
	Skipped string `json:"skipped,omitempty"`
	Guard   Guard  `json:"guard"`

	// plan is the request this row becomes. Not sent to the browser:
	// the apply works from the held copy, never from what came back.
	plan plan
}

// BatchPreview is a held proposal. Allowed and DeleteAllowed say what the
// deployment would let an apply do, so the bar can show the setting that
// stands in the way rather than a button that fails.
type BatchPreview struct {
	ID        string     `json:"id"`
	Digest    string     `json:"digest"`
	Action    string     `json:"action"`
	Reverses  string     `json:"reverses,omitempty"` // the batch this one undoes
	Rows      []BatchRow `json:"rows"`
	Changes   int        `json:"changes"`
	Skipped   int        `json:"skipped"`
	ExpiresAt time.Time  `json:"expires_at"`

	Allowed       bool `json:"allowed"`
	DeleteAllowed bool `json:"delete_allowed"`
	Irreversible  bool `json:"irreversible"`

	params map[string]any
}

// Batch holds previews and applies them.
type Batch struct {
	client *jira.Client
	store  store.Store
	cfg    BatchConfig

	mu          sync.Mutex
	sprintField string
	previews    map[string]BatchPreview
	now         func() time.Time
}

// NewBatch reads the settings a caller left unset and opens the client's
// gate as the configuration says. Called at startup, before the sprint
// service has resolved its fields: the points field stored here is the
// pinned one, and the sprint service replaces it with the board's on its
// first sweep, which is the value that matters for points.
func NewBatch(client *jira.Client, st store.Store, cfg BatchConfig) *Batch {
	if cfg.OperationsLabel == "" {
		cfg.OperationsLabel = "Operations"
	}
	if len(cfg.StoryLinkTypes) == 0 {
		cfg.StoryLinkTypes = config.JiraStoryLinkTypes()
	}
	if cfg.StoryLinkChild == "" {
		cfg.StoryLinkChild = config.BacklogStoryLinkChild()
	}
	if len(cfg.ContainerTypes) == 0 {
		cfg.ContainerTypes = config.Strings("ARGUS_JIRA_CONTAINER_TYPES", []string{"Story"})
	}
	if cfg.SprintField == "" {
		cfg.SprintField = config.JiraSprintField()
	}
	if cfg.SprintFieldName == "" {
		cfg.SprintFieldName = config.JiraSprintFieldName()
	}
	if cfg.EnableWrites {
		client.AllowWrites(cfg.PointsField)
	}
	if cfg.AllowDelete {
		client.AllowDeletes()
	}
	return &Batch{
		client: client, store: st, cfg: cfg,
		sprintField: cfg.SprintField,
		previews:    map[string]BatchPreview{},
		now:         func() time.Time { return time.Now().UTC() },
	}
}

// Preview reads every ticket in the selection and says, row by row, what
// the action would do to it. Nothing is written. A ticket that cannot be
// read fails the whole preview, because a table with a row missing is a
// table the person would approve without having seen it.
func (b *Batch) Preview(ctx context.Context, req BatchRequest) (BatchPreview, error) {
	sp, err := b.parse(req)
	if err != nil {
		return BatchPreview{}, err
	}
	sprintField, err := b.resolveSprintField()
	if err != nil {
		return BatchPreview{}, err
	}
	if sp.action == ActionSprintAssign {
		// The name is what the preview shows; the id is what is written.
		// Reading it also says, before any table is drawn, that the
		// sprint exists.
		s, err := b.client.SprintByID(sp.sprintID)
		if err != nil {
			return BatchPreview{}, fmt.Errorf("reading sprint %d: %w", sp.sprintID, err)
		}
		sp.sprintName = s.Name
	}
	rows := make([]BatchRow, 0, len(req.Keys))
	for _, key := range req.Keys {
		is, err := b.client.GetIssue(key, b.fields(sprintField))
		if err != nil {
			return BatchPreview{}, fmt.Errorf("reading %s: %w", key, err)
		}
		rows = append(rows, b.row(sp, is, sprintField))
	}
	return b.hold(BatchPreview{
		Action: sp.action, Rows: rows, params: req.Params,
		Irreversible: sp.action == ActionIssueDelete,
	}), nil
}

// fields is what a preview and a reversal read on each ticket: enough
// to show it, to say what it holds now, and to guard the write.
func (b *Batch) fields(sprintField string) []string {
	return []string{"summary", "issuetype", "updated", "labels", "parent", "issuelinks", sprintField}
}

// resolveSprintField is the pinned id or the name lookup, once.
func (b *Batch) resolveSprintField() (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.sprintField != "" {
		return b.sprintField, nil
	}
	id, err := b.client.FieldID(b.cfg.SprintFieldName)
	if err != nil {
		return "", fmt.Errorf("resolving the %s field: %w", b.cfg.SprintFieldName, err)
	}
	if id == "" {
		return "", fmt.Errorf("no field named %s on this Jira site", b.cfg.SprintFieldName)
	}
	b.sprintField = id
	return id, nil
}

// invalid wraps a reason a request cannot be previewed, so a handler can
// answer 400 for the lot and still say which.
func invalid(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, a...))
}

func joinLabels(labels []string) string { return strings.Join(labels, ", ") }
