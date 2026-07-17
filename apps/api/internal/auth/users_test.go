package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestSecurityPolicyNormalization(t *testing.T) {
	optional := (SecurityPolicy{MinimumPasswordLength: 8, MaximumPasswordLength: 20, PasswordHistory: 99, MFAMode: MFAOptional}).Normalized()
	if optional.MinimumPasswordLength != 15 || optional.MaximumPasswordLength != 64 || optional.PasswordHistory != 24 {
		t.Fatalf("unexpected optional policy: %+v", optional)
	}
	required := (SecurityPolicy{MinimumPasswordLength: 8, MaximumPasswordLength: 128, MFAMode: MFARequiredAll}).Normalized()
	if required.MinimumPasswordLength != 8 {
		t.Fatalf("MFA-required minimum = %d, want 8", required.MinimumPasswordLength)
	}
}

func TestUserLifecycleAndSessionRevocation(t *testing.T) {
	ctx := context.Background()
	store := NewUserStoreFromEnv("admin", "bootstrap-passphrase")
	if err := store.Create(ctx, CreateUserRequest{Username: "alice", Email: "alice@example.com", Password: "several quiet copper forests", Role: RoleOperator}); err != nil {
		t.Fatalf("create: %v", err)
	}
	user, err := store.Authenticate(ctx, "alice", "several quiet copper forests")
	if err != nil || user.Email != "alice@example.com" {
		t.Fatalf("authenticate: user=%+v err=%v", user, err)
	}
	if !store.ValidateSession(*user) {
		t.Fatal("fresh session rejected")
	}
	disabled := true
	if err := store.UpdateAdmin(ctx, "alice", AdminUserUpdate{Disabled: &disabled}); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if store.ValidateSession(*user) {
		t.Fatal("session survived account disable")
	}
	if _, err := store.Authenticate(ctx, "alice", "several quiet copper forests"); err != ErrAccountDisabled {
		t.Fatalf("disabled authenticate error = %v", err)
	}
}

func TestPasswordPolicyAndHistory(t *testing.T) {
	ctx := context.Background()
	store := NewUserStoreFromEnv("admin", "bootstrap-passphrase")
	if err := store.Create(ctx, CreateUserRequest{Username: "robert", Password: "short", Role: RoleViewer}); err == nil {
		t.Fatal("short password accepted")
	}
	if err := store.Create(ctx, CreateUserRequest{Username: "robert", Password: "robert has a unique phrase", Role: RoleViewer}); err == nil {
		t.Fatal("identity-derived password accepted")
	}
	if err := store.Create(ctx, CreateUserRequest{Username: "robert", Password: "several quiet copper forests", Role: RoleViewer}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := store.ChangePassword(ctx, "robert", "several quiet copper forests", "another unique violet harbor"); err != nil {
		t.Fatalf("change password: %v", err)
	}
	if err := store.ChangePassword(ctx, "robert", "another unique violet harbor", "several quiet copper forests"); err == nil {
		t.Fatal("recent password reuse accepted")
	}
}

func TestMFAEnrollmentTOTPAndRecoveryCode(t *testing.T) {
	ctx := context.Background()
	store := NewUserStoreFromEnv("admin", "bootstrap-passphrase")
	enrollment, err := store.BeginMFAEnrollment(ctx, "admin", "bootstrap-passphrase")
	if err != nil || enrollment.Secret == "" || len(enrollment.RecoveryCodes) != 10 {
		t.Fatalf("begin enrollment: %+v err=%v", enrollment, err)
	}
	code := totpCode(enrollment.Secret, time.Now().Unix()/totpPeriod)
	if err := store.ConfirmMFAEnrollment(ctx, "admin", code); err != nil {
		t.Fatalf("confirm enrollment: %v", err)
	}
	user, err := store.VerifySecondFactor(ctx, "admin", code)
	if err != nil || !user.MFAEnabled {
		t.Fatalf("verify TOTP: user=%+v err=%v", user, err)
	}
	recovery := enrollment.RecoveryCodes[0]
	if _, err := store.VerifySecondFactor(ctx, "admin", recovery); err != nil {
		t.Fatalf("verify recovery code: %v", err)
	}
	if _, err := store.VerifySecondFactor(ctx, "admin", recovery); err == nil {
		t.Fatal("recovery code was reusable")
	}
}

func TestKubernetesIdentityPersistenceEncryptsMFASecrets(t *testing.T) {
	ctx := context.Background()
	initial, _ := json.Marshal(IdentityDocument{Version: 1, Policy: DefaultSecurityPolicy()})
	client := fake.NewSimpleClientset(&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "identity", Namespace: "system"}, Data: map[string][]byte{identityDataKey: initial}})
	persistence, err := NewKubernetesIdentityPersistence(client, "system", "identity", []byte("01234567890123456789012345678901"))
	if err != nil {
		t.Fatalf("new persistence: %v", err)
	}
	store := NewUserStoreFromEnv("admin", "bootstrap-passphrase")
	if err := store.ConfigurePersistence(ctx, persistence); err != nil {
		t.Fatalf("configure: %v", err)
	}
	enrollment, err := store.BeginMFAEnrollment(ctx, "admin", "bootstrap-passphrase")
	if err != nil {
		t.Fatalf("begin enrollment: %v", err)
	}
	secret, _ := client.CoreV1().Secrets("system").Get(ctx, "identity", metav1.GetOptions{})
	if string(secret.Data[identityDataKey]) == "" || bytes.Contains(secret.Data[identityDataKey], []byte(enrollment.Secret)) {
		t.Fatal("TOTP secret was persisted in plaintext")
	}
	loaded, err := persistence.Load(ctx)
	if err != nil || loaded.Users[0].PendingTOTPSecret != enrollment.Secret {
		t.Fatalf("encrypted round trip: %+v err=%v", loaded, err)
	}
}
