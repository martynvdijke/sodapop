// Command fcmtest sends one notification through the configured FCM
// credentials so an operator can prove the setup end to end before relying on
// push in production.
//
// It reads the same environment as the server (FCM_SERVICE_ACCOUNT_JSON and,
// optionally, FCM_PROJECT_ID) and the same database, then posts to Google's FCM
// HTTP v1 endpoint. Nothing is written to the notification or outbox tables.
//
//	go run ./cmd/fcmtest -handle alice
//	go run ./cmd/fcmtest -token <device-token>
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"sodapop/ent/pushdevice"
	"sodapop/ent/user"
	"sodapop/internal/config"
	"sodapop/internal/db"
	"sodapop/internal/integrations"
)

func main() {
	dbPath := flag.String("db", "data/sodapop.db", "path to the SQLite database")
	token := flag.String("token", "", "device token to target (overrides -handle)")
	handle := flag.String("handle", "", "send to this user's newest registered device")
	title := flag.String("title", "Sodapop", "notification title")
	body := flag.String("body", "FCM is configured correctly.", "notification body")
	link := flag.String("url", "/", "relative href to open when the notification is tapped")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg, err := config.Load()
	if err != nil {
		log.Error("load config", "err", err)
		os.Exit(1)
	}

	database, err := db.Init(*dbPath)
	if err != nil {
		log.Error("open database", "err", err)
		os.Exit(1)
	}
	defer func() { _ = database.Close() }()

	svc := integrations.New(database, cfg, log)
	ready, detail := svc.FCMReady()
	if !ready {
		log.Error("fcm is not usable", "reason", detail)
		os.Exit(1)
	}
	log.Info("fcm credentials accepted", "detail", detail)

	target := *token
	if target == "" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		query := database.Client.PushDevice.Query().
			Order(pushdevice.ByLastSeenAt(), pushdevice.ByCreatedAt())
		if *handle != "" {
			owner, err := database.Client.User.Query().Where(user.HandleEQ(*handle)).Only(ctx)
			if err != nil {
				log.Error("look up handle", "handle", *handle, "err", err)
				os.Exit(1)
			}
			query = query.Where(pushdevice.UserID(owner.ID))
		}
		device, err := query.First(ctx)
		if err != nil {
			log.Error("no registered device found; open the app and allow notifications first", "err", err)
			os.Exit(1)
		}
		target = device.Token
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pruned, err := svc.SendFCM(ctx, target, *title, *body, *link)
	switch {
	case err != nil:
		log.Error("fcm send failed", "err", err)
		os.Exit(1)
	case pruned:
		log.Error("device token is no longer registered; it was pruned from the database")
		os.Exit(1)
	default:
		fmt.Println("sent. Check the device; if nothing appears, confirm notifications are allowed for Sodapop.")
	}
}
