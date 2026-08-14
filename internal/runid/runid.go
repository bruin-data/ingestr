package runid

import (
	"strings"

	"github.com/google/uuid"
)

func New() string {
	return strings.ReplaceAll(uuid.NewString(), "-", "")
}
