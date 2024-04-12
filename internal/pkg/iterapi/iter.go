package iterapi

import (
	"context"

	typesv1 "github.com/humanlogio/api/go/types/v1"
)

type Lister[Elem any] func(ctx context.Context, cursor *typesv1.Cursor, limit int32) ([]Elem, *typesv1.Cursor, error)

type Iter[Elem any] struct {
	ctx    context.Context
	lister Lister[Elem]
	limit  int32

	i      int
	items  []Elem
	cursor *typesv1.Cursor
	err    error
}

func New[Elem any](ctx context.Context, limit int32, lister Lister[Elem]) *Iter[Elem] {
	return &Iter[Elem]{
		ctx:    ctx,
		lister: lister,
		limit:  limit,
	}
}

func (iter *Iter[Elem]) Next() bool {

	if iter.i == -1 || // first call
		iter.i+1 >= len(iter.items) { // or reached last item

		if len(iter.items) > 0 && len(iter.items) < int(iter.limit) {
			// last list call returned less than limit
			return false
		}
		iter.items, iter.cursor, iter.err = iter.lister(iter.ctx, iter.cursor, iter.limit)
		if iter.err != nil {
			return false
		}
		if len(iter.items) == 0 {
			return false
		}
		iter.i = 0
	} else {
		iter.i++
	}

	return true
}

func (iter *Iter[Elem]) Current() Elem {
	return iter.items[iter.i]
}

func (iter *Iter[Elem]) Err() error {
	return iter.err
}

func Find[Elem any](iter *Iter[Elem], lookup func(Elem) bool) (Elem, bool, error) {
	for iter.Next() {
		cur := iter.Current()
		if lookup(cur) {
			return cur, true, nil
		}
	}
	var e Elem
	return e, false, iter.Err()
}
