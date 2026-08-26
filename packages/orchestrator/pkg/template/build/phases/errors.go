//go:build linux

package phases

import (
	"errors"
)

type PhaseBuildError struct {
	Phase string
	Step  string
	Err   error

	// UserFacing controls whether the error is safe to expose as a build
	// configuration error. Internal phase diagnostics retain the phase/step for
	// operators without changing the public error classification.
	UserFacing bool
}

func (e *PhaseBuildError) Error() string {
	return e.Err.Error()
}

func (e *PhaseBuildError) Unwrap() error {
	return e.Err
}

func NewPhaseBuildError(phaseMetadata PhaseMeta, err error) *PhaseBuildError {
	return &PhaseBuildError{
		Phase:      string(phaseMetadata.Phase),
		Step:       stepString(phaseMetadata),
		Err:        err,
		UserFacing: true,
	}
}

// NewInternalPhaseBuildError preserves phase metadata for diagnostics while
// keeping the error classified as an internal failure.
func NewInternalPhaseBuildError(phaseMetadata PhaseMeta, err error) *PhaseBuildError {
	return &PhaseBuildError{
		Phase:      string(phaseMetadata.Phase),
		Step:       stepString(phaseMetadata),
		Err:        err,
		UserFacing: false,
	}
}

func UnwrapPhaseBuildError(err error) *PhaseBuildError {
	var phaseBuildError *PhaseBuildError
	if errors.As(err, &phaseBuildError) {
		return phaseBuildError
	}

	return nil
}
