package rushline

import (
	"testing"
	"time"
)

func TestNewEvent(t *testing.T) {
	tests := []struct {
		name               string
		id                 string
		capacity           int
		onsaleOpening      time.Time
		holdCreationCutoff time.Time
		confirmationCutoff time.Time
		wantErr            string
	}{
		{"has id", "", 2, now, now.Add(time.Hour), now.Add(time.Hour + time.Minute), "id is required"},
		{"has non-zero capacity", "1", 0, now, now.Add(time.Hour), now.Add(time.Hour + time.Minute), "must request positive capacity"},
		{"has positive capacity", "1", -2, now, now.Add(time.Hour), now.Add(time.Hour + time.Minute), "must request positive capacity"},
		{"has hold creation cutoff occurring after onsale opening", "1", 2, now.Add(time.Hour), now, now.Add(time.Minute), "hold creation cutoff must be after onsale opening"},
		{"has confirmation cutoff occurring after hold creation cutoff", "1", 2, now, now.Add(time.Minute), now, "confirmation must be after hold creation cutoff"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, got := newEvent(tt.id, tt.capacity, tt.onsaleOpening, tt.holdCreationCutoff, tt.confirmationCutoff)
			if got == nil {
				t.Errorf("newEvent wanted error: '%v'", tt.wantErr)
			}
			if got != nil && got.Error() != tt.wantErr {
				t.Errorf("newEvent validation error: '%v'; want '%v'", got.Error(), tt.wantErr)
			}
		})
	}
}
