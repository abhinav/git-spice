//go:build profile

package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"runtime/pprof"
	"runtime/trace"
)

type ProfileFlags struct {
	CPUProfile string `name:"cpuprofile" placeholder:"FILE" help:"Write CPU profile to file"`
	MemProfile string `name:"memprofile" placeholder:"FILE" help:"Write memory profile to file"`
	Trace      string `name:"trace" placeholder:"FILE" help:"Write trace to file"`

	cpuProfileFile io.WriteCloser
	memProfileFile io.WriteCloser
	traceFile      io.WriteCloser
}

func (pf *ProfileFlags) Start() error {
	return errors.Join(
		pf.startCPUProfile(),
		pf.startMemProfile(),
		pf.startTrace(),
	)
}

func (pf *ProfileFlags) Stop() error {
	return errors.Join(
		pf.stopCPUProfile(),
		pf.stopMemProfile(),
		pf.stopTrace(),
	)
}

func (pf *ProfileFlags) startCPUProfile() error {
	path := pf.CPUProfile
	if path == "" {
		return nil
	}

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create CPU profile file: %w", err)
	}

	if err := pprof.StartCPUProfile(f); err != nil {
		return fmt.Errorf("start CPU profile: %w", err)
	}

	pf.cpuProfileFile = f
	return nil
}

func (pf *ProfileFlags) stopCPUProfile() error {
	f := pf.cpuProfileFile
	if f == nil {
		return nil
	}

	pprof.StopCPUProfile()

	if err := f.Close(); err != nil {
		return fmt.Errorf("close CPU profile file: %w", err)
	}

	return nil
}

func (pf *ProfileFlags) startMemProfile() error {
	path := pf.MemProfile
	if path == "" {
		return nil
	}

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create memory profile file: %w", err)
	}

	pf.memProfileFile = f
	return nil
}

func (pf *ProfileFlags) stopMemProfile() error {
	f := pf.memProfileFile
	if f == nil {
		return nil
	}

	if err := pprof.Lookup("heap").WriteTo(f, 0); err != nil {
		return fmt.Errorf("write memory profile: %w", err)
	}

	if err := f.Close(); err != nil {
		return fmt.Errorf("close memory profile file: %w", err)
	}

	return nil
}

func (pf *ProfileFlags) startTrace() error {
	path := pf.Trace
	if path == "" {
		return nil
	}

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create trace file: %w", err)
	}

	if err := trace.Start(f); err != nil {
		return fmt.Errorf("start trace: %w", err)
	}

	pf.traceFile = f
	return nil
}

func (pf *ProfileFlags) stopTrace() error {
	f := pf.traceFile
	if f == nil {
		return nil
	}

	trace.Stop()

	if err := f.Close(); err != nil {
		return fmt.Errorf("close trace file: %w", err)
	}

	return nil
}
