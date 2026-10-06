package main

import (
	"encoding/json"
	"fmt"
	"image"
	"image/draw"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMarkerDiagnosticReportsOnlyBoundedNumericFixtureEvidence(t *testing.T) {
	result, region := captureFixture()
	_, img, _ := decodeGUICapture(result, region)
	for _, sample := range fixtureMarkerDiagnostic(img) {
		if sample["mismatched_pixels"] != 0 || sample["matching_color_pixels"] != 289 {
			t.Fatal("诊断没有读取真实夹具像素", sample)
		}
	}
	changed := image.NewRGBA(img.Bounds())
	draw.Draw(changed, changed.Bounds(), img, image.Point{}, draw.Src)
	changed.Set(20, 20, fixtureColors[1])
	samples := fixtureMarkerDiagnostic(changed)
	if samples[0]["mismatched_pixels"] != 1 || samples[0]["matching_color_pixels"] != 288 {
		t.Fatal("中心像素偏差没有进入固定诊断", samples)
	}
	encoded, err := json.Marshal(samples)
	if err != nil || len(encoded) > 2048 || strings.Contains(string(encoded), guiTestText) {
		t.Fatal("数值诊断应有界且不输出测试内容")
	}
	expectPanic(t, func() { fixtureMarkerDiagnostic(image.NewRGBA(image.Rect(0, 0, 1001, 1000))) })
}

func TestCleanupFailurePreservesOriginalFailure(t *testing.T) {
	previous := guiCleanupErrors
	defer func() { guiCleanupErrors = previous }()
	guiCleanupErrors = nil
	var failure any
	func() {
		defer func() { failure = recover() }()
		defer cleanupWithCause("service_cleanup", func() { panic("Cleanup failed") })
		panic("Fixture markers were obscured or misplaced")
	}()
	message := fmt.Sprint(failure)
	if !strings.Contains(message, "Fixture markers were obscured or misplaced") || !strings.Contains(message, "service_cleanup") || len(guiCleanupErrors) != 1 {
		t.Fatal("原始失败和清理失败必须分别保留", failure)
	}
}

func TestGUIArtifactGuardRejectsWrongHashAndHelperDirectory(t *testing.T) {
	previousArtifacts, previousStage := guiArtifacts, guiStage
	defer func() { guiArtifacts, guiStage = previousArtifacts, previousStage }()
	root := t.TempDir()
	server := filepath.Join(root, "remote-mcp.exe")
	helper := filepath.Join(root, "native-helper.exe")
	if err := os.WriteFile(server, []byte("new server"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(helper, []byte("new helper"), 0600); err != nil {
		t.Fatal(err)
	}
	verifyGUIArtifacts(root, helper, hash([]byte("new server")), hash([]byte("new helper")))
	expectPanic(t, func() { verifyGUIArtifacts(root, helper, hash([]byte("old server")), hash([]byte("new helper"))) })
	expectPanic(t, func() {
		verifyGUIArtifacts(root, filepath.Join(t.TempDir(), "native-helper.exe"), hash([]byte("new server")), hash([]byte("new helper")))
	})
}

func TestFixtureStartupFailureStopsOnceAndPreservesCause(t *testing.T) {
	previous := guiCleanupErrors
	defer func() { guiCleanupErrors = previous }()
	for _, reason := range []string{"Native fixture startup failed", "Native fixture startup timed out"} {
		for _, cleanupFails := range []bool{false, true} {
			guiCleanupErrors = nil
			stops := 0
			var failure any
			func() {
				defer func() { failure = recover() }()
				failFixtureStartup(reason, func() {
					stops++
					if cleanupFails {
						panic("Fixture window cleanup timed out")
					}
				})
			}()
			if stops != 1 || !strings.Contains(fmt.Sprint(failure), reason) {
				t.Fatal("启动失败必须回收一次并保留原始原因", failure)
			}
			if cleanupFails {
				if len(guiCleanupErrors) != 1 || guiCleanupErrors[0] != "window_cleanup" {
					t.Fatal("启动清理失败必须独立记录窗口清理阶段")
				}
			} else if len(guiCleanupErrors) != 0 {
				t.Fatal("成功清理不能记录清理失败")
			}
		}
	}
}
