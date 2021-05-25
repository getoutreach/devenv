package devenvutil

import (
	"context"
	"fmt"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/sirupsen/logrus"
)

// Backoff is a light wrapper around the backoff library
func Backoff(ctx context.Context, d time.Duration, max uint64, fn func() error, log logrus.FieldLogger) error {
	t := backoff.WithContext(backoff.WithMaxRetries(backoff.NewConstantBackOff(d), max), ctx)
	for {
		err := fn()
		if err == nil {
			return nil
		}

		waitTime := t.NextBackOff()
		if waitTime == backoff.Stop { // this is hit when max attempts or context is canceled
			return fmt.Errorf("reached maximum attempts")
		}

		if log != nil {
			log.Infof("Retrying operation in %s", waitTime)
		}

		t := time.NewTicker(waitTime)
		defer t.Stop()

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			continue
		}
	}
}
