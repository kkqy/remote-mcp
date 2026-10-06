package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

var guiStage = "initializing"
var guiDiagnostic object
var guiArtifacts object
var guiCleanupErrors []string

// 只保留本助手固定阶段、数值像素摘要与产物哈希，不保留截图或请求载荷。
func guiFailureSummary() object {
	return object{"ok": false, "gui_diagnostic": object{"stage": guiStage, "capture": guiDiagnostic, "artifacts": guiArtifacts, "cleanup_errors": guiCleanupErrors}}
}

func verifyGUIArtifacts(base, self, serverDigest, helperDigest string) {
	guiStage = "artifact_verification"
	ensure(strings.EqualFold(filepath.Clean(filepath.Dir(self)), filepath.Clean(base)), "GUI helper was not launched from its deployment directory")
	guiArtifacts = object{"server_sha256": artifactHash(filepath.Join(base, "remote-mcp.exe")), "helper_sha256": artifactHash(self)}
	ensure(guiArtifacts["server_sha256"] == serverDigest && guiArtifacts["helper_sha256"] == helperDigest, "GUI executable hash verification failed")
}

func artifactHash(path string) string {
	file, err := os.Open(path)
	check(err)
	defer file.Close()
	digest := sha256.New()
	count, err := io.Copy(digest, io.LimitReader(file, (64<<20)+1))
	check(err)
	ensure(count <= 64<<20, "Native executable exceeded the hash limit")
	return hex.EncodeToString(digest.Sum(nil))
}

// 启动失败时先保留原始原因，再取消并回收仍未交付给调用方的夹具。
func failFixtureStartup(reason string, stop func()) {
	defer cleanupWithCause("window_cleanup", stop)
	panic(reason)
}

// 清理失败不能覆盖最初的验收失败；这不是忽略清理错误，二者都会导致失败。
func cleanupWithCause(label string, cleanup func()) {
	original := recover()
	var failure any
	func() {
		defer func() { failure = recover() }()
		cleanup()
	}()
	if failure != nil {
		guiCleanupErrors = append(guiCleanupErrors, label)
		if original != nil {
			panic(fmt.Sprintf("%v; cleanup failed: %s", original, label))
		}
		panic(failure)
	}
	if original != nil {
		panic(original)
	}
}
