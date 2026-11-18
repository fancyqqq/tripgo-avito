package positions

import "time"

type systemTicker struct {
	*time.Ticker
}

func NewTicker(interval time.Duration) Ticker {
	return systemTicker{Ticker: time.NewTicker(interval)}
}

func (t systemTicker) C() <-chan time.Time {
	return t.Ticker.C
}
