// Package inspection 提供有界的系统快照和分阶段网络探测。
package inspection

import "time"

type Config struct {
	DefaultTimeout  time.Duration
	MaxTimeout      time.Duration
	MaxConcurrent   int
	MaxProcesses    int
	MaxFDs          int
	MaxCommandBytes int
	MaxNameBytes    int
	MaxResultBytes  int
	MaxHeaderBytes  int64
}

func DefaultConfig() Config {
	return Config{5 * time.Second, 10 * time.Second, 4, 16384, 65536, 1 << 20, 1024, 1 << 20, 64 << 10}
}
func (c Config) Validate() error {
	if c.DefaultTimeout <= 0 || c.MaxTimeout < c.DefaultTimeout || c.MaxTimeout > 10*time.Second || c.MaxConcurrent <= 0 || c.MaxConcurrent > 4 || c.MaxProcesses <= 0 || c.MaxProcesses > 16384 || c.MaxFDs <= 0 || c.MaxFDs > 65536 || c.MaxCommandBytes <= 0 || c.MaxCommandBytes > 1<<20 || c.MaxNameBytes <= 0 || c.MaxNameBytes > 1024 || c.MaxResultBytes < 4096 || c.MaxResultBytes > 1<<20 || c.MaxHeaderBytes <= 0 || c.MaxHeaderBytes > 64<<10 {
		return failure("invalid_argument", "Inspection limits or timeouts are invalid")
	}
	return nil
}

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string            { return e.Code + ": " + e.Message }
func failure(code, message string) *Error { return &Error{code, message} }

type Result struct {
	OK        bool      `json:"ok"`
	Code      string    `json:"code,omitempty"`
	Message   string    `json:"message,omitempty"`
	Partial   bool      `json:"partial"`
	Truncated bool      `json:"truncated"`
	Warnings  []Warning `json:"warnings,omitempty"`
}
type Warning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (r Result) outcome() Result { return r }
func (r *Result) fail(err error) {
	e, ok := err.(*Error)
	if !ok {
		e = failure("io_error", "Inspection failed while reading system data")
	}
	r.OK = false
	r.Code = e.Code
	r.Message = e.Message
}
func (r *Result) warn(code, msg string) {
	r.Partial = true
	for _, w := range r.Warnings {
		if w.Code == code && w.Message == msg {
			return
		}
	}
	if len(r.Warnings) < 16 {
		r.Warnings = append(r.Warnings, Warning{code, msg})
	}
}

type EnvironmentInput struct {
	Runtimes  []string `json:"runtimes,omitempty" jsonschema:"Optional subset of go, node, python, java, dotnet; defaults to all five"`
	TimeoutMS int      `json:"timeout_ms,omitempty" jsonschema:"Total budget in milliseconds; defaults to 5000, maximum 10000"`
}
type SnapshotInput struct {
	RootPID   int `json:"root_pid,omitempty" jsonschema:"Return this process and its descendants; omit for the visible process snapshot"`
	Limit     int `json:"limit,omitempty" jsonschema:"Maximum returned entries; default 256, maximum 4096"`
	TimeoutMS int `json:"timeout_ms,omitempty" jsonschema:"Total budget in milliseconds; defaults to 5000, maximum 10000"`
}
type ListenersInput struct {
	PID       int `json:"pid,omitempty" jsonschema:"Optional owner PID filter"`
	Port      int `json:"port,omitempty" jsonschema:"Optional TCP port filter"`
	Limit     int `json:"limit,omitempty" jsonschema:"Maximum returned entries; default 256, maximum 4096"`
	TimeoutMS int `json:"timeout_ms,omitempty" jsonschema:"Total budget in milliseconds; defaults to 5000, maximum 10000"`
}
type EnvironmentResult struct {
	Result
	OS           string    `json:"os"`
	Arch         string    `json:"arch"`
	Kernel       string    `json:"kernel"`
	Hostname     string    `json:"hostname"`
	Runtimes     []Runtime `json:"runtimes"`
	Capabilities []string  `json:"capabilities"`
}
type Runtime struct {
	Name      string `json:"name"`
	Available bool   `json:"available"`
	Path      string `json:"path,omitempty"`
	Version   string `json:"version,omitempty"`
	Truncated bool   `json:"truncated"`
	Code      string `json:"code,omitempty"`
	Message   string `json:"message,omitempty"`
}
type Process struct {
	PID  int    `json:"pid"`
	PPID int    `json:"ppid"`
	Name string `json:"name"`
}
type ProcessesResult struct {
	Result
	RootPID    int       `json:"root_pid"`
	Processes  []Process `json:"processes"`
	Scanned    int       `json:"scanned"`
	Visibility string    `json:"visibility"`
}
type Listener struct {
	Protocol      string `json:"protocol"`
	Address       string `json:"address"`
	Port          int    `json:"port"`
	PIDs          []int  `json:"pids"`
	PIDVisibility string `json:"pid_visibility"`
	ScopeID       uint32 `json:"scope_id,omitempty"`
}
type ListenersResult struct {
	Result
	Listeners        []Listener `json:"listeners"`
	ScannedProcesses int        `json:"scanned_processes"`
	ScannedFDs       int        `json:"scanned_fds"`
	PIDVisibility    string     `json:"pid_visibility"`
}
type ProbeInput struct {
	Mode      string `json:"mode" jsonschema:"Farthest stage: dns, tcp, tls, or http"`
	Target    string `json:"target" jsonschema:"Hostname for dns, host:port for tcp or tls, HTTP(S) URL for http; credentials and fragments are prohibited"`
	TimeoutMS int    `json:"timeout_ms,omitempty" jsonschema:"Total budget in milliseconds; defaults to 5000, maximum 10000"`
}
type Stage struct {
	Stage     string `json:"stage"`
	State     string `json:"state"`
	ElapsedMS int64  `json:"elapsed_ms"`
	Code      string `json:"code,omitempty"`
	Message   string `json:"message,omitempty"`
	Reason    string `json:"reason,omitempty"`
}
type Attempt struct {
	Address string `json:"address"`
	State   string `json:"state"`
	Reason  string `json:"reason,omitempty"`
}
type ProbeResult struct {
	Result
	FailedStage   string    `json:"failed_stage,omitempty"`
	Stages        []Stage   `json:"stages"`
	Addresses     []string  `json:"addresses"`
	Attempts      []Attempt `json:"attempts"`
	LocalAddress  string    `json:"local_address,omitempty"`
	RemoteAddress string    `json:"remote_address,omitempty"`
	TLSVersion    string    `json:"tls_version,omitempty"`
	CipherSuite   string    `json:"cipher_suite,omitempty"`
	StatusCode    int       `json:"status_code,omitempty"`
}
