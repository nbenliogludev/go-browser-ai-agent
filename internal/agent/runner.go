package agent

import (
	"errors"
	"time"
)

var (
	ErrInterrupted  = errors.New("execution interrupted")
	ErrMaxSteps     = errors.New("max steps reached")
	ErrSnapshotFail = errors.New("snapshot error")
	ErrLLMFail      = errors.New("llm error")
)

type Runner struct {
	agent      *Agent
	task       string
	maxSteps   int
	mem        *StepMemory
	prevSnap   *PageSnapshotWrapper
	reporter   *Reporter
	signalCtrl *SignalController
}

func NewRunner(a *Agent, task string, maxSteps int) *Runner {
	return &Runner{
		agent:      a,
		task:       task,
		maxSteps:   maxSteps,
		mem:        NewStepMemory(10, 3),
		reporter:   NewReporter(a.llm, task),
		signalCtrl: NewSignalController(),
	}
}

func (r *Runner) Run() error {
	start := time.Now()
	defer r.signalCtrl.Close()

	for step := 1; step <= r.maxSteps; step++ {
		if r.signalCtrl.Interrupted() {
			r.reporter.Interrupted(start, r.mem)
			return ErrInterrupted
		}

		finished, err := r.executeStep(step)
		if err != nil {
			r.reporter.StepError(err)
		}

		if finished {
			r.reporter.Finished(start, r.mem)
			return nil
		}

		time.Sleep(3 * time.Second)
	}

	r.reporter.MaxStepsReached(start, r.mem)
	return ErrMaxSteps
}
