package cmd

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"reflect"
	"testing"

	"github.com/EnderRealm/ticket/v8/internal/project"
	"github.com/EnderRealm/ticket/v8/pkg/ticket"
)

func captureVerifyPolicy(t *testing.T) (ticket.VerifyPolicyReport, string, error) {
	t.Helper()
	verifyCmd.SetContext(context.Background())
	jsonOutput, verifyPolicy = true, true
	defer func() { jsonOutput, verifyPolicy = false, false }()

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	err := runVerify(verifyCmd, nil)
	w.Close()
	os.Stdout = oldStdout
	out, _ := io.ReadAll(r)

	var report ticket.VerifyPolicyReport
	if err == nil {
		if jerr := json.Unmarshal(out, &report); jerr != nil {
			t.Fatalf("json parse: %v\noutput: %s", jerr, out)
		}
	}
	return report, string(out), err
}

func TestVerifyPolicyJSON(t *testing.T) {
	verifyStore(t, "vf-1234", mixedCriteriaBody)
	cfg, err := project.Load()
	if err != nil {
		t.Fatal(err)
	}
	p := cfg.Projects["vf-verify"]
	p.VerifyTimeout = "5m"
	cfg.Projects["vf-verify"] = p
	if err := project.Save(cfg); err != nil {
		t.Fatal(err)
	}

	report, out, err := captureVerifyPolicy(t)
	if err != nil {
		t.Fatal(err)
	}
	if report.Allow == nil || !reflect.DeepEqual(*report.Allow, []string{"/bin/echo", "/bin/sh"}) {
		t.Errorf("allow = %v, want the configured list\n%s", report.Allow, out)
	}
	if report.Timeout != "5m0s" || report.TimeoutError != "" {
		t.Errorf("timeout = %q (%q), want 5m0s", report.Timeout, report.TimeoutError)
	}

	// Unset: the allow-list falls to defaults and the bound to RunVerify's.
	cfg.VerifyAllow = nil
	p.VerifyTimeout = ""
	cfg.Projects["vf-verify"] = p
	if err := project.Save(cfg); err != nil {
		t.Fatal(err)
	}
	report, _, err = captureVerifyPolicy(t)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := project.VerifyAllow()
	if report.Allow == nil || !reflect.DeepEqual(*report.Allow, want) || len(want) == 0 {
		t.Errorf("allow = %v, want defaults %v", report.Allow, want)
	}
	if report.Timeout != ticket.DefaultVerifyTimeout.String() {
		t.Errorf("timeout = %q, want the default", report.Timeout)
	}
}

func TestVerifyPolicyUnparseableConfig(t *testing.T) {
	verifyStore(t, "vf-1234", mixedCriteriaBody)
	path, err := project.ConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("verify_allow: [unclosed\n\t: :"), 0o600); err != nil {
		t.Fatal(err)
	}

	report, out, err := captureVerifyPolicy(t)
	if err != nil {
		t.Fatal(err)
	}
	if report.Allow != nil || report.AllowError == "" {
		t.Errorf("allow = %v, error = %q, want the error and no list\n%s", report.Allow, report.AllowError, out)
	}
	if report.Timeout != "" || report.TimeoutError == "" {
		t.Errorf("timeout = %q, error = %q, want the error and no bound", report.Timeout, report.TimeoutError)
	}
}
