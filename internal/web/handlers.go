package web

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/mail"
	"regexp"
	"strconv"
	"strings"
	"time"

	"sodapop/ent"
	"sodapop/ent/apitoken"
	"sodapop/ent/emailverificationtoken"
	"sodapop/ent/list"
	"sodapop/ent/style"
	"sodapop/ent/user"
	"sodapop/internal/auth"
	"sodapop/internal/catalog"
	"sodapop/internal/checkins"
	"sodapop/internal/db"
	"sodapop/internal/integrations"
	"sodapop/internal/social"
)

var handleRE = regexp.MustCompile(`^[a-z0-9_]{3,24}$`)

func (s *Server) home(w http.ResponseWriter, r *http.Request) {
	u := UserFromContext(r)
	data := map[string]any{"Title": "Sodapop"}
	if u != nil {
		feed, _ := s.Social.Feed(r.Context(), u.ID, 20, "")
		csrf := ""
		if sess := SessionFromContext(r); sess != nil {
			csrf = sess.CsrfToken
		}
		for i := range feed {
			feed[i].CSRF = csrf
		}
		data["Feed"] = feed
		data["Stats"], _ = s.Checkins.Stats(r.Context(), u.ID)
	}
	s.render(w, r, http.StatusOK, "home", data)
}

func (s *Server) loginPage(w http.ResponseWriter, r *http.Request) {
	if UserFromContext(r) != nil {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	s.render(w, r, http.StatusOK, "login", map[string]any{
		"Title":    "Sign in",
		"Next":     r.URL.Query().Get("next"),
		"Fallback": s.Cfg.PasswordFallback,
	})
}

func (s *Server) loginSubmit(w http.ResponseWriter, r *http.Request) {
	if !s.Cfg.PasswordFallback {
		http.Error(w, "password login is disabled", http.StatusForbidden)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	ident := strings.ToLower(strings.TrimSpace(r.FormValue("identifier")))
	password := r.FormValue("password")
	key := "login:" + clientIP(r) + "|" + ident
	if !s.Limiter.Allow(key, 5, 15*time.Minute) {
		s.render(w, r, http.StatusTooManyRequests, "login", map[string]any{
			"Title": "Sign in", "Error": "Too many attempts. Try again in a few minutes.", "Fallback": true,
		})
		return
	}
	if ident == "" || password == "" {
		s.render(w, r, http.StatusBadRequest, "login", map[string]any{
			"Title": "Sign in", "Error": "Enter your email or handle and password.", "Fallback": true,
		})
		return
	}
	u, err := s.Auth.Client.User.Query().
		Where(user.Or(user.EmailEqualFold(ident), user.HandleEqualFold(ident)), user.DeletedAtIsNil()).
		Only(r.Context())
	if err != nil || u.PasswordHash == nil || !auth.CheckPassword(*u.PasswordHash, password) {
		s.render(w, r, http.StatusUnauthorized, "login", map[string]any{
			"Title": "Sign in", "Error": "That email/handle and password combination did not match.", "Fallback": true,
		})
		return
	}
	if u.Status != "active" {
		s.render(w, r, http.StatusForbidden, "login", map[string]any{
			"Title": "Sign in", "Error": "This account is disabled.", "Fallback": true,
		})
		return
	}
	res, err := s.Auth.CreateSession(r.Context(), u.ID, clientIP(r), r.UserAgent())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.Auth.SetSessionCookie(w, r, res.Token, res.ExpiresAt)
	next := r.FormValue("next")
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") {
		next = "/"
	}
	http.Redirect(w, r, next, http.StatusFound)
}

func (s *Server) registerPage(w http.ResponseWriter, r *http.Request) {
	if !s.Cfg.PasswordFallback {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	s.render(w, r, http.StatusOK, "register", map[string]any{"Title": "Create an account"})
}

func (s *Server) registerSubmit(w http.ResponseWriter, r *http.Request) {
	if !s.Cfg.PasswordFallback {
		http.Error(w, "password registration is disabled", http.StatusForbidden)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	handle := strings.ToLower(strings.TrimSpace(r.FormValue("handle")))
	email := strings.ToLower(strings.TrimSpace(r.FormValue("email")))
	display := strings.TrimSpace(r.FormValue("display_name"))
	password := r.FormValue("password")
	renderErr := func(status int, msg string) {
		s.render(w, r, status, "register", map[string]any{
			"Title": "Create an account", "Error": msg,
			"Handle": handle, "Email": email, "DisplayName": display,
		})
	}
	if !handleRE.MatchString(handle) {
		renderErr(http.StatusBadRequest, "Handle must be 3–24 characters: lowercase letters, numbers or underscores.")
		return
	}
	if _, err := mail.ParseAddress(email); err != nil {
		renderErr(http.StatusBadRequest, "Enter a valid email address.")
		return
	}
	if len(password) < 10 {
		renderErr(http.StatusBadRequest, "Use at least 10 characters for your password.")
		return
	}
	ctx := r.Context()
	if n, err := s.Auth.Client.User.Query().Where(user.HandleEqualFold(handle)).Count(ctx); err != nil || n > 0 {
		renderErr(http.StatusConflict, "That handle is already taken.")
		return
	}
	if n, err := s.Auth.Client.User.Query().Where(user.EmailEqualFold(email)).Count(ctx); err != nil || n > 0 {
		renderErr(http.StatusConflict, "An account with that email already exists.")
		return
	}
	hash, err := s.Auth.HashPassword(password)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	tx, err := s.Auth.Client.Tx(ctx)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	u, err := tx.User.Create().
		SetHandle(handle).SetEmail(email).SetDisplayName(display).SetPasswordHash(hash).
		Save(ctx)
	if err != nil {
		_ = tx.Rollback()
		s.fail(w, r, err)
		return
	}
	if _, err := tx.UserSetting.Create().SetUserID(u.ID).Save(ctx); err != nil {
		_ = tx.Rollback()
		s.fail(w, r, err)
		return
	}
	if _, err := tx.List.Create().
		SetUserID(u.ID).SetName("Wishlist").SetSlug("wishlist").
		SetVisibility(list.VisibilityPrivate).SetIsSystem(true).Save(ctx); err != nil {
		_ = tx.Rollback()
		s.fail(w, r, err)
		return
	}
	raw := auth.RandomToken(32)
	if _, err := tx.EmailVerificationToken.Create().
		SetTokenHash(auth.HashToken(raw)).SetUserID(u.ID).SetEmail(email).
		SetExpiresAt(time.Now().UTC().Add(24 * time.Hour).Format(time.RFC3339)).
		Save(ctx); err != nil {
		_ = tx.Rollback()
		s.fail(w, r, err)
		return
	}
	if err := tx.Commit(); err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.Mailer.Verification(ctx, email, handle, raw); err != nil {
		s.Log.Warn("queue verification email failed", "err", err)
	}
	res, err := s.Auth.CreateSession(ctx, u.ID, clientIP(r), r.UserAgent())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.Auth.SetSessionCookie(w, r, res.Token, res.ExpiresAt)
	data := map[string]any{
		"Title":   "Check your inbox",
		"Pending": true,
		"Email":   email,
	}
	if s.Cfg.Env != "production" {
		data["DevLink"] = s.Cfg.BaseURL + "/verify?token=" + raw
	}
	s.render(w, r, http.StatusOK, "verify", data)
}

func (s *Server) verify(w http.ResponseWriter, r *http.Request) {
	raw := strings.TrimSpace(r.URL.Query().Get("token"))
	if raw == "" {
		s.render(w, r, http.StatusOK, "verify", map[string]any{
			"Title": "Verify your email", "Pending": false,
			"Error": "Open the verification link from your email to confirm your address.",
		})
		return
	}
	ctx := r.Context()
	row, err := s.Auth.Client.EmailVerificationToken.Query().
		Where(emailverificationtoken.TokenHash(auth.HashToken(raw)), emailverificationtoken.UsedAtIsNil()).
		Only(ctx)
	if err != nil {
		s.render(w, r, http.StatusBadRequest, "verify", map[string]any{
			"Title": "Verify your email", "Pending": false, "Error": "This verification link is invalid or has already been used.",
		})
		return
	}
	expires, _ := time.Parse(time.RFC3339, row.ExpiresAt)
	if time.Now().UTC().After(expires) {
		s.render(w, r, http.StatusBadRequest, "verify", map[string]any{
			"Title": "Verify your email", "Pending": false, "Error": "This verification link has expired.",
		})
		return
	}
	if _, err := s.Auth.Client.User.UpdateOneID(row.UserID).
		SetEmailVerifiedAt(db.Now()).SetUpdatedAt(db.Now()).Save(ctx); err != nil {
		s.fail(w, r, err)
		return
	}
	_, _ = row.Update().SetUsedAt(db.Now()).Save(ctx)
	s.render(w, r, http.StatusOK, "verify", map[string]any{
		"Title": "Email confirmed", "Pending": false, "Success": true,
	})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	s.Auth.DestroySession(r.Context(), r)
	s.Auth.ClearSessionCookie(w, r)
	http.Redirect(w, r, "/", http.StatusFound)
}

func (s *Server) searchPage(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	data := map[string]any{"Title": "Search", "Query": q}
	if q != "" {
		if len([]rune(q)) < catalog.MinSearchLen {
			data["Error"] = "Type at least two characters."
		} else {
			results, err := s.Catalog.Search(r.Context(), q, r.URL.Query().Get("style"), 10)
			if err != nil {
				data["Error"] = "Search is temporarily unavailable."
			}
			data["Results"] = results
		}
	}
	s.render(w, r, http.StatusOK, "search", data)
}

func (s *Server) drinkPage(w http.ResponseWriter, r *http.Request) {
	d, err := s.Catalog.GetBySlug(r.Context(), r.PathValue("slug"))
	if err != nil {
		s.render(w, r, http.StatusNotFound, "error", map[string]any{"Title": "Not found", "Message": "That soda is not in the catalog yet."})
		return
	}
	lists, _ := s.Auth.Client.List.Query().
		Where(list.UserID(UserFromContext(r).ID)).
		Order(ent.Asc(list.FieldName)).All(r.Context())
	u := UserFromContext(r)
	s.render(w, r, http.StatusOK, "drink", map[string]any{
		"Title": d.Drink.Name, "Drink": d, "Lists": lists,
		"CanManage": u != nil && (u.Role == "admin" || u.Role == "moderator"),
	})
}

func (s *Server) checkinPage(w http.ResponseWriter, r *http.Request) {
	slug := r.URL.Query().Get("slug")
	d, err := s.Catalog.GetBySlug(r.Context(), slug)
	if err != nil {
		http.Redirect(w, r, "/search", http.StatusFound)
		return
	}
	rows, _ := s.Auth.SQL.QueryContext(r.Context(), `
		SELECT id, name, COALESCE(city,'') FROM venues
		WHERE merged_into_id IS NULL ORDER BY updated_at DESC LIMIT 40`)
	venues := []venueOption{}
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var v venueOption
			if rows.Scan(&v.ID, &v.Name, &v.City) == nil {
				venues = append(venues, v)
			}
		}
	}
	defaultVisibility := "public"
	if u := UserFromContext(r); u != nil {
		if v := string(u.DefaultVisibility); v != "" {
			defaultVisibility = v
		}
	}
	s.render(w, r, http.StatusOK, "checkin", map[string]any{
		"Title": "Log " + d.Drink.Name, "Drink": d, "Ratings": ratingChoices(),
		"Idempotency": auth.RandomToken(16), "Venues": venues,
		"DefaultVisibility": defaultVisibility,
	})
}

type venueOption struct {
	ID   int
	Name string
	City string
}

func (s *Server) checkinSubmit(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 126<<20)
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}
	}
	if !s.csrfOK(w, r) {
		return
	}
	u := UserFromContext(r)
	photoID := ""
	if file, header, err := r.FormFile("media"); err == nil {
		defer file.Close()
		row, err := s.Media.Store(r.Context(), u.ID, "checkin", file, header.Filename, header.Header.Get("Content-Type"))
		if err != nil {
			s.Log.Warn("media upload failed", "err", err)
			http.Error(w, "could not store that photo or video: "+err.Error(), http.StatusBadRequest)
			return
		}
		photoID = row.ID
	} else if err != http.ErrMissingFile {
		http.Error(w, "could not read upload", http.StatusBadRequest)
		return
	}
	drinkID, _ := strconv.Atoi(r.FormValue("drink_id"))
	var rating *int
	if v := strings.TrimSpace(r.FormValue("rating")); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil || f < 0 || f > 5 {
			http.Error(w, "invalid rating", http.StatusBadRequest)
			return
		}
		scaled := int(math.Round(f * 4))
		rating = &scaled
	}
	var venueID *int
	if v, err := strconv.Atoi(r.FormValue("venue_id")); err == nil && v > 0 {
		venueID = &v
	}
	c, created, err := s.Checkins.Create(r.Context(), u, checkins.CreateInput{
		DrinkID:        drinkID,
		VenueID:        venueID,
		Rating:         rating,
		Serving:        r.FormValue("serving"),
		Note:           strings.TrimSpace(r.FormValue("note")),
		Visibility:     r.FormValue("visibility"),
		IdempotencyKey: r.FormValue("idempotency_key"),
		PhotoMediaID:   photoID,
	})
	if err != nil {
		s.Log.Warn("checkin failed", "err", err)
		http.Error(w, "could not save check-in", http.StatusBadRequest)
		return
	}
	if created {
		earned, err := s.Badges.Evaluate(r.Context(), u.ID)
		if err != nil {
			s.Log.Warn("badge evaluation failed", "err", err)
		}
		for _, slug := range earned {
			s.Integrations.Dispatch(r.Context(), integrations.Event{
				UserID: u.ID, Kind: "badge", Text: "Earned the " + slug + " badge.", URL: s.Cfg.BaseURL + "/you",
			})
		}
		s.Integrations.Dispatch(r.Context(), integrations.Event{
			UserID: u.ID, Kind: "checkin", Text: "Logged a new soda.", URL: s.Cfg.BaseURL + "/checkins/" + c.ID,
		})
	}
	http.Redirect(w, r, "/", http.StatusFound)
}

func (s *Server) friendsPage(w http.ResponseWriter, r *http.Request) {
	u := UserFromContext(r)
	f, err := s.Social.Friends(r.Context(), u.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.render(w, r, http.StatusOK, "friends", map[string]any{"Title": "Friends", "Friends": f})
}

func (s *Server) friendRequest(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	u := UserFromContext(r)
	handle := strings.TrimSpace(r.FormValue("handle"))
	fr, err := s.Social.Request(r.Context(), u.ID, handle)
	if err != nil && !errors.Is(err, social.ErrSelfAction) {
		s.fail(w, r, err)
		return
	}
	if err == nil && fr != nil && string(fr.Status) == "pending" && s.Mailer != nil {
		ctx := r.Context()
		target, terr := s.Auth.Client.User.Query().
			Where(user.HandleEqualFold(strings.ToLower(strings.TrimSpace(handle))), user.DeletedAtIsNil()).
			Only(ctx)
		if terr == nil && target.Email != "" {
			if settings, serr := s.Auth.GetOrCreateSettings(ctx, target.ID); serr == nil && settings.EmailFriendReq {
				if merr := s.Mailer.FriendRequest(ctx, target.Email, target.DisplayName, u.Handle); merr != nil {
					s.Log.Warn("friend request email failed", "err", merr)
				}
			}
		}
	}
	http.Redirect(w, r, "/friends", http.StatusFound)
}

func (s *Server) friendAccept(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	u := UserFromContext(r)
	if err := s.Social.Accept(r.Context(), u.ID, r.PathValue("id")); err != nil {
		http.Error(w, "could not accept request", http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/friends", http.StatusFound)
}

func (s *Server) friendRemove(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	u := UserFromContext(r)
	if err := s.Social.Remove(r.Context(), u.ID, r.PathValue("id")); err != nil {
		http.Error(w, "could not remove friend", http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/friends", http.StatusFound)
}

func (s *Server) tokensPage(w http.ResponseWriter, r *http.Request) {
	u := UserFromContext(r)
	tokens, err := s.Auth.Client.APIToken.Query().
		Where(apitoken.UserID(u.ID)).
		Order(ent.Desc(apitoken.FieldCreatedAt)).All(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.render(w, r, http.StatusOK, "tokens", map[string]any{"Title": "API tokens", "Tokens": tokens})
}

func (s *Server) tokensCreate(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	u := UserFromContext(r)
	if u.EmailVerifiedAt == nil {
		http.Error(w, "verify your email first", http.StatusForbidden)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		name = "Personal token"
	}
	raw, _, err := s.Auth.CreateAPIToken(r.Context(), u.ID, name, []string{"read", "write", "feed"})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	tokens, _ := s.Auth.Client.APIToken.Query().
		Where(apitoken.UserID(u.ID)).Order(ent.Desc(apitoken.FieldCreatedAt)).All(r.Context())
	s.render(w, r, http.StatusCreated, "tokens", map[string]any{
		"Title": "API tokens", "Tokens": tokens, "NewToken": raw,
		"Notice": "Copy this token now. It is not shown again.",
	})
}

func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
	s.Log.Error("web request failed", "err", err, "path", r.URL.Path)
	s.render(w, r, http.StatusInternalServerError, "error", map[string]any{
		"Title": "Something went wrong", "Message": "The request could not be completed. Try again.",
	})
}

func clientIP(r *http.Request) string {
	if v := r.Header.Get("X-Forwarded-For"); v != "" {
		return strings.TrimSpace(strings.Split(v, ",")[0])
	}
	return r.RemoteAddr
}

func ratingChoices() []string {
	out := make([]string, 0, 21)
	for i := 0; i <= 20; i++ {
		out = append(out, fmt.Sprintf("%.2f", float64(i)/4))
	}
	return out
}

func (s *Server) checkinToast(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	u := UserFromContext(r)
	id := r.PathValue("id")
	var err error
	if r.FormValue("undo") == "1" {
		err = s.Social.Untoast(r.Context(), u.ID, id)
	} else {
		err = s.Social.Toast(r.Context(), u.ID, id)
	}
	if err != nil && !errors.Is(err, social.ErrNotFound) {
		s.fail(w, r, err)
		return
	}
	target := r.Referer()
	if target == "" {
		target = "/"
	}
	http.Redirect(w, r, target, http.StatusFound)
}

func (s *Server) newDrinkPage(w http.ResponseWriter, r *http.Request) {
	styles, _ := s.Auth.Client.Style.Query().
		Order(ent.Asc(style.FieldName)).All(r.Context())
	s.render(w, r, http.StatusOK, "newdrink", map[string]any{
		"Title":   "Add a soda",
		"Styles":  styles,
		"Barcode": r.URL.Query().Get("barcode"),
	})
}

func (s *Server) newDrinkSubmit(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	u := UserFromContext(r)
	volume, _ := strconv.Atoi(r.FormValue("volume_ml"))
	d, created, err := s.Catalog.CreateProvisional(r.Context(), u.ID, catalog.CreateInput{
		Name:        r.FormValue("name"),
		Brand:       r.FormValue("brand"),
		StyleSlug:   r.FormValue("style"),
		Container:   r.FormValue("container"),
		VolumeML:    volume,
		Sweetener:   r.FormValue("sweetener"),
		Carbonation: r.FormValue("carbonation"),
		Acid:        r.FormValue("acid"),
		Barcode:     r.FormValue("barcode"),
		Description: r.FormValue("description"),
	})
	if err != nil {
		s.render(w, r, http.StatusBadRequest, "newdrink", map[string]any{
			"Title": "Add a soda", "Error": err.Error(),
			"Barcode": r.FormValue("barcode"),
		})
		return
	}
	if !created {
		// Someone already catalogued this soda; send the contributor there.
		http.Redirect(w, r, "/drinks/"+d.Slug+"?existing=1", http.StatusFound)
		return
	}
	http.Redirect(w, r, "/drinks/"+d.Slug+"?created=1", http.StatusFound)
}
