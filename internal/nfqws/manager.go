// Package nfqws используется для управления процессами nfqws.
// Обрабатывает запросы последовательно из канала.
package nfqws

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sync"
)

type Manager struct {
	progPath string

	reqCh chan Req   // Канал для получения запросов Manager
	errCh chan error // Возвращаем ошибки

	procs map[int]*exec.Cmd

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}
type Req struct {
	Queue int
	Args  string
}

func NewManager(progPath string, reqCh chan Req, errCh chan error) *Manager {
	procs := make(map[int]*exec.Cmd)
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{
		progPath: progPath,
		reqCh:    reqCh, errCh: errCh,
		procs: procs, ctx: ctx, cancel: cancel,
	}
}

func (m *Manager) Start() {
	m.wg.Add(1)
	go m.run()
}

func (m *Manager) Stop() {
	m.cancel()
	m.wg.Wait()
	close(m.reqCh)
	close(m.errCh)
}

func (m *Manager) run() {
	defer m.wg.Done()
	for {
		select {
		case req := <-m.reqCh:
			m.errCh <- m.handleReq(req)
		case <-m.ctx.Done():
			m.errCh <- m.killAll()
			return
		}
	}
}

func (m *Manager) handleReq(req Req) error {
	_, ok := m.procs[req.Queue]
	if ok {
		// Завершаем
		if err := m.killProc(req.Queue); err != nil {
			return err
		}
	}
	// Запускаем
	err := m.startProc(req)
	return err
}

func (m *Manager) killProc(q int) error {
	cmd, ok := m.procs[q]
	if !ok {
		return nil
	}
	if err := cmd.Process.Kill(); err != nil {
		if err != os.ErrProcessDone {
			return err
		}
	}
	return nil
}

func (m *Manager) killAll() error {
	for q := range m.procs {
		if err := m.killProc(q); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) startProc(req Req) error {
	qFlag := fmt.Sprintf("-qnum=%d", req.Queue)
	cmd := exec.Command(m.progPath, qFlag, req.Args)
	if err := cmd.Start(); err != nil {
		return err
	}
	m.procs[req.Queue] = cmd
	return nil
}
