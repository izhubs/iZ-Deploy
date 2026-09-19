package diagnostics

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/izhubs/izdeploy/pkg/contract"
)

func TestProblemDetails_JSONSerialization(t *testing.T) {
	prob := NewProblem("urn:izdeploy:error:test", "Test Problem", 400, "Details here", true, "ERR_TEST")

	data := prob.JSON()
	var unmarshaled ProblemDetails
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("failed unmarshaling ProblemDetails JSON: %v", err)
	}

	if unmarshaled.Status != 400 || !unmarshaled.AgentActionable || unmarshaled.ErrorCode != "ERR_TEST" {
		t.Errorf("unexpected deserialized problem values: %+v", unmarshaled)
	}
}

func TestTruncateDetail_TokenBudget(t *testing.T) {
	longString := strings.Repeat("A", 1000)
	truncated := TruncateDetail(longString)

	if len(truncated) > MaxDetailLength {
		t.Errorf("detail length %d exceeded MaxDetailLength %d", len(truncated), MaxDetailLength)
	}

	if !strings.HasSuffix(truncated, "...") {
		t.Errorf("expected truncated string to end with '...', got %s", truncated[len(truncated)-5:])
	}
}

func TestAnalyzeExitCode_OOM(t *testing.T) {
	prob := AnalyzeExitCode(137, "Killed")
	if prob.AgentActionable {
		t.Errorf("expected agent_actionable=false for OOM 137, got true")
	}
	if prob.ErrorCode != CodeContainerOOM {
		t.Errorf("expected error code %s, got %s", CodeContainerOOM, prob.ErrorCode)
	}
}

func TestAnalyzeExitCode_PortClash(t *testing.T) {
	prob := AnalyzeExitCode(1, "listen tcp 0.0.0.0:3000: bind: address already in use")
	if prob.AgentActionable {
		t.Errorf("expected agent_actionable=false for port clash, got true")
	}
	if prob.ErrorCode != CodePortConflict {
		t.Errorf("expected error code %s, got %s", CodePortConflict, prob.ErrorCode)
	}
}

func TestAnalyzeExitCode_SyntaxError(t *testing.T) {
	prob := AnalyzeExitCode(1, "SyntaxError: Unexpected token '{' in index.js:14")
	if !prob.AgentActionable {
		t.Errorf("expected agent_actionable=true for syntax error, got false")
	}
	if prob.ErrorCode != CodeAppCrash {
		t.Errorf("expected error code %s, got %s", CodeAppCrash, prob.ErrorCode)
	}
}

func TestAnalyzeExitCode_MissingBinary(t *testing.T) {
	prob := AnalyzeExitCode(127, "/entrypoint.sh: line 3: node: command not found")
	if !prob.AgentActionable {
		t.Errorf("expected agent_actionable=true for missing binary exit 127, got false")
	}
}

func TestClassifyError_ContractValidation(t *testing.T) {
	valErr := contract.ValidationError{Field: "port", Reason: "must be > 0"}
	prob := ClassifyError(valErr)

	if !prob.AgentActionable {
		t.Errorf("expected agent_actionable=true for contract validation error, got false")
	}
	if prob.Status != 400 {
		t.Errorf("expected status 400, got %d", prob.Status)
	}
}

func TestClassifyError_LockfileMismatch(t *testing.T) {
	err := errors.New("infrastructure parameters modified without lockfile update: expected infra_hash=123, actual=456")
	prob := ClassifyError(err)

	if prob.AgentActionable {
		t.Errorf("expected agent_actionable=false for lockfile mismatch, got true")
	}
	if prob.Status != 422 {
		t.Errorf("expected status 422, got %d", prob.Status)
	}
}
