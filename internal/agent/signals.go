package agent

import (
	"os"
	"os/signal"
)

type SignalController struct {
	ch chan os.Signal
}

func NewSignalController() *SignalController {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt)
	return &SignalController{ch: ch}
}

func (s *SignalController) Interrupted() bool {
	select {
	case <-s.ch:
		return true
	default:
		return false
	}
}

func (s *SignalController) Close() {
	signal.Stop(s.ch)
	close(s.ch)
}
