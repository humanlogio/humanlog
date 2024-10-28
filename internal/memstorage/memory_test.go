package memstorage

import (
	"log/slog"
	"os"
	"testing"

	"github.com/humanlogio/humanlog/pkg/localstorage"
)

func TestMemoryStorage(t *testing.T) {
	localstorage.RunTest(t, func(t *testing.T) localstorage.Storage {
		return NewMemStorage(slog.New(slog.NewTextHandler(os.Stderr, nil)))
	})
}
