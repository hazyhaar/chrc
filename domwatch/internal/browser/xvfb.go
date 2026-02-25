// CLAUDE:SUMMARY Starts and stops an Xvfb virtual display for headful stealth browser mode.
package browser

import (
	"fmt"
	"os/exec"
	"time"
)

// startXvfb launches an Xvfb virtual display for headful stealth mode.
func (m *Manager) startXvfb() error {
	if m.xvfb != nil {
		return nil // already running
	}

	display := m.cfg.XvfbDisplay
	cmd := exec.Command("Xvfb", display, "-screen", "0", "1920x1080x24", "-ac")
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start xvfb: %w", err)
	}
	m.xvfb = cmd

	// Give Xvfb a moment to initialise.
	time.Sleep(500 * time.Millisecond)

	m.cfg.Logger.Info("browser: xvfb started", "display", display, "pid", cmd.Process.Pid)
	return nil
}

// stopXvfb kills the Xvfb process if running.
func (m *Manager) stopXvfb() {
	if m.xvfb == nil {
		return
	}
	if m.xvfb.Process != nil {
		m.xvfb.Process.Kill()
		m.xvfb.Wait()
	}
	m.cfg.Logger.Info("browser: xvfb stopped")
	m.xvfb = nil
}
