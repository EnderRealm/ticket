package mcp

import (
	"context"
	"errors"
	"testing"

	"github.com/EnderRealm/ticket/v8/pkg/ticket"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func verifyJobSession(t *testing.T) *mcp.ServerSession {
	t.Helper()
	server := mcp.NewServer(&mcp.Implementation{Name: "verify-test", Version: "1"}, nil)
	st, ct := mcp.NewInMemoryTransports()
	owner, err := server.Connect(context.Background(), st, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { owner.Close() })
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	session, err := client.Connect(context.Background(), ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Close() })
	return owner
}

func TestVerifyJobRunnerErrorDoesNotRecord(t *testing.T) {
	jobs := newVerifyJobs()
	j, err := jobs.start(verifyJobSession(t), "alpha/example", ticket.VerifyProvenance{}, func(context.Context) (ticket.VerifyReport, string, error) {
		return ticket.VerifyReport{}, "", errors.New("checkout disappeared")
	}, func(string) error {
		t.Error("runner failure must not record results")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	<-j.done
	result := j.snapshot()
	if result.State != "failed" || result.Error != "checkout disappeared" || result.Report != nil {
		t.Fatalf("result = %+v", result)
	}
}

func TestVerifyCancelAfterCommandsFinishKeepsRecording(t *testing.T) {
	jobs := newVerifyJobs()
	recording, release := make(chan struct{}), make(chan struct{})
	j, err := jobs.start(verifyJobSession(t), "alpha/example", ticket.VerifyProvenance{}, func(context.Context) (ticket.VerifyReport, string, error) {
		return ticket.VerifyReport{ID: "alpha/example", OK: true}, "full result", nil
	}, func(record string) error {
		close(recording)
		<-release
		if record != "full result" {
			t.Errorf("record = %q", record)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	<-recording
	j.stop()
	if result := j.snapshot(); result.State != "recording" {
		t.Errorf("record in flight misreported: %+v", result)
	}
	close(release)
	<-j.done
	if result := j.snapshot(); result.State != "completed" || result.Report == nil || !result.Report.OK {
		t.Fatalf("complete result lost after cancellation: %+v", result)
	}
}

func TestVerifyJobRetentionEvictsFinishedResultsWithoutReplay(t *testing.T) {
	jobs := newVerifyJobs()
	owner := verifyJobSession(t)
	runs := 0
	var first, last *verifyJob
	for range verifyJobLimit + 1 {
		var err error
		last, err = jobs.start(owner, "alpha/example", ticket.VerifyProvenance{}, func(context.Context) (ticket.VerifyReport, string, error) {
			runs++
			return ticket.VerifyReport{}, "", nil
		}, func(string) error { return nil })
		if err != nil {
			t.Fatal(err)
		}
		if first == nil {
			first = last
		}
		<-last.done
	}
	if _, err := jobs.lookup(owner, nil, verifyLookupArgs{VerificationID: first.snapshot().VerificationID}); err == nil {
		t.Fatal("oldest result was not evicted")
	}
	if _, err := jobs.lookup(owner, nil, verifyLookupArgs{VerificationID: last.snapshot().VerificationID}); err != nil {
		t.Fatal(err)
	}
	if runs != verifyJobLimit+1 {
		t.Fatalf("lookup replayed a command: %d runs", runs)
	}
}
