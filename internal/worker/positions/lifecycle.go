package positions

import "context"

func (p *Pool) Start(ctx context.Context) error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return ErrStopped
	}
	if p.started {
		p.mu.Unlock()
		return ErrAlreadyStarted
	}
	if err := ctx.Err(); err != nil {
		p.mu.Unlock()
		return err
	}
	p.ctx, p.cancel = context.WithCancel(ctx)
	p.started = true
	p.wg.Add(p.config.Workers)
	for range p.config.Workers {
		go p.work(p.ctx)
	}
	go p.waitWorkers()
	p.mu.Unlock()
	p.logger.InfoContext(ctx, "position workers started", "workers", p.config.Workers)
	return nil
}

func (p *Pool) Shutdown(ctx context.Context) error {
	p.stop(false)
	select {
	case <-p.done:
		return nil
	default:
	}
	select {
	case <-p.done:
		return nil
	case <-ctx.Done():
		p.stop(true)
		return ctx.Err()
	}
}

func (p *Pool) Close() {
	p.stop(true)
	<-p.done
}

func (p *Pool) stop(force bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.started {
		if !p.closed {
			p.closeQueue()
			p.discardQueued()
			close(p.done)
		}
		return
	}
	p.closeQueue()
	if force {
		p.cancel()
	}
}

func (p *Pool) waitWorkers() {
	p.wg.Wait()
	p.cancel()
	p.mu.Lock()
	p.closeQueue()
	p.discardQueued()
	p.mu.Unlock()
	p.logger.InfoContext(context.WithoutCancel(p.ctx), "position workers stopped")
	close(p.done)
}

func (p *Pool) closeQueue() {
	if !p.closed {
		p.closed = true
		close(p.queue)
	}
}

func (p *Pool) discardQueued() {
	for range p.queue {
	}
	clear(p.pending)
}
