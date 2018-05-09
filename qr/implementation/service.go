package implementation

import (
	// stdlib imports
	"context"
	"errors"

	// external lib imports
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	qrcode "github.com/skip2/go-qrcode"

	// project imports
	"github.com/basvanbeek/opencensus-gokit-example/qr"
)

// Common Errors for QR Service
var (
	ErrInvalidRecoveryLevel = errors.New("invalid recovery level requested")
	ErrInvalidSize          = errors.New("invalid size requested")
	ErrGenerate             = errors.New("unable to generate QR")
)

// service implements qr.Service
type service struct {
	logger log.Logger
}

// NewService creates and returns new QR service instance
func NewService(logger log.Logger) qr.Service {
	return &service{
		logger: logger,
	}
}

// Generate returns a new QR code image based on provided details
func (s *service) Generate(
	ctx context.Context, url string, recLevel qr.RecoveryLevel, size int,
) ([]byte, error) {
	// test for valid input
	if recLevel < qr.LevelL || recLevel > qr.LevelH {
		return nil, ErrInvalidRecoveryLevel
	}
	if size < 25 || size > 4096 {
		return nil, ErrInvalidSize
	}

	// do the actual work
	b, err := qrcode.Encode(url, qrcode.RecoveryLevel(recLevel), size)
	if err != nil {
		// actual qrcode lib error... log it...
		level.Error(s.logger).Log("method", "Generate", "err", err.Error())
		// consumer of this api gets a generic error returned
		err = ErrGenerate
	}

	return b, err
}
