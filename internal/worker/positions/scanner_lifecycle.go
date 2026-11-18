package positions

import "context"

func (s *Scanner) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrStopped
	}
	if s.started {
		return ErrAlreadyStarted
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	ticker := s.newTicker(s.config.CheckInterval)
	ctx, s.cancel = context.WithCancel(ctx)
	s.started = true
	go s.run(ctx, ticker)
	return nil
}

func (s *Scanner) Shutdown(ctx context.Context) error {
	s.stop()
	select {
	case <-s.done:
		return nil
	default:
	}
	select {
	case <-s.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Scanner) Close() {
	s.stop()
	<-s.done
}

func (s *Scanner) Done() <-chan struct{} {
	return s.done
}

func (s *Scanner) stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	if s.started {
		s.cancel()
	} else {
		close(s.done)
	}
}

func (s *Scanner) finish(ctx context.Context, ticker Ticker) {
	ticker.Stop()
	s.cancel()
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	s.logger.InfoContext(context.WithoutCancel(ctx), "position scanner stopped")
	close(s.done)
}
