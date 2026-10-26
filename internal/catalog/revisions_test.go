package catalog

import (
	"context"
	"strings"
	"testing"

	"sodapop/ent/catalogrevision"
	"sodapop/ent/drink"
)

func TestCreateProvisionalRecordsRevision(t *testing.T) {
	svc, database := newBrowseService(t)
	u := mkUser(t, database.Client, "reviser")
	ctx := context.Background()

	d, created, err := svc.CreateProvisional(ctx, u.ID, CreateInput{
		Name: "Revision Cola", Brand: "Revision", Container: "can", VolumeML: 355,
	})
	if err != nil || !created {
		t.Fatalf("create: created=%v err=%v", created, err)
	}
	row, err := database.Client.CatalogRevision.Query().
		Where(catalogrevision.EntityEQ(catalogrevision.EntityDrink), catalogrevision.EntityID(d.ID)).
		Only(ctx)
	if err != nil {
		t.Fatalf("revision: %v", err)
	}
	if row.Action != "create" {
		t.Fatalf("action = %q, want create", row.Action)
	}
	if row.UserID == nil || *row.UserID != u.ID {
		t.Fatalf("revision user = %v, want %s", row.UserID, u.ID)
	}
	if !strings.Contains(row.DiffJSON, `"name"`) || !strings.Contains(row.DiffJSON, "Revision Cola") {
		t.Fatalf("diff missing name: %s", row.DiffJSON)
	}
	if len(row.DiffJSON) > MaxRevisionDiffBytes {
		t.Fatalf("diff %d bytes exceeds cap %d", len(row.DiffJSON), MaxRevisionDiffBytes)
	}
}

func TestFailedCreateDoesNotRecordRevision(t *testing.T) {
	svc, database := newBrowseService(t)
	u := mkUser(t, database.Client, "failed")
	ctx := context.Background()

	before, err := database.Client.CatalogRevision.Query().Count(ctx)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if _, _, err := svc.CreateProvisional(ctx, u.ID, CreateInput{
		Name: "Bad Barcode", Barcode: "123",
	}); err == nil {
		t.Fatal("expected invalid barcode error")
	}
	after, err := database.Client.CatalogRevision.Query().Count(ctx)
	if err != nil {
		t.Fatalf("count after: %v", err)
	}
	if after != before {
		t.Fatalf("revisions created on failed mutation: %d -> %d", before, after)
	}
}

func TestAttachBarcodeRecordsRevision(t *testing.T) {
	svc, database := newBrowseService(t)
	u := mkUser(t, database.Client, "barcode")
	ctx := context.Background()
	d, _, err := svc.CreateProvisional(ctx, u.ID, CreateInput{Name: "Bar Code", Container: "can", VolumeML: 355})
	if err != nil {
		t.Fatalf("drink: %v", err)
	}
	if _, err := svc.AttachBarcode(ctx, d.ID, "5449000000996", "ean13", "user"); err != nil {
		t.Fatalf("attach: %v", err)
	}
	row, err := database.Client.CatalogRevision.Query().
		Where(catalogrevision.ActionEQ("barcode"), catalogrevision.EntityID(d.ID)).
		Only(ctx)
	if err != nil {
		t.Fatalf("barcode revision: %v", err)
	}
	if !strings.Contains(row.DiffJSON, "05449000000996") {
		t.Fatalf("diff missing gtin: %s", row.DiffJSON)
	}
	if row.UserID != nil {
		t.Fatalf("barcode revision user = %v, want nil", row.UserID)
	}
}

func TestSetDrinkStatusRecordsRevisionAndNoOps(t *testing.T) {
	svc, database := newBrowseService(t)
	u := mkUser(t, database.Client, "statusadmin")
	ctx := context.Background()
	d := mkDrink(t, database.Client, "Status Cola", "status-cola", drink.StatusActive)

	if err := svc.SetDrinkStatus(ctx, &u.ID, d.ID, "discontinued", ""); err != nil {
		t.Fatalf("set discontinued: %v", err)
	}
	rows, err := database.Client.CatalogRevision.Query().
		Where(catalogrevision.EntityEQ(catalogrevision.EntityDrink), catalogrevision.EntityID(d.ID)).
		All(ctx)
	if err != nil {
		t.Fatalf("revisions: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("revisions = %d, want 1", len(rows))
	}
	row := rows[0]
	if row.Action != "status" {
		t.Fatalf("action = %q, want status", row.Action)
	}
	if row.UserID == nil || *row.UserID != u.ID {
		t.Fatalf("revision user = %v, want %s", row.UserID, u.ID)
	}
	if !strings.Contains(row.DiffJSON, `"status"`) || !strings.Contains(row.DiffJSON, "discontinued") {
		t.Fatalf("diff missing status change: %s", row.DiffJSON)
	}
	got, err := database.Client.Drink.Get(ctx, d.ID)
	if err != nil {
		t.Fatalf("drink: %v", err)
	}
	if got.Status != drink.StatusDiscontinued || got.DiscontinuedAt == nil {
		t.Fatalf("drink after status change = %+v", got)
	}

	// The same status again must not write another audit row.
	if err := svc.SetDrinkStatus(ctx, &u.ID, d.ID, "discontinued", ""); err != nil {
		t.Fatalf("no-op status: %v", err)
	}
	if n, err := database.Client.CatalogRevision.Query().
		Where(catalogrevision.EntityEQ(catalogrevision.EntityDrink), catalogrevision.EntityID(d.ID)).
		Count(ctx); err != nil || n != 1 {
		t.Fatalf("revisions after no-op = %d, %v; want 1", n, err)
	}

	// A limited window is audited too, and changing only the date is a change.
	if err := svc.SetDrinkStatus(ctx, &u.ID, d.ID, "limited", "2026-12-31T00:00:00Z"); err != nil {
		t.Fatalf("set limited: %v", err)
	}
	if err := svc.SetDrinkStatus(ctx, &u.ID, d.ID, "limited", "2027-01-31T00:00:00Z"); err != nil {
		t.Fatalf("extend limited: %v", err)
	}
	if n, err := database.Client.CatalogRevision.Query().
		Where(catalogrevision.EntityEQ(catalogrevision.EntityDrink), catalogrevision.EntityID(d.ID)).
		Count(ctx); err != nil || n != 3 {
		t.Fatalf("revisions after limited changes = %d, %v; want 3", n, err)
	}
}

func TestRecordRevisionBoundsAndRedacts(t *testing.T) {
	svc, database := newBrowseService(t)
	ctx := context.Background()
	big := strings.Repeat("x", 32*1024)
	if err := svc.RecordRevision(ctx, "drink", 7, nil, "update",
		map[string]any{"description": "short", "password": "hunter2"},
		map[string]any{"description": big, "password": "hunter3", "api_key": "secret"},
	); err != nil {
		t.Fatalf("record: %v", err)
	}
	row, err := database.Client.CatalogRevision.Query().Only(ctx)
	if err != nil {
		t.Fatalf("revision: %v", err)
	}
	if len(row.DiffJSON) > MaxRevisionDiffBytes {
		t.Fatalf("diff %d bytes exceeds cap %d", len(row.DiffJSON), MaxRevisionDiffBytes)
	}
	if !strings.Contains(row.DiffJSON, "truncated") {
		t.Fatalf("oversized diff was not summarised: %s", row.DiffJSON)
	}
	if strings.Contains(row.DiffJSON, "hunter") || strings.Contains(row.DiffJSON, "secret") {
		t.Fatalf("diff leaked a secret: %s", row.DiffJSON)
	}
}

func TestRecordRevisionRejectsUnknownEntity(t *testing.T) {
	svc, _ := newBrowseService(t)
	if err := svc.RecordRevision(context.Background(), "comment", 1, nil, "update", nil, nil); err != ErrRevisionEntity {
		t.Fatalf("err = %v, want ErrRevisionEntity", err)
	}
}
