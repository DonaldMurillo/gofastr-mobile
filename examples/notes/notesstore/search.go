package notesstore

import (
	"context"
	"github.com/DonaldMurillo/gofastr-native/examples/notes/notes"
	"sort"
)

// Relevance ranks one bounded 100-note recency window, then pages through it.
// The continuation anchor remains in repository order, never in score order.
func (store *Store) relevantCollection(ctx context.Context, query notes.Query) (notes.Collection, error) {
	window := query
	window.Order = "recent"
	window.Page = notes.MaximumRows / notes.PageSize
	result, err := store.Collection(ctx, window)
	if err != nil {
		return result, err
	}
	var boundary string
	if len(result.Notes) > 0 {
		boundary = result.Notes[len(result.Notes)-1].ID
	}
	more := result.Next != ""
	sort.SliceStable(result.Notes, func(i, j int) bool {
		a, b := notes.SearchRank(result.Notes[i], query.Search), notes.SearchRank(result.Notes[j], query.Search)
		return a < b
	})
	count := max(query.Page, 1) * notes.PageSize
	result.Next = ""
	if len(result.Notes) > count {
		result.Notes = result.Notes[:count]
		query.Page = max(query.Page, 1) + 1
		result.Next = query.PageValue()
	} else if more {
		query.After = &notes.Cursor{ID: boundary}
		query.Page = 1
		result.Next = query.PageValue()
	}
	result.Total = len(result.Notes)
	return result, nil
}
