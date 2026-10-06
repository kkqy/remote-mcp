#!/usr/bin/env python3
"""复用 Windows 部署生命周期，仅在 --execute 下运行专用原生 GUI 验收窗口。"""
import importlib.util
from pathlib import Path

spec = importlib.util.spec_from_file_location("windows_native_gui_deployment", Path(__file__).with_name("windows-native-smoke.py"))
native = importlib.util.module_from_spec(spec)
spec.loader.exec_module(native)


def validate_summary(summary, arch):
    """两种鉴权及真实事件证据齐备，仍保留整体 GUI 验收未完成。"""
    assert isinstance(summary, dict) and summary.get("ok") is True, "Invalid native GUI summary"
    assert summary.get("platform") == "windows" and summary.get("arch") == arch, "Native GUI platform mismatch"
    assert summary.get("protocol_version") == "2025-11-25", "Native GUI protocol mismatch"
    assert summary.get("gui_exercised") is True and summary.get("clipboard_accessed") is False, "Native GUI capability mismatch"
    artifacts = summary.get("artifacts")
    assert isinstance(artifacts, dict) and set(artifacts) == {"server_sha256", "helper_sha256"}, "Missing native GUI executable evidence"
    assert all(isinstance(value, str) and len(value) == 64 and set(value) <= set("0123456789abcdef") for value in artifacts.values()), "Invalid native GUI executable digest"
    assert summary.get("acceptance_complete") is False, "Basic GUI validation cannot declare complete acceptance"
    required_pending = {"multiple_displays_and_mixed_scaling", "layout_changes", "permission_revocation", "MCP_client_image_display"}
    pending = summary.get("pending")
    assert isinstance(pending, list) and required_pending <= set(pending), "Native GUI pending coverage was lost"
    rounds = summary.get("rounds")
    assert isinstance(rounds, list) and len(rounds) == 2, "Native GUI must complete two authentication rounds"
    seen = set()
    for result in rounds:
        assert isinstance(result, dict) and result.get("suite") == "gui", "Invalid native GUI round"
        mode = result.get("authentication")
        assert mode in ("token", "anonymous") and mode not in seen, "Missing or duplicate GUI authentication round"
        assert result.get("ok") is True and result.get("service_shutdown") == "terminate_process", "Native GUI service cleanup failed"
        evidence = result.get("evidence")
        assert isinstance(evidence, dict), "Native GUI evidence is missing"
        for key in ("utf8_text_verified", "region_verified", "markers_verified", "coordinate_events_verified", "foreground_guard", "repeated_close", "window_cleaned"):
            assert evidence.get(key) is True, "Native GUI verification failed: " + key
        assert evidence.get("clipboard_accessed") is False, "Native GUI must not access the clipboard"
        for key in ("clicks", "double_clicks", "drags", "select_all", "backspaces", "width", "height", "display_count"):
            assert type(evidence.get(key)) is int and evidence[key] > 0, "Native GUI event or dimensions missing: " + key
        for key in ("vertical_scroll", "horizontal_scroll"):
            assert type(evidence.get(key)) is int and evidence[key] != 0, "Native GUI scrolling missing: " + key
        digests = [evidence.get(key) for key in ("png_before_sha256", "png_after_sha256")]
        assert all(isinstance(value, str) and len(value) == 64 and set(value) <= set("0123456789abcdef") for value in digests), "Invalid native GUI PNG digests"
        assert digests[0] != digests[1], "Native GUI screenshots did not change"
        seen.add(mode)
    assert seen == {"token", "anonymous"}, "Native GUI authentication coverage is incomplete"


def main():
    native.main(gui_validator=validate_summary, gui_diagnostic_validator=validate_diagnostic)


def validate_diagnostic(summary):
    """只允许助手产生的固定阶段、四种夹具颜色计数和公开产物哈希。"""
    assert isinstance(summary, dict) and set(summary) == {"ok", "gui_diagnostic"} and summary["ok"] is False, "Invalid GUI failure diagnostic"
    diagnostic = summary["gui_diagnostic"]
    assert isinstance(diagnostic, dict) and set(diagnostic) == {"stage", "capture", "artifacts", "cleanup_errors"}, "Unexpected GUI diagnostic fields"
    stages = {"initializing", "artifact_verification", "service_starting", "tool_discovery", "session_open", "fixture_start", "initial_capture", "before_click", "before_double_click", "before_drag", "before_scroll", "key_input", "direct_text", "final_capture", "session_close", "window_close"}
    assert diagnostic["stage"] in stages, "Unexpected GUI diagnostic stage"
    cleanup_errors = diagnostic["cleanup_errors"]
    allowed_cleanup = {"session_cleanup", "window_cleanup", "service_cleanup", "service_log_cleanup", "round_directory_cleanup"}
    assert cleanup_errors is None or (isinstance(cleanup_errors, list) and len(cleanup_errors) <= 5 and set(cleanup_errors) <= allowed_cleanup), "Unexpected GUI cleanup errors"
    artifacts = diagnostic["artifacts"]
    if artifacts is not None:
        assert isinstance(artifacts, dict) and set(artifacts) == {"server_sha256", "helper_sha256"}, "Unexpected GUI artifact fields"
        assert all(isinstance(value, str) and len(value) == 64 and set(value) <= set("0123456789abcdef") for value in artifacts.values()), "Invalid GUI artifact digest"
    capture = diagnostic["capture"]
    if capture is None:
        return
    base_fields = {"region", "paint_count", "paint_failure_bits"}
    assert isinstance(capture, dict) and base_fields <= set(capture) <= base_fields | {"markers", "attempt", "dc"}, "Unexpected GUI capture diagnostic"
    region = capture["region"]
    assert isinstance(region, dict) and set(region) == {"x", "y", "width", "height"}, "Unexpected GUI diagnostic region"
    assert all(type(value) is int and 0 <= value <= 100000 for value in region.values()), "Invalid GUI diagnostic geometry"
    assert region["width"] > 40 and region["height"] > 40 and region["width"] * region["height"] <= 1000000, "GUI diagnostic crop exceeded the limit"
    assert type(capture["paint_count"]) is int and 0 <= capture["paint_count"] <= 1024, "Invalid GUI paint count"
    assert type(capture["paint_failure_bits"]) is int and 0 <= capture["paint_failure_bits"] <= 127, "Invalid GUI paint failure bits"
    if "attempt" in capture:
        assert type(capture["attempt"]) is int and 1 <= capture["attempt"] <= 61, "Invalid GUI capture attempt count"
    if "dc" in capture:
        dc = capture["dc"]
        assert isinstance(dc, dict) and set(dc) == {"visible", "iconic", "cloaked_valid", "cloaked", "markers"}, "Unexpected GUI DC diagnostic"
        assert all(type(dc[key]) is bool for key in ("visible", "iconic", "cloaked_valid")), "Invalid GUI window visibility flags"
        assert type(dc["cloaked"]) is int and 0 <= dc["cloaked"] <= 7, "Invalid GUI window cloak flags"
        assert isinstance(dc["markers"], list) and len(dc["markers"]) == 4, "Invalid GUI DC sample count"
        for index, marker in enumerate(dc["markers"]):
            assert isinstance(marker, dict) and set(marker) == {"index", "window_rgb", "window_valid", "desktop_rgb", "desktop_valid"}, "Unexpected GUI DC marker diagnostic"
            assert type(marker["index"]) is int and marker["index"] == index, "Invalid GUI DC marker index"
            for source in ("window", "desktop"):
                assert type(marker[source + "_valid"]) is bool, "Invalid GUI DC validity flag"
                rgb = marker[source + "_rgb"]
                assert isinstance(rgb, list) and len(rgb) == 3 and all(type(value) is int and 0 <= value <= 255 for value in rgb), "Invalid GUI DC RGB sample"
    if "markers" not in capture:
        return
    markers = capture["markers"]
    expected = [[240, 20, 80], [20, 200, 90], [30, 100, 240], [220, 170, 20]]
    assert isinstance(markers, list) and len(markers) == 4, "Invalid GUI marker count"
    for index, marker in enumerate(markers):
        assert isinstance(marker, dict) and set(marker) == {"index", "expected_rgb", "actual_rgb", "mismatched_pixels", "matching_color_pixels"}, "Unexpected GUI marker diagnostic"
        assert marker["index"] == index and marker["expected_rgb"] == expected[index], "Unexpected GUI marker identity"
        assert isinstance(marker["actual_rgb"], list) and len(marker["actual_rgb"]) == 3 and all(type(value) is int and 0 <= value <= 255 for value in marker["actual_rgb"]), "Invalid GUI RGB sample"
        assert type(marker["mismatched_pixels"]) is int and 0 <= marker["mismatched_pixels"] <= 49, "Invalid GUI mismatch count"
        assert type(marker["matching_color_pixels"]) is int and 0 <= marker["matching_color_pixels"] <= 1000000, "Invalid GUI matching color count"


if __name__ == "__main__":
    main()
