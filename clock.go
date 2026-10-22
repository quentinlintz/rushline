package rushline

import (
	"time"
)

type Clock interface {
	Now() time.Time
}

type RealClock struct{}

func (RealClock) Now() time.Time {
	return time.Now()
}

type MockClock struct {
	StaticTime time.Time
}

func (m *MockClock) Now() time.Time {
	return m.StaticTime
}
