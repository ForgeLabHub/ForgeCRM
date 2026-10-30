// Command smoke exercises the extracted notify package: a transport with no
// configuration reports off rather than failing, and sending through it returns
// the sentinel error rather than a nil that would look like a delivery.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/geff1778/forgecrm/internal/config"
	"github.com/geff1778/forgecrm/internal/notify"
)

func main() {
	var nilSvc *notify.EmailService
	if nilSvc.Enabled() {
		fail("a nil email service reported enabled")
	}

	svc := notify.NewEmailService(&config.EmailConfig{Enabled: false}, nil,
		slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if svc.Enabled() {
		fail("a disabled email service reported enabled")
	}
	err := svc.Send(context.Background(), notify.Message{To: []string{"a@b.test"}, Subject: "x", TextBody: "y"})
	if err == nil {
		fail("sending through a disabled transport returned nil")
	}
	fmt.Println("disabled transport reports off and refuses to send")

	if notify.LinkCodeTTL <= 0 {
		fail("the Telegram link code TTL is not positive")
	}
	if len(notify.AllowedCommands) == 0 {
		fail("no Telegram commands are allowed")
	}
	fmt.Printf("telegram contract ok: %d commands\n", len(notify.AllowedCommands))
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "smoke: "+format+"\n", args...)
	os.Exit(1)
}
