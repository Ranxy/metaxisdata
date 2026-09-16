package cmd

import "context"

// page is one response from a list call.
type page[T any] struct {
	items []T
	next  string
}

// collectPages walks a listing until it is exhausted or the item budget is
// reached. The budgets matter: the server pages at ten or fifty items by
// default, so a client that reads one page silently loses data, which is far
// worse for an agent than a clear truncation.
func collectPages[T any](ctx context.Context, pageSize int32, maxItems int, defaultPageSize int32,
	fetch func(ctx context.Context, pageToken string, pageSize int32) (page[T], error)) (items []T, truncated bool, nextToken string, err error) {
	if pageSize <= 0 {
		pageSize = defaultPageSize
	}
	if maxItems <= 0 {
		maxItems = defaultMaxItems
	}

	token := ""
	for {
		response, err := fetch(ctx, token, pageSize)
		if err != nil {
			return nil, false, "", err
		}
		items = append(items, response.items...)

		// An empty token ends the listing; a token that does not advance would
		// loop forever.
		if response.next == "" || response.next == token {
			return items, false, "", nil
		}
		if len(items) >= maxItems {
			return items[:maxItems], true, response.next, nil
		}
		token = response.next
	}
}

// defaultMaxItems bounds one listing unless the caller says otherwise.
const defaultMaxItems = 1000
