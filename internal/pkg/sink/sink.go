package sink

import "github.com/humanlogio/humanlog/internal/pkg/model"

type Sink interface {
	Receive(*model.Event) error
}
