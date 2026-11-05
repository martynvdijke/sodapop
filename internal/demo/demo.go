// Package demo provisions the shared demo account used by the hosted Sodapop
// instance. The account is deliberately public: its handle and password are
// advertised on the login screen so a visitor can try posting without
// registering.
package demo

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"sodapop/ent"
	"sodapop/ent/brand"
	"sodapop/ent/checkin"
	"sodapop/ent/drink"
	"sodapop/ent/user"
	"sodapop/ent/usersetting"
	"sodapop/internal/checkins"
	"sodapop/internal/config"
	"sodapop/internal/db"
)

var handleRE = regexp.MustCompile(`^[a-z0-9_]{3,24}$`)

const (
	demoDisplayName = "Demo Soda"
	demoBio         = "A shared account for trying Sodapop. Post a pour, then make your own account."
)

type seedDrink struct {
	Name      string
	Slug      string
	Brand     string
	BrandSlug string
	Container drink.Container
	VolumeML  int
}

var seedDrinks = []seedDrink{
	{Name: "Coca-Cola", Slug: "coca-cola-355", Brand: "Coca-Cola", BrandSlug: "coca-cola", Container: drink.ContainerCan, VolumeML: 355},
	{Name: "Fentimans Curiosity Cola", Slug: "fentimans-curiosity-cola-275", Brand: "Fentimans", BrandSlug: "fentimans", Container: drink.ContainerGlass, VolumeML: 275},
	{Name: "Fever-Tree Ginger Beer", Slug: "fever-tree-ginger-beer-200", Brand: "Fever-Tree", BrandSlug: "fever-tree", Container: drink.ContainerGlass, VolumeML: 200},
}

type seedCheckin struct {
	DrinkIndex int
	Rating     int
	Serving    string
	Note       string
	AgeHours   int
}

var seedCheckins = []seedCheckin{
	{DrinkIndex: 0, Rating: 16, Serving: "can", Note: "Cold, sharp, and exactly the reference cola.", AgeHours: 2},
	{DrinkIndex: 2, Rating: 15, Serving: "glass", Note: "Fiery ginger and a long finish.", AgeHours: 20},
	{DrinkIndex: 1, Rating: 17, Serving: "glass", Note: "Botanical cola with a gentler sweetness.", AgeHours: 44},
	{DrinkIndex: 0, Rating: 15, Serving: "can", Note: "Better over ice, honestly.", AgeHours: 72},
	{DrinkIndex: 2, Rating: 14, Serving: "glass", Note: "Needs a squeeze of lime.", AgeHours: 100},
	{DrinkIndex: 1, Rating: 18, Serving: "glass", Note: "The one I keep coming back to.", AgeHours: 140},
}

// Ensure creates or repairs the demo account and, on a fresh database, seeds a
// handful of public pours so the logged-out preview has something to show.
// It is safe to call on every boot.
func Ensure(ctx context.Context, client *ent.Client, cfg *config.Config, svc *checkins.Service) error {
	if cfg == nil || !cfg.DemoUserEnabled {
		return nil
	}
	handle := strings.ToLower(strings.TrimSpace(cfg.DemoUserHandle))
	if !handleRE.MatchString(handle) {
		return fmt.Errorf("DEMO_USER_HANDLE must be 3-24 characters of a-z, 0-9 or _")
	}
	email := strings.ToLower(strings.TrimSpace(cfg.DemoUserEmail))
	if email == "" {
		return fmt.Errorf("DEMO_USER_EMAIL must not be empty")
	}
	if cfg.DemoUserPassword == "" {
		return fmt.Errorf("DEMO_USER_PASSWORD must not be empty")
	}

	u, err := client.User.Query().Where(user.Handle(handle)).Only(ctx)
	if ent.IsNotFound(err) {
		hash, hashErr := bcrypt.GenerateFromPassword([]byte(cfg.DemoUserPassword), cfg.BcryptCost)
		if hashErr != nil {
			return hashErr
		}
		u, err = client.User.Create().
			SetHandle(handle).
			SetEmail(email).
			SetEmailVerifiedAt(db.Now()).
			SetPasswordHash(string(hash)).
			SetDisplayName(demoDisplayName).
			SetBio(demoBio).
			SetDefaultVisibility(user.DefaultVisibilityPublic).
			Save(ctx)
	}
	if err != nil {
		return fmt.Errorf("demo user: %w", err)
	}
	if !strings.EqualFold(u.Email, email) {
		// Never take over an existing account that happens to use the demo
		// handle. The operator can choose a different DEMO_USER_HANDLE.
		return fmt.Errorf("DEMO_USER_HANDLE %q already belongs to another account", handle)
	}

	// Repair the account if it already existed. Never touch the role: a demo
	// account must never become an admin by accident.
	update := client.User.UpdateOneID(u.ID).
		SetStatus(user.StatusActive).
		SetDefaultVisibility(user.DefaultVisibilityPublic)
	if u.EmailVerifiedAt == nil {
		update.SetEmailVerifiedAt(db.Now())
	}
	if strings.TrimSpace(u.DisplayName) == "" {
		update.SetDisplayName(demoDisplayName)
	}
	if strings.TrimSpace(u.Bio) == "" {
		update.SetBio(demoBio)
	}
	if u.Email != email {
		// Do not steal another account's email address. Leaving the existing
		// address in place is safer than failing startup on a shared host.
		if taken, _ := client.User.Query().Where(user.Email(email)).Exist(ctx); !taken {
			update.SetEmail(email)
		}
	}
	if u.PasswordHash == nil || bcrypt.CompareHashAndPassword([]byte(*u.PasswordHash), []byte(cfg.DemoUserPassword)) != nil {
		hash, hashErr := bcrypt.GenerateFromPassword([]byte(cfg.DemoUserPassword), cfg.BcryptCost)
		if hashErr != nil {
			return hashErr
		}
		update.SetPasswordHash(string(hash))
	}
	u, err = update.Save(ctx)
	if err != nil {
		return fmt.Errorf("repair demo user: %w", err)
	}

	prefs, err := client.UserSetting.Query().Where(usersetting.UserID(u.ID)).Only(ctx)
	if ent.IsNotFound(err) {
		prefs, err = client.UserSetting.Create().SetUserID(u.ID).SetPublicProfile(true).Save(ctx)
	} else if err == nil && !prefs.PublicProfile {
		prefs, err = prefs.Update().SetPublicProfile(true).SetUpdatedAt(db.Now()).Save(ctx)
	}
	if err != nil {
		return fmt.Errorf("demo settings: %w", err)
	}

	count, err := client.Checkin.Query().
		Where(checkin.UserID(u.ID), checkin.DeletedAtIsNil()).
		Count(ctx)
	if err != nil {
		return fmt.Errorf("count demo check-ins: %w", err)
	}
	if count > 0 {
		return nil
	}
	if svc == nil {
		return nil
	}

	for _, seed := range seedDrinks {
		if _, err := ensureDrink(ctx, client, seed); err != nil {
			return err
		}
	}
	drinkIDs, err := client.Drink.Query().
		Where(drink.SlugIn(seedDrinks[0].Slug, seedDrinks[1].Slug, seedDrinks[2].Slug)).
		All(ctx)
	if err != nil {
		return err
	}
	bySlug := make(map[string]int, len(drinkIDs))
	for _, row := range drinkIDs {
		bySlug[row.Slug] = row.ID
	}
	for i, seed := range seedCheckins {
		drinkID := bySlug[seedDrinks[seed.DrinkIndex].Slug]
		if drinkID == 0 {
			continue
		}
		row, created, err := svc.Create(ctx, u, checkins.CreateInput{
			DrinkID:        drinkID,
			Rating:         &seed.Rating,
			Serving:        seed.Serving,
			Note:           seed.Note,
			Visibility:     "public",
			IdempotencyKey: fmt.Sprintf("demo-seed:%d", i+1),
		})
		if err != nil {
			return fmt.Errorf("seed demo check-in %d: %w", i+1, err)
		}
		if !created {
			continue
		}
		stamp := time.Now().UTC().Add(-time.Duration(seed.AgeHours) * time.Hour).Format(time.RFC3339)
		if _, err := client.Checkin.UpdateOneID(row.ID).SetCreatedAt(stamp).Save(ctx); err != nil {
			return fmt.Errorf("stamp demo check-in %d: %w", i+1, err)
		}
	}
	return nil
}

func ensureDrink(ctx context.Context, client *ent.Client, seed seedDrink) (int, error) {
	existing, err := client.Drink.Query().Where(drink.Slug(seed.Slug)).Only(ctx)
	if err == nil {
		return existing.ID, nil
	}
	if !ent.IsNotFound(err) {
		return 0, err
	}
	b, err := client.Brand.Query().Where(brand.Slug(seed.BrandSlug)).Only(ctx)
	if ent.IsNotFound(err) {
		b, err = client.Brand.Create().
			SetName(seed.Brand).
			SetSlug(seed.BrandSlug).
			SetVerified(true).
			SetSource(brand.SourceLocal).
			Save(ctx)
	}
	if err != nil {
		return 0, fmt.Errorf("seed brand %s: %w", seed.BrandSlug, err)
	}
	row, err := client.Drink.Create().
		SetName(seed.Name).
		SetSlug(seed.Slug).
		SetBrandID(b.ID).
		SetContainer(seed.Container).
		SetVolumeMl(seed.VolumeML).
		SetStatus(drink.StatusActive).
		SetPublished(true).
		Save(ctx)
	if err != nil {
		return 0, fmt.Errorf("seed drink %s: %w", seed.Slug, err)
	}
	return row.ID, nil
}
