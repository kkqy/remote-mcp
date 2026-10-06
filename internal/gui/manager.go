package gui

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"image"
	"image/draw"
	"image/png"
	"math"
	"reflect"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

type captureRecord struct {
	meta                    CaptureMetadata
	display                 Display
	frameWidth, frameHeight int
	expires                 time.Time
}
type session struct {
	status          Status
	request         OpenInput
	ctx             context.Context
	cancel          context.CancelFunc
	desktop         Desktop
	opened          chan struct{}
	closeDone       chan struct{}
	closeOnce       sync.Once
	closeErr        error
	busy            bool
	opDone          chan struct{}
	lastUsed, ended time.Time
	captures        map[string]captureRecord
	sequence        uint64
}
type Manager struct {
	cfg          Config
	backend      Backend
	mu           sync.Mutex
	sessions     map[string]*session
	requests     map[string]string
	closed       bool
	stop         chan struct{}
	wg           sync.WaitGroup
	shutdownDone chan struct{}
	closeErr     error
}

func New(cfg Config) (*Manager, error) { return NewWithBackend(cfg, newPlatformBackend(cfg)) }
func NewWithBackend(cfg Config, backend Backend) (*Manager, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if backend == nil {
		return nil, failure("invalid_argument", "GUI backend must not be nil")
	}
	m := &Manager{cfg: cfg, backend: backend, sessions: map[string]*session{}, requests: map[string]string{}, stop: make(chan struct{}), shutdownDone: make(chan struct{})}
	m.wg.Add(1)
	go m.sweep()
	return m, nil
}
func randomID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("Unable to obtain system randomness")
	}
	return hex.EncodeToString(b[:])
}
func copyStatus(s Status) Status {
	s.Displays = append([]Display{}, s.Displays...)
	if s.Capabilities.Reasons != nil {
		r := map[string]string{}
		for k, v := range s.Capabilities.Reasons {
			r[k] = v
		}
		s.Capabilities.Reasons = r
	}
	return s
}
func setStatusError(s *Status, err error) {
	var e *Error
	if errors.As(err, &e) {
		s.Code, s.Message = e.Code, e.Message
	} else {
		s.Code, s.Message = "session_closed", "GUI session is no longer valid"
	}
}
func (m *Manager) Open(ctx context.Context, in OpenInput) (Status, error) {
	if len(in.RequestID) < 1 || len(in.RequestID) > 256 || strings.TrimSpace(in.RequestID) != in.RequestID || in.WaitMS < 0 || in.WaitMS > 10000 {
		return Status{}, failure("invalid_argument", "Invalid request ID or wait time")
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return Status{}, failure("session_closed", "GUI manager is closed")
	}
	var s *session
	if id, ok := m.requests[in.RequestID]; ok {
		s = m.sessions[id]
		if s.request != in {
			m.mu.Unlock()
			return Status{}, failure("conflict", "The same request ID was used with different parameters")
		}
	} else {
		for _, existing := range m.sessions {
			if existing.status.State == "ready" || existing.status.State == "authorizing" || existing.status.State == "closing" {
				m.mu.Unlock()
				return Status{}, failure("busy", "The current desktop already has a GUI session")
			}
		}
		m.trimLocked(time.Now())
		if len(m.sessions) >= m.cfg.MaxRecords {
			m.mu.Unlock()
			return Status{}, failure("limit_exceeded", "GUI resource record limit reached")
		}
		lifetime, cancel := context.WithCancel(context.Background())
		s = &session{status: Status{ID: randomID(), State: "authorizing", Displays: []Display{}}, request: in, ctx: lifetime, cancel: cancel, opened: make(chan struct{}), closeDone: make(chan struct{}), lastUsed: time.Now(), captures: map[string]captureRecord{}}
		m.sessions[s.status.ID] = s
		m.requests[in.RequestID] = s.status.ID
		m.wg.Add(1)
		go m.authorize(s)
	}
	s.lastUsed = time.Now()
	m.mu.Unlock()
	if in.WaitMS > 0 {
		t := time.NewTimer(time.Duration(in.WaitMS) * time.Millisecond)
		defer t.Stop()
		select {
		case <-s.opened:
		case <-t.C:
		case <-ctx.Done():
			m.mu.Lock()
			authorizing := s.status.State == "authorizing"
			m.mu.Unlock()
			if authorizing {
				s.cancel()
			}
			return Status{}, contextError(ctx)
		}
	}
	m.mu.Lock()
	out := copyStatus(s.status)
	m.mu.Unlock()
	return out, nil
}
func (m *Manager) authorize(s *session) {
	defer m.wg.Done()
	defer close(s.opened)
	ctx, cancel := context.WithTimeout(s.ctx, m.cfg.AuthorizationTimeout)
	defer cancel()
	probe, err := m.backend.Probe(ctx)
	m.mu.Lock()
	s.status.Backend = probe.Backend
	m.mu.Unlock()
	var d Desktop
	if err == nil {
		d, err = m.backend.Open(ctx)
	}
	var displays []Display
	if err == nil {
		displays, err = d.Displays(ctx)
	}
	if err != nil {
		if d != nil {
			_ = d.Close()
		}
		if ctx.Err() != nil {
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				err = failure("authorization_timeout", "Desktop authorization timed out")
			} else {
				err = failure("authorization_cancelled", "Desktop authorization was cancelled")
			}
		}
		m.mu.Lock()
		if s.status.State == "authorizing" {
			s.status.State = "failed"
			setStatusError(&s.status, err)
			s.ended = time.Now()
		}
		m.mu.Unlock()
		return
	}
	m.mu.Lock()
	if s.status.State != "authorizing" || s.ctx.Err() != nil {
		m.mu.Unlock()
		_ = d.Close()
		return
	}
	s.desktop = d
	s.status.State = "ready"
	s.status.Capabilities = d.Capabilities()
	s.status.Displays = displays
	s.status.LayoutGeneration = 1
	m.mu.Unlock()
	if h, ok := d.(desktopHealth); ok {
		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			select {
			case <-h.Done():
				m.mu.Lock()
				if s.status.State == "ready" {
					s.status.State = "failed"
					setStatusError(&s.status, h.Err())
					s.ended = time.Now()
				}
				m.mu.Unlock()
				_ = m.closeResource(s, false)
			case <-s.ctx.Done():
			}
		}()
	}
}
func (m *Manager) Status(ctx context.Context, in StatusInput) (Status, error) {
	if in.ID == "" {
		ctx, cancel := context.WithTimeout(ctx, m.cfg.OperationTimeout)
		defer cancel()
		out, err := m.backend.Probe(ctx)
		if err != nil {
			setStatusError(&out, err)
			out.State = "unavailable"
		}
		if out.Displays == nil {
			out.Displays = []Display{}
		}
		return out, nil
	}
	m.mu.Lock()
	s := m.sessions[in.ID]
	if s == nil {
		m.mu.Unlock()
		return Status{}, failure("not_found", "GUI session not found")
	}
	s.lastUsed = time.Now()
	refresh := s.status.State == "ready" && !s.busy
	out := copyStatus(s.status)
	m.mu.Unlock()
	if refresh {
		updated, err := run(m, ctx, in.ID, false, func(ctx context.Context, s *session) (Status, error) {
			_, err := m.refresh(ctx, s)
			if err != nil {
				return Status{}, err
			}
			m.mu.Lock()
			defer m.mu.Unlock()
			return copyStatus(s.status), nil
		})
		if err == nil {
			return updated, nil
		}
		var business *Error
		if errors.As(err, &business) && business.Code == "busy" {
			return out, nil
		}
		return Status{}, err
	}
	return out, nil
}
func (m *Manager) CloseSession(ctx context.Context, in IDInput) (Status, error) {
	m.mu.Lock()
	s := m.sessions[in.ID]
	m.mu.Unlock()
	if s == nil {
		return Status{}, failure("not_found", "GUI session not found")
	}
	m.startClose(s, true)
	select {
	case <-s.closeDone:
		if s.closeErr != nil {
			return Status{}, failure("session_closed", "Failed to clean up GUI session resources")
		}
		m.mu.Lock()
		out := copyStatus(s.status)
		m.mu.Unlock()
		return out, nil
	case <-ctx.Done():
		return Status{}, contextError(ctx)
	}
}
func (m *Manager) startClose(s *session, explicit bool) {
	s.closeOnce.Do(func() {
		go func() {
			m.mu.Lock()
			failed := s.status.State == "failed"
			s.status.State = "closing"
			s.cancel()
			d := s.desktop
			op := s.opDone
			m.mu.Unlock()
			if d != nil {
				s.closeErr = d.Close()
			}
			<-s.opened
			if op != nil {
				<-op
			}
			m.mu.Lock()
			s.captures = map[string]captureRecord{}
			s.ended = time.Now()
			if failed && !explicit {
				s.status.State = "failed"
			} else {
				s.status.State = "closed"
			}
			if s.closeErr != nil {
				setStatusError(&s.status, failure("session_closed", "Failed to clean up GUI session resources"))
			}
			m.mu.Unlock()
			close(s.closeDone)
		}()
	})
}
func (m *Manager) closeResource(s *session, explicit bool) error {
	m.startClose(s, explicit)
	<-s.closeDone
	if s.closeErr != nil {
		return failure("session_closed", "Failed to clean up GUI session resources")
	}
	return nil
}
func (m *Manager) Close() error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		<-m.shutdownDone
		return m.closeErr
	}
	m.closed = true
	close(m.stop)
	list := make([]*session, 0, len(m.sessions))
	for _, s := range m.sessions {
		list = append(list, s)
	}
	m.mu.Unlock()
	var err error
	for _, s := range list {
		if e := m.closeResource(s, true); e != nil {
			err = e
		}
	}
	m.wg.Wait()
	m.mu.Lock()
	m.closeErr = err
	close(m.shutdownDone)
	m.mu.Unlock()
	return err
}
func (m *Manager) trimLocked(now time.Time) {
	for id, s := range m.sessions {
		if !s.ended.IsZero() && now.Sub(s.ended) >= m.cfg.Retention {
			delete(m.requests, s.request.RequestID)
			delete(m.sessions, id)
		}
	}
	if len(m.sessions) >= m.cfg.MaxRecords {
		var oldest *session
		for _, s := range m.sessions {
			if !s.ended.IsZero() && (oldest == nil || s.ended.Before(oldest.ended)) {
				oldest = s
			}
		}
		if oldest != nil {
			delete(m.requests, oldest.request.RequestID)
			delete(m.sessions, oldest.status.ID)
		}
	}
}
func (m *Manager) sweep() {
	defer m.wg.Done()
	t := time.NewTicker(m.cfg.SweepInterval)
	defer t.Stop()
	for {
		select {
		case <-m.stop:
			return
		case now := <-t.C:
			m.mu.Lock()
			var idle []*session
			for _, s := range m.sessions {
				if s.status.State == "ready" && !s.busy && now.Sub(s.lastUsed) >= m.cfg.IdleTimeout {
					idle = append(idle, s)
				}
				for id, c := range s.captures {
					if now.After(c.expires) {
						delete(s.captures, id)
					}
				}
			}
			m.trimLocked(now)
			m.mu.Unlock()
			for _, s := range idle {
				_ = m.closeResource(s, true)
			}
		}
	}
}

// run 持有占用直到实际后台操作结束；取消请求只结束等待，不提前允许下一个输入。
func run[T any](m *Manager, ctx context.Context, id string, input bool, fn func(context.Context, *session) (T, error)) (T, error) {
	var zero T
	if ctx.Err() != nil {
		return zero, contextError(ctx)
	}
	m.mu.Lock()
	s := m.sessions[id]
	if s == nil {
		m.mu.Unlock()
		return zero, failure("not_found", "GUI session not found")
	}
	if s.status.State != "ready" {
		m.mu.Unlock()
		return zero, failure("session_closed", "GUI session is not ready or has already closed")
	}
	if s.busy {
		m.mu.Unlock()
		return zero, failure("busy", "GUI session is running another operation")
	}
	s.busy = true
	s.lastUsed = time.Now()
	s.opDone = make(chan struct{})
	opDone := s.opDone
	m.mu.Unlock()
	opctx, cancel := context.WithTimeout(ctx, m.cfg.OperationTimeout)
	stop := context.AfterFunc(s.ctx, cancel)
	defer cancel()
	type result struct {
		out T
		err error
	}
	ch := make(chan result, 1)
	go func() {
		out, err := fn(opctx, s)
		if errors.Is(err, context.Canceled) {
			err = failure("authorization_cancelled", "GUI call was cancelled")
			if input {
				err = partialError(err)
			}
		} else if errors.Is(err, context.DeadlineExceeded) {
			err = failure("timeout", "Timed out waiting for the GUI operation")
			if input {
				err = partialError(err)
			}
		}
		stop()
		m.mu.Lock()
		s.busy = false
		s.opDone = nil
		s.lastUsed = time.Now()
		close(opDone)
		m.mu.Unlock()
		ch <- result{out, err}
	}()
	select {
	case r := <-ch:
		return r.out, r.err
	case <-opctx.Done():
		select {
		case r := <-ch:
			return r.out, r.err
		default:
		}
		err := contextError(opctx)
		if input {
			err = partialError(err)
		}
		return zero, err
	}
}
func (m *Manager) refresh(ctx context.Context, s *session) ([]Display, error) {
	displays, err := s.desktop.Displays(ctx)
	if err != nil {
		return nil, err
	}
	caps := s.desktop.Capabilities()
	m.mu.Lock()
	s.status.Capabilities = copyStatus(Status{Capabilities: caps}).Capabilities
	if !reflect.DeepEqual(displays, s.status.Displays) {
		s.status.LayoutGeneration++
		s.status.Displays = append([]Display{}, displays...)
		s.captures = map[string]captureRecord{}
	}
	m.mu.Unlock()
	return displays, nil
}

type limitedBuffer struct {
	buffer bytes.Buffer
	limit  int
}

func (b *limitedBuffer) Len() int      { return b.buffer.Len() }
func (b *limitedBuffer) Bytes() []byte { return b.buffer.Bytes() }

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.Len() {
		return 0, failure("limit_exceeded", "PNG screenshot exceeds the byte limit")
	}
	return b.buffer.Write(p)
}
func (m *Manager) Screenshot(ctx context.Context, in ScreenshotInput) (ScreenshotResult, error) {
	return run(m, ctx, in.ID, false, func(ctx context.Context, s *session) (ScreenshotResult, error) {
		if !s.desktop.Capabilities().Screenshot {
			return ScreenshotResult{}, failure("unsupported", "Screenshot capture is not available in this session")
		}
		displays, err := m.refresh(ctx, s)
		if err != nil {
			return ScreenshotResult{}, err
		}
		var d Display
		for _, candidate := range displays {
			if candidate.ID == in.DisplayID || (in.DisplayID == "" && (d.ID == "" || candidate.Primary)) {
				d = candidate
				if in.DisplayID != "" || candidate.Primary {
					break
				}
			}
		}
		if d.ID == "" {
			return ScreenshotResult{}, failure("not_found", "Display not found or not authorized")
		}
		if in.Region != nil {
			r := *in.Region
			if r.X < 0 || r.Y < 0 || r.Width <= 0 || r.Height <= 0 {
				return ScreenshotResult{}, failure("invalid_argument", "Invalid screenshot region")
			}
			if d.PixelWidth > 0 && (r.X > d.PixelWidth-r.Width || r.Y > d.PixelHeight-r.Height) {
				return ScreenshotResult{}, failure("invalid_argument", "Screenshot region is outside the display")
			}
		}
		if d.PixelWidth > 0 {
			if err := checkPixels(d.PixelWidth, d.PixelHeight, m.cfg.MaxPixels); err != nil {
				return ScreenshotResult{}, err
			}
		}
		frame, err := s.desktop.Capture(ctx, d)
		if err != nil {
			return ScreenshotResult{}, err
		}
		if frame.Image == nil {
			return ScreenshotResult{}, failure("capture_failed", "The backend did not return a screenshot")
		}
		bounds := frame.Image.Bounds()
		w, h := bounds.Dx(), bounds.Dy()
		if err := checkPixels(w, h, m.cfg.MaxPixels); err != nil {
			return ScreenshotResult{}, err
		}
		if frame.CapturedAt.IsZero() || frame.Freshness == "" {
			return ScreenshotResult{}, failure("capture_failed", "The screenshot has no reliable capture timestamp")
		}
		// 后端必须同时更新实际媒体尺寸，否则旧尺寸不能作为输入映射依据。
		displays, err = m.refresh(ctx, s)
		if err != nil {
			return ScreenshotResult{}, err
		}
		foundDisplay := false
		for _, current := range displays {
			if current.ID == d.ID {
				d = current
				foundDisplay = true
				break
			}
		}
		if !foundDisplay {
			return ScreenshotResult{}, failure("capture_failed", "The selected display was removed during capture; select a display again")
		}
		if d.PixelWidth != w || d.PixelHeight != h {
			return ScreenshotResult{}, failure("capture_failed", "Screenshot dimensions do not match the current display layout; try again")
		}
		region := Rect{Width: w, Height: h}
		if in.Region != nil {
			region = *in.Region
			if region.X > w-region.Width || region.Y > h-region.Height {
				return ScreenshotResult{}, failure("invalid_argument", "Screenshot region is outside the captured frame")
			}
		}
		crop := image.NewRGBA(image.Rect(0, 0, region.Width, region.Height))
		draw.Draw(crop, crop.Bounds(), frame.Image, bounds.Min.Add(image.Pt(region.X, region.Y)), draw.Src)
		encoded := &limitedBuffer{limit: m.cfg.MaxPNGBytes}
		if err := png.Encode(encoded, crop); err != nil {
			return ScreenshotResult{}, failure("limit_exceeded", "Encoded PNG screenshot exceeds the byte limit")
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		if s.status.State != "ready" {
			return ScreenshotResult{}, failure("session_closed", "The session closed during capture")
		}
		s.sequence++
		meta := CaptureMetadata{ID: s.status.ID, CaptureID: randomID(), DisplayID: d.ID, Width: region.Width, Height: region.Height, Region: region, LogicalBounds: d.LogicalBounds, LayoutGeneration: s.status.LayoutGeneration, CapturedAt: frame.CapturedAt, FrameSequence: s.sequence, Freshness: frame.Freshness}
		meta.CapturedAtSource = "acquired_at"
		if frame.Freshness == "latest_available" {
			meta.CapturedAtSource = "received_at"
		}
		if len(s.captures) >= m.cfg.MaxCaptures {
			var oldest string
			var expiration time.Time
			for id, c := range s.captures {
				if oldest == "" || c.expires.Before(expiration) {
					oldest = id
					expiration = c.expires
				}
			}
			delete(s.captures, oldest)
		}
		s.captures[meta.CaptureID] = captureRecord{meta, d, w, h, time.Now().Add(m.cfg.CaptureRetention)}
		return ScreenshotResult{meta, encoded.Bytes()}, nil
	})
}
func validPoint(x, y float64, w, h int) bool {
	return !math.IsNaN(x) && !math.IsNaN(y) && !math.IsInf(x, 0) && !math.IsInf(y, 0) && x >= 0 && y >= 0 && x < float64(w) && y < float64(h)
}
func (m *Manager) Mouse(ctx context.Context, in MouseInput) (InputResult, error) {
	if in.Action != "move" && in.Action != "click" && in.Action != "double_click" && in.Action != "drag" && in.Action != "scroll" {
		return InputResult{}, failure("invalid_argument", "Unknown mouse action")
	}
	if in.Button == "" {
		in.Button = "left"
	}
	if in.Button != "left" && in.Button != "middle" && in.Button != "right" {
		return InputResult{}, failure("invalid_argument", "Unknown mouse button")
	}
	if in.DurationMS < 0 || in.DurationMS > 10000 || in.ScrollX < -1000 || in.ScrollX > 1000 || in.ScrollY < -1000 || in.ScrollY > 1000 {
		return InputResult{}, failure("invalid_argument", "Mouse duration or scroll count exceeds the limit")
	}
	return run(m, ctx, in.ID, true, func(ctx context.Context, s *session) (InputResult, error) {
		if !s.desktop.Capabilities().Mouse {
			return InputResult{}, failure("unsupported", "Mouse input is not authorized in this session")
		}
		if _, err := m.refresh(ctx, s); err != nil {
			return InputResult{}, err
		}
		m.mu.Lock()
		c, ok := s.captures[in.CaptureID]
		generation := s.status.LayoutGeneration
		m.mu.Unlock()
		if !ok || time.Now().After(c.expires) || c.meta.LayoutGeneration != generation {
			return InputResult{}, failure("stale_capture", "Screenshot coordinates have expired or the display layout has changed")
		}
		if !c.display.AbsoluteInput {
			return InputResult{}, failure("unsupported", "The display has no reliable absolute coordinate mapping")
		}
		if !validPoint(in.X, in.Y, c.meta.Width, c.meta.Height) || (in.Action == "drag" && !validPoint(in.EndX, in.EndY, c.meta.Width, c.meta.Height)) {
			return InputResult{}, failure("invalid_argument", "Input coordinates must be inside the specified screenshot")
		}
		convert := func(x, y float64) Point {
			return Point{(x + float64(c.meta.Region.X)) * c.display.LogicalBounds.Width / float64(c.frameWidth), (y + float64(c.meta.Region.Y)) * c.display.LogicalBounds.Height / float64(c.frameHeight)}
		}
		ev := MouseEvent{Display: c.display, Action: in.Action, Button: in.Button, Start: convert(in.X, in.Y), End: convert(in.EndX, in.EndY), ScrollX: in.ScrollX, ScrollY: in.ScrollY, Duration: time.Duration(in.DurationMS) * time.Millisecond}
		if in.Action == "drag" && ev.Duration == 0 {
			ev.Duration = 300 * time.Millisecond
		}
		if err := s.desktop.Mouse(ctx, ev); err != nil {
			return InputResult{}, err
		}
		return InputResult{Submitted: true}, nil
	})
}
func (m *Manager) Key(ctx context.Context, in KeyInput) (InputResult, error) {
	keys, err := normalizeKeys(in.Keys)
	if err != nil {
		return InputResult{}, err
	}
	return run(m, ctx, in.ID, true, func(ctx context.Context, s *session) (InputResult, error) {
		if !s.desktop.Capabilities().Keyboard {
			return InputResult{}, failure("unsupported", "Keyboard input is not authorized in this session")
		}
		if err := s.desktop.Key(ctx, keys); err != nil {
			return InputResult{}, err
		}
		return InputResult{Submitted: true}, nil
	})
}
func (m *Manager) Text(ctx context.Context, in TextInput) (InputResult, error) {
	if len(in.Text) > m.cfg.MaxTextBytes {
		return InputResult{}, failure("limit_exceeded", "Text exceeds the byte limit")
	}
	if !utf8.ValidString(in.Text) || len(in.Text) == 0 {
		return InputResult{}, failure("invalid_argument", "Text must be nonempty UTF-8 and within the byte limit")
	}
	if in.Mode == "" {
		in.Mode = "auto"
	}
	if in.Mode != "auto" && in.Mode != "direct" && in.Mode != "clipboard" {
		return InputResult{}, failure("invalid_argument", "Unknown text input mode")
	}
	if len(in.PasteKeys) > 0 {
		keys, err := normalizeKeys(in.PasteKeys)
		if err != nil {
			return InputResult{}, err
		}
		in.PasteKeys = keys
	}
	return run(m, ctx, in.ID, true, func(ctx context.Context, s *session) (InputResult, error) {
		caps := s.desktop.Capabilities()
		if !caps.Text {
			return InputResult{}, failure("unsupported", "Text input is not available in this session")
		}
		if in.Mode == "auto" {
			if caps.DirectText {
				in.Mode = "direct"
			} else {
				in.Mode = "clipboard"
			}
		}
		if in.Mode == "direct" && !caps.DirectText {
			return InputResult{}, failure("unsupported", "The current backend does not support direct text input")
		}
		if in.Mode == "clipboard" && !caps.Clipboard {
			return InputResult{}, failure("unsupported", "The current backend does not support clipboard input")
		}
		return s.desktop.Text(ctx, in)
	})
}
