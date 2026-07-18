package gen_test

import (
	"testing"

	"github.com/highland-io/highland/apps/api/internal/api/gen"
)

// Compile-time smoke: generated wire types for key public surfaces exist and
// can be referenced by consumers without importing domain packages.
func TestGeneratedWireTypesCompile(t *testing.T) {
	_ = gen.LoginRequest{Username: "admin", Password: "x"}
	pw := "y"
	_ = gen.CreateUserRequest{Username: "alice", Password: &pw, Role: gen.RoleOperator}
	_ = gen.AuditListResponse{Data: []gen.AuditEvent{}}
	_ = gen.HealthzResponse{Status: "ok", Service: "highland-api"}
	// Ensure MFA and platform types landed in the generation surface.
	_ = gen.MFAVerifyRequest{ChallengeToken: "t", Code: "123456"}
	_ = gen.CreateBenchmarkRequest{}
}
