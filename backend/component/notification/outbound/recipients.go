package outbound

import (
	"context"
	"fmt"
	"net/mail"
	"strings"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// maxEmailRecipients bounds one email delivery. The list is fixed addresses plus
// the event's audience; a workspace with more responsible people than this is
// addressed through a fixed team address instead, so one message cannot turn
// into an unbounded SMTP transaction.
const maxEmailRecipients = 50

// UserEmailResolver resolves one user id to the address email is sent to.
type UserEmailResolver func(ctx context.Context, userID int32) (string, error)

// EmailRecipients resolves the addresses one email delivery goes to: the
// channel's fixed addresses plus, when configured, the audience the event's
// in-app message addressed. Addresses are trimmed, validated and de-duplicated
// case-insensitively; a resolver failure aborts the delivery so the dispatcher
// retries it rather than silently dropping a person.
func EmailRecipients(ctx context.Context, config *storepb.EmailChannelConfig, audience []int32, resolve UserEmailResolver) ([]string, error) {
	recipients := make([]string, 0, len(config.GetAddresses())+len(audience))
	seen := make(map[string]struct{}, len(config.GetAddresses())+len(audience))
	add := func(address string) error {
		address = strings.TrimSpace(address)
		if address == "" {
			return nil
		}
		if strings.ContainsAny(address, "\r\n") {
			return fmt.Errorf("invalid email recipient %q", address)
		}
		parsed, err := mail.ParseAddress(address)
		if err != nil {
			return fmt.Errorf("invalid email recipient %q", address)
		}
		key := strings.ToLower(parsed.Address)
		if _, ok := seen[key]; ok {
			return nil
		}
		seen[key] = struct{}{}
		recipients = append(recipients, parsed.Address)
		return nil
	}
	for _, address := range config.GetAddresses() {
		if err := add(address); err != nil {
			return nil, err
		}
	}
	if config.GetIncludeAudience() {
		for _, userID := range audience {
			if userID <= 0 {
				continue
			}
			address, err := resolve(ctx, userID)
			if err != nil {
				return nil, err
			}
			if err := add(address); err != nil {
				return nil, err
			}
		}
	}
	if len(recipients) > maxEmailRecipients {
		return nil, fmt.Errorf("the event has more than %d email recipients", maxEmailRecipients)
	}
	return recipients, nil
}
