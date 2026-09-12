package store

// ListResourceFilter carries a translated CEL filter: the SQL fragment and the
// positional arguments that belong to it.
type ListResourceFilter struct {
	Args  []any
	Where string
}

// ExtraArgs is one additional predicate appended to a meta registry query.
type ExtraArgs struct {
	Left  string
	Op    string
	Right any
}
