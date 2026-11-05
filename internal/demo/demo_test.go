package demo_test

import (
	"context"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"sodapop/ent/checkin"
	"sodapop/ent/user"
	"sodapop/ent/usersetting"
	"sodapop/internal/checkins"
	"sodapop/internal/config"
	"sodapop/internal/db"
	"sodapop/internal/demo"
	"sodapop/internal/social"
)

func newDB(t *testing.T) *db.DB {
	t.Helper()
	database, err := db.Init(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func demoConfig() *config.Config {
	return &config.Config{
		DemoUserEnabled:  true,
		DemoUserHandle:   "demo",
		DemoUserPassword: "demo",
		DemoUserEmail:    "demo@sodapop.local",
	}
}

func TestEnsureCreatesPublicDemoUserAndCheckins(t *testing.T) {
	database := newDB(t)
	ctx := context.Background()
	svc := checkins.New(database, social.New(database))

	if err := demo.Ensure(ctx, database.Client, demoConfig(), svc); err != nil {
		t.Fatalf("ensure: %v", err)
	}

	u, err := database.Client.User.Query().Where(user.HandleEQ("demo")).Only(ctx)
	if err != nil {
		t.Fatalf("demo user: %v", err)
	}
	if u.Status != user.StatusActive || u.EmailVerifiedAt == nil {
		t.Fatalf("demo user is not active/verified: %+v", u)
	}
	if u.Role == user.RoleAdmin {
		t.Fatal("demo user must not be an admin")
	}
	if u.DefaultVisibility != user.DefaultVisibilityPublic {
		t.Fatalf("default visibility = %q", u.DefaultVisibility)
	}

	if u.PasswordHash == nil || bcrypt.CompareHashAndPassword([]byte(*u.PasswordHash), []byte("demo")) != nil {
		t.Fatal("demo password does not match demo")
	}

	setting, err := database.Client.UserSetting.Query().
		Where(usersetting.UserIDEQ(u.ID)).
		Only(ctx)
	if err != nil {
		t.Fatalf("demo setting: %v", err)
	}
	if !setting.PublicProfile {
		t.Fatal("demo profile must be public")
	}

	rows, err := database.Client.Checkin.Query().
		Where(checkin.HasUserWith(user.ID(u.ID))).
		All(ctx)
	if err != nil {
		t.Fatalf("checkins: %v", err)
	}
	if len(rows) != 6 {
		t.Fatalf("seeded checkins = %d, want 6", len(rows))
	}
	for _, row := range rows {
		if row.Visibility != checkin.VisibilityPublic {
			t.Fatalf("seeded checkin %s visibility = %q", row.ID, row.Visibility)
		}
		if row.DeletedAt != nil {
			t.Fatalf("seeded checkin %s is deleted", row.ID)
		}
	}
}

func TestEnsureIsIdempotent(t *testing.T) {
	database := newDB(t)
	ctx := context.Background()
	svc := checkins.New(database, social.New(database))

	for i := 0; i < 2; i++ {
		if err := demo.Ensure(ctx, database.Client, demoConfig(), svc); err != nil {
			t.Fatalf("ensure %d: %v", i+1, err)
		}
	}
	users, err := database.Client.User.Query().Count(ctx)
	if err != nil {
		t.Fatalf("count users: %v", err)
	}
	if users != 1 {
		t.Fatalf("users = %d, want 1", users)
	}
	rows, err := database.Client.Checkin.Query().Count(ctx)
	if err != nil {
		t.Fatalf("count checkins: %v", err)
	}
	if rows != 6 {
		t.Fatalf("checkins = %d, want 6", rows)
	}
}

func TestEnsureRefusesToTakeOverAnotherAccount(t *testing.T) {
	database := newDB(t)
	ctx := context.Background()
	if _, err := database.Client.User.Create().
		SetHandle("demo").
		SetEmail("someone@example.com").
		SetDisplayName("Real Demo").
		Save(ctx); err != nil {
		t.Fatalf("existing user: %v", err)
	}
	err := demo.Ensure(ctx, database.Client, demoConfig(), nil)
	if err == nil {
		t.Fatal("expected an error instead of taking over an existing account")
	}
	u, err := database.Client.User.Query().Where(user.HandleEQ("demo")).Only(ctx)
	if err != nil {
		t.Fatalf("demo user: %v", err)
	}
	if u.Email != "someone@example.com" || u.DisplayName != "Real Demo" || u.PasswordHash != nil {
		t.Fatalf("existing account was modified: %+v", u)
	}
}

func TestEnsureDisabledIsNoop(t *testing.T) {
	database := newDB(t)
	cfg := demoConfig()
	cfg.DemoUserEnabled = false

	if err := demo.Ensure(context.Background(), database.Client, cfg, nil); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	users, err := database.Client.User.Query().Count(context.Background())
	if err != nil {
		t.Fatalf("count users: %v", err)
	}
	if users != 0 {
		t.Fatalf("users = %d, want 0", users)
	}
}
