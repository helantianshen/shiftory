package imageai

import (
	"context"
	"shiftory-server/internal/schedule"
	"time"
)

type Request struct {
	Description   string
	Timezone      string
	FixedNow      time.Time
	OnCall        func(context.Context, Call) error
	Image         []byte
	ImageFormat   string
	Start         schedule.Date
	End           schedule.Date
	Instructions  string
	ShiftMappings map[string]string
}
