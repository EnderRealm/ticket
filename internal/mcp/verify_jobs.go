package mcp

import (
	"context"
	"crypto/rand"
	"fmt"
	"sync"
	"time"

	"github.com/EnderRealm/ticket/v8/pkg/ticket"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Keep every command wait below common MCP request deadlines. This is not a
// command timeout; only the host's VerifyPolicy bounds command execution.
const verifyResponseWait = 10 * time.Second
const verifyJobLimit = 128

type verifyJobResult struct {
	ID             string               `json:"id"`
	VerificationID string               `json:"verification_id"`
	State          string               `json:"state"`
	Report         *ticket.VerifyReport `json:"report,omitempty"`
	Error          string               `json:"error,omitempty"`
}

type verifyJob struct {
	mu     sync.Mutex
	result verifyJobResult
	owner  *mcp.ServerSession
	seq    uint64
	cancel context.CancelFunc
	done   chan struct{}
}

func (j *verifyJob) snapshot() verifyJobResult {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.result
}

func (j *verifyJob) stop() {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.result.State == "running" {
		j.result.State = "cancelling"
		j.cancel()
	}
	// Once recording begins, the complete result is committed. Cancellation
	// cannot retract a store write or leave its outcome ambiguous.
}

func (j *verifyJob) response(ctx context.Context, quickReport bool) (*mcp.CallToolResult, any, error) {
	timer := time.NewTimer(verifyResponseWait)
	defer timer.Stop()
	select {
	case <-j.done:
	case <-timer.C:
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	}
	result := j.snapshot()
	// Preserve the established ticket_verify response for short runs. Polling
	// always returns the lifecycle envelope, including the same report data.
	if quickReport && result.State == "completed" {
		r, err := jsonResult(result.Report)
		return r, nil, err
	}
	r, err := jsonResult(result)
	return r, nil, err
}

type verifyJobs struct {
	mu       sync.Mutex
	jobs     map[string]*verifyJob
	active   map[string]*verifyJob
	sessions map[*mcp.ServerSession]bool
	seq      uint64
	closing  bool
}

func newVerifyJobs() *verifyJobs {
	return &verifyJobs{
		jobs: make(map[string]*verifyJob), active: make(map[string]*verifyJob),
		sessions: make(map[*mcp.ServerSession]bool),
	}
}

func (v *verifyJobs) start(owner *mcp.ServerSession, id string,
	run func(context.Context) (ticket.VerifyReport, string, error), record func(string) error,
) (*verifyJob, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.closing {
		return nil, fmt.Errorf("verification server is shutting down")
	}
	if j := v.active[id]; j != nil {
		if j.owner != owner {
			return nil, fmt.Errorf("%s already has a verification in another MCP session", id)
		}
		return j, nil
	}
	// Bound retained results and concurrent work together. Eviction never
	// cancels active work; polling an evicted ID is an error, never a replay.
	if len(v.jobs) >= verifyJobLimit {
		var oldest *verifyJob
		for _, j := range v.jobs {
			select {
			case <-j.done:
				if oldest == nil || j.seq < oldest.seq {
					oldest = j
				}
			default:
			}
		}
		if oldest == nil {
			return nil, fmt.Errorf("verification capacity reached; wait for or cancel an active job")
		}
		delete(v.jobs, oldest.result.VerificationID)
	}
	ctx, cancel := context.WithCancel(context.Background())
	v.seq++
	j := &verifyJob{
		result: verifyJobResult{ID: id, VerificationID: rand.Text(), State: "running"},
		owner:  owner, seq: v.seq, cancel: cancel, done: make(chan struct{}),
	}
	v.jobs[j.result.VerificationID] = j
	v.active[id] = j
	if !v.sessions[owner] {
		v.sessions[owner] = true
		go v.disconnect(owner)
	}
	go func() {
		defer cancel()
		report, text, err := run(ctx)
		j.mu.Lock()
		switch {
		case ctx.Err() != nil:
			j.result.State = "cancelled"
		case err != nil:
			j.result.State = "failed"
			j.result.Error = err.Error()
		default:
			j.result.State = "recording"
		}
		recording := j.result.State == "recording"
		j.mu.Unlock()
		if recording {
			if err := record(text); err != nil {
				report.RecordError = fmt.Sprintf("failed to record verify results: %v", err)
			}
			j.mu.Lock()
			j.result.Report = &report
			j.result.State = "completed"
			j.mu.Unlock()
		}
		v.mu.Lock()
		delete(v.active, id)
		close(j.done)
		v.mu.Unlock()
	}()
	return j, nil
}

func (v *verifyJobs) shutdown() {
	v.mu.Lock()
	v.closing = true
	pending := make([]*verifyJob, 0, len(v.active))
	for _, j := range v.active {
		j.stop()
		pending = append(pending, j)
	}
	v.mu.Unlock()
	for _, j := range pending {
		<-j.done
	}
}

func (v *verifyJobs) disconnect(owner *mcp.ServerSession) {
	_ = owner.Wait()
	v.mu.Lock()
	defer v.mu.Unlock()
	for id, j := range v.jobs {
		if j.owner == owner {
			j.stop()
			delete(v.jobs, id)
		}
	}
	delete(v.sessions, owner)
	// Active entries remain until their workers exit, so another session cannot
	// launch the same ticket while the cancelled process is still stopping.
}

type verifyLookupArgs struct {
	VerificationID string `json:"verification_id,omitempty" jsonschema:"job ID returned by ticket_verify; provide this or id"`
	ID             string `json:"id,omitempty" jsonschema:"ticket ID to recover this session's latest job when the start response was lost; provide this or verification_id"`
}

func (v *verifyJobs) lookup(owner *mcp.ServerSession, store ticket.Store, args verifyLookupArgs) (*verifyJob, error) {
	if (args.ID == "") == (args.VerificationID == "") {
		return nil, fmt.Errorf("provide exactly one of id or verification_id")
	}
	if args.ID != "" {
		t, err := store.Get(args.ID)
		if err != nil {
			return nil, fmt.Errorf("ticket not found: %w", err)
		}
		args.ID = t.ID
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	var found *verifyJob
	for key, j := range v.jobs {
		if j.owner == owner && (key == args.VerificationID || j.result.ID == args.ID) {
			if found == nil || j.seq > found.seq {
				found = j
			}
		}
	}
	if found == nil {
		return nil, fmt.Errorf("verification not found in this MCP session (unknown, expired, or server restarted); no commands were run")
	}
	return found, nil
}

func registerVerifyLifecycle(server *mcp.Server, store ticket.Store, jobs *verifyJobs) {
	addFlexTool(server, &mcp.Tool{
		Name:        "ticket_verify_status",
		Description: "Retrieve this MCP session's verification by verification_id, or its latest run by ticket id if a start response was lost. Waits at most 10 seconds. Poll while state is running, cancelling or recording. completed carries the exact report (including failed/refused/unverified criteria and record_error); failed carries an error; cancelled has no report and does not overwrite Test Results. This tool never starts commands. Jobs are held in memory until session disconnect or server restart; the server retains at most 128 jobs and evicts the oldest finished jobs. Unknown/expired IDs are errors, not passes.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args verifyLookupArgs) (*mcp.CallToolResult, any, error) {
		j, err := jobs.lookup(req.Session, store, args)
		if err != nil {
			r, _ := errResult("%v", err)
			return r, nil, nil
		}
		return j.response(ctx, false)
	})
	addFlexTool(server, &mcp.Tool{
		Name:        "ticket_verify_cancel",
		Description: "Cancel this MCP session's verification by verification_id, or its latest run by ticket id. Repeated cancellation is safe. Returns state immediately; poll ticket_verify_status until terminal. Only running commands can be cancelled: recording/completed already have full results and finish recording them. Cancellation does not replace Test Results with partial failures. Request timeout alone does not cancel a job; disconnect does.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args verifyLookupArgs) (*mcp.CallToolResult, any, error) {
		j, err := jobs.lookup(req.Session, store, args)
		if err != nil {
			r, _ := errResult("%v", err)
			return r, nil, nil
		}
		j.stop()
		r, err := jsonResult(j.snapshot())
		return r, nil, err
	})
}
